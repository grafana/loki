// Copyright (c) 2016, 2018, 2026, Oracle and/or its affiliates.  All rights reserved.
// This software is dual-licensed to you under the Universal Permissive License (UPL) 1.0 as shown at https://oss.oracle.com/licenses/upl or Apache License 2.0 as shown at http://www.apache.org/licenses/LICENSE-2.0. You may choose either license.

package auth

import (
	"crypto/md5"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/oracle/oci-go-sdk/v65/common"
)

type TokenExchangeBuilder struct {
	DomainUrl                 string
	ClientId                  string
	ClientSecret              string
	Region                    string
	RequestedTokenType        string
	ResType                   string
	RpstExp                   string
	SubjectTokenType          string
	PublicKey                 string
	InstancePrincipalProvider common.ConfigurationProvider
}

// TokenIssuer defines a type capable of retrieving tokens for the issuing
// authorization server.
type TokenIssuer interface {
	GetToken() (string, error)
}

// StaticTokenIssuer is a defined TokenIssuer that holds a static token. Not suitable
// for use longer than the validity period of the token.
type StaticTokenIssuer struct {
	token string
}

// GetToken satisfies the TokenIssuer interface for StaticTokenIssuer by returning
// the token held by StaticTokenIssuer.
func (s StaticTokenIssuer) GetToken() (string, error) {
	return s.token, nil
}

// TokenExchangeConfigurationProvider provides OCI configuration via token exchange,
// exposing claims and supporting a custom HTTP client.
type TokenExchangeConfigurationProvider struct {
	federationClient federationClient
	region           common.Region
}

// TokenExchangeConfigurationProviderFromIssuer creates a Configuration Provider from a
// function provided to retrieve a token from an identity provider.
func TokenExchangeConfigurationProviderFromIssuer(tokenIssuer TokenIssuer,
	tokenExchangeBuilder TokenExchangeBuilder) (common.ConfigurationProvider, error) {

	if tokenIssuer == nil {
		return nil, fmt.Errorf("invalid TokenIssuer")
	}

	var authCode string
	if tokenExchangeBuilder.ClientId != "" && tokenExchangeBuilder.ClientSecret != "" {
		authCode = base64.StdEncoding.EncodeToString([]byte(
			tokenExchangeBuilder.ClientId + ":" + tokenExchangeBuilder.ClientSecret))
	}

	requestData := map[string][]string{
		"grant_type": {"urn:ietf:params:oauth:grant-type:token-exchange"},
	}

	if tokenExchangeBuilder.RequestedTokenType == "" {
		return nil, fmt.Errorf("requested_token_type must be provided and non-empty")
	} else {
		requestData["requested_token_type"] = []string{tokenExchangeBuilder.RequestedTokenType}
	}

	if tokenExchangeBuilder.SubjectTokenType != "" {
		requestData["subject_token_type"] = []string{tokenExchangeBuilder.SubjectTokenType}
	}

	if tokenExchangeBuilder.PublicKey != "" {
		requestData["public_key"] = []string{tokenExchangeBuilder.PublicKey}
	}

	if tokenExchangeBuilder.RequestedTokenType == "urn:oci:token-type:oci-rpst" {
		if tokenExchangeBuilder.ResType != "" {
			requestData["res_type"] = []string{tokenExchangeBuilder.ResType}
		} else {
			return nil, fmt.Errorf("res_type parameter is required when requested_token_type is urn:oci:token-type:oci-rpst")
		}

		if tokenExchangeBuilder.RpstExp != "" {
			requestData["rpst_exp"] = []string{tokenExchangeBuilder.RpstExp}
		}
	}

	instancePrincipalProvider := tokenExchangeBuilder.InstancePrincipalProvider

	if instancePrincipalProvider == nil && authCode == "" {
		return nil, fmt.Errorf("InstancePrincipalProvider or ClientId and ClientSecret must be provided and non-nil")
	}

	fc := newTokenExchangeFederationClient(tokenIssuer, tokenExchangeBuilder.DomainUrl, authCode, requestData, instancePrincipalProvider)

	return TokenExchangeConfigurationProvider{
		federationClient: fc,
		region:           common.StringToRegion(tokenExchangeBuilder.Region),
	}, nil
}

// TokenExchangeConfigurationProviderFromToken returns a new configuration provider
// from a static token.
func TokenExchangeConfigurationProviderFromToken(token string, tokenExchangeBuilder TokenExchangeBuilder) (common.ConfigurationProvider, error) {

	issuer := StaticTokenIssuer{token: token}

	return TokenExchangeConfigurationProviderFromIssuer(issuer, tokenExchangeBuilder)
}

func (c TokenExchangeConfigurationProvider) GetClaim(key string) (interface{}, error) {
	return c.federationClient.GetClaim(key)
}

func (c TokenExchangeConfigurationProvider) KeyID() (string, error) {
	return c.federationClient.SecurityToken()
}

func (c TokenExchangeConfigurationProvider) PrivateRSAKey() (*rsa.PrivateKey, error) {
	return c.federationClient.PrivateKey()
}

// TenancyOCID provides the required receiver for the ConfigurationProvider interface
func (c TokenExchangeConfigurationProvider) TenancyOCID() (string, error) {
	claim, err := c.federationClient.GetClaim("tenant")
	if err != nil {
		return "", err
	}

	ocid, ok := claim.(string)
	if !ok {
		return "", ErrNonStringClaim
	}

	return ocid, nil
}

// UserOCID provides the required receiver for the ConfigurationProvider interface.
func (c TokenExchangeConfigurationProvider) UserOCID() (string, error) {
	claim, err := c.federationClient.GetClaim("sub")
	if err != nil {
		return "", err
	}

	ocid, ok := claim.(string)
	if !ok {
		return "", ErrNonStringClaim
	}

	return ocid, nil
}

// KeyFingerprint provides the required receiver for the ConfigurationProvider
// interface.
func (c TokenExchangeConfigurationProvider) KeyFingerprint() (string, error) {
	privateKey, err := c.PrivateRSAKey()
	if err != nil {
		return "", err
	}
	der, err := x509.MarshalPKIXPublicKey(privateKey.Public())
	if err != nil {
		return "", err
	}

	sum := md5.Sum(der)
	hexStr := hex.EncodeToString(sum[:]) // 32 hex chars

	var sb strings.Builder
	for i := 0; i < len(hexStr); i += 2 {
		if i > 0 {
			sb.WriteByte(':')
		}
		sb.WriteString(hexStr[i : i+2])
	}
	return sb.String(), nil

}

// Region provides the required receiver for the ConfigurationProvider interface.
func (c TokenExchangeConfigurationProvider) Region() (string, error) {
	r := string(c.region)
	if r == "" {
		return "", fmt.Errorf("no region assigned")
	}

	return r, nil
}

// AuthType provides the required receiver for the ConfigurationProvider interface.
func (c TokenExchangeConfigurationProvider) AuthType() (common.AuthConfig, error) {

	return common.AuthConfig{
		AuthType:         common.WorkloadIdentityFederation,
		IsFromConfigFile: false,
	}, nil
}

// tokenExchangeToken contains token and any related fields.
type tokenExchangeToken struct {
	token jwtToken
}

// String implements fmt.Stringer.
func (t tokenExchangeToken) String() string {
	return t.token.raw
}

// Valid implements the securityToken interface.
func (t tokenExchangeToken) Valid() bool {
	return !t.token.expired()
}

// GetClaim implements the ClaimHolder interface.
func (t tokenExchangeToken) GetClaim(key string) (interface{}, error) {

	// Per RFC7519 parsers should return only the lexically last member in the case
	// of duplicate claim names. We check payload first and return if claim found
	// and check header only if claim is not found in payload.
	if claim, ok := t.token.payload[key]; ok {
		return claim, nil
	}

	if claim, ok := t.token.header[key]; ok {
		return claim, nil
	}

	return nil, ErrNoSuchClaim
}
