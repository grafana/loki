// Package sas provides SAS token functionality which implements TokenProvider from package auth for use with Azure
// Event Hubs and Service Bus.
package sas

//	MIT License
//
//	Copyright (c) Microsoft Corporation. All rights reserved.
//
//	Permission is hereby granted, free of charge, to any person obtaining a copy
//	of this software and associated documentation files (the "Software"), to deal
//	in the Software without restriction, including without limitation the rights
//	to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
//	copies of the Software, and to permit persons to whom the Software is
//	furnished to do so, subject to the following conditions:
//
//	The above copyright notice and this permission notice shall be included in all
//	copies or substantial portions of the Software.
//
//	THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
//	IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
//	FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
//	AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
//	LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
//	OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
//	SOFTWARE

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Azure/azure-amqp-common-go/v3/auth"
	"github.com/Azure/azure-amqp-common-go/v3/conn"
)

type (
	// Signer provides SAS token generation for use in Service Bus and Event Hub
	Signer struct {
		KeyName string
		Key     string
	}

	// TokenProvider is a SAS claims-based security token provider
	TokenProvider struct {
		signer *Signer
	}

	// TokenProviderOption provides configuration options for SAS Token Providers
	TokenProviderOption func(*TokenProvider) error
)

// TokenProviderWithEnvironmentVars creates a new SAS TokenProvider from environment variables
//
// There are two sets of environment variables which can produce a SAS TokenProvider
//
// 1) Expected Environment Variables:
//   - "EVENTHUB_KEY_NAME" the name of the Event Hub key
//   - "EVENTHUB_KEY_VALUE" the secret for the Event Hub key named in "EVENTHUB_KEY_NAME"
//
// 2) Expected Environment Variable:
//   - "EVENTHUB_CONNECTION_STRING" connection string from the Azure portal
//
// looks like: Endpoint=sb://namespace.servicebus.windows.net/;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=superSecret1234=
func TokenProviderWithEnvironmentVars() TokenProviderOption {
	return func(provider *TokenProvider) error {
		connStr := os.Getenv("EVENTHUB_CONNECTION_STRING")
		if connStr != "" {
			parsed, err := conn.ParsedConnectionFromStr(connStr)
			if err != nil {
				return err
			}
			provider.signer = NewSigner(parsed.KeyName, parsed.Key)
			return nil
		}

		var (
			keyName  = os.Getenv("EVENTHUB_KEY_NAME")
			keyValue = os.Getenv("EVENTHUB_KEY_VALUE")
		)

		if keyName == "" || keyValue == "" {
			return errors.New("unable to build SAS token provider because (EVENTHUB_KEY_NAME and EVENTHUB_KEY_VALUE) were empty, and EVENTHUB_CONNECTION_STRING was empty")
		}
		provider.signer = NewSigner(keyName, keyValue)
		return nil
	}
}

// TokenProviderWithKey configures a SAS TokenProvider to use the given key name and key (secret) for signing
func TokenProviderWithKey(keyName, key string) TokenProviderOption {
	return func(provider *TokenProvider) error {
		provider.signer = NewSigner(keyName, key)
		return nil
	}
}

// NewTokenProvider builds a SAS claims-based security token provider
func NewTokenProvider(opts ...TokenProviderOption) (*TokenProvider, error) {
	provider := new(TokenProvider)
	for _, opt := range opts {
		err := opt(provider)
		if err != nil {
			return nil, err
		}
	}
	return provider, nil
}

// GetToken gets a CBS SAS token
func (t *TokenProvider) GetToken(audience string) (*auth.Token, error) {
	signature, expiry := t.signer.SignWithDuration(audience, 2*time.Hour)
	return auth.NewToken(auth.CBSTokenTypeSAS, signature, expiry), nil
}

// NewSigner builds a new SAS signer for use in generation Service Bus and Event Hub SAS tokens
func NewSigner(keyName, key string) *Signer {
	return &Signer{
		KeyName: keyName,
		Key:     key,
	}
}

// SignWithDuration signs a given for a period of time from now
func (s *Signer) SignWithDuration(uri string, interval time.Duration) (signature, expiry string) {
	expiry = signatureExpiry(time.Now().UTC(), interval)
	return s.SignWithExpiry(uri, expiry), expiry
}

// SignWithExpiry signs a given uri with a given expiry string
func (s *Signer) SignWithExpiry(uri, expiry string) string {
	audience := strings.ToLower(url.QueryEscape(uri))
	sts := stringToSign(audience, expiry)
	sig := s.signString(sts)
	return fmt.Sprintf("SharedAccessSignature sr=%s&sig=%s&se=%s&skn=%s", audience, sig, expiry, s.KeyName)
}

func signatureExpiry(from time.Time, interval time.Duration) string {
	t := from.Add(interval).Round(time.Second).Unix()
	return strconv.FormatInt(t, 10)
}

func stringToSign(uri, expiry string) string {
	return uri + "\n" + expiry
}

func (s *Signer) signString(str string) string {
	h := hmac.New(sha256.New, []byte(s.Key))
	h.Write([]byte(str))
	encodedSig := base64.StdEncoding.EncodeToString(h.Sum(nil))
	return url.QueryEscape(encodedSig)
}
