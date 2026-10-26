// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package idtoken

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/auth/credentials"
	"cloud.google.com/go/auth/credentials/impersonate"
	intexternalaccount "cloud.google.com/go/auth/credentials/internal/externalaccount"
	intimpersonate "cloud.google.com/go/auth/credentials/internal/impersonate"
	"cloud.google.com/go/auth/internal"
	"cloud.google.com/go/auth/internal/credsfile"
	"github.com/googleapis/gax-go/v2/internallog"
)

const (
	jwtTokenURL = "https://oauth2.googleapis.com/token"
	iamCredAud  = "https://iamcredentials.googleapis.com/"
)

// credsFromDefault takes the credentials detected from the environment or JSON
// and constructs appropriate ID token credentials.
//
// Note: For ExternalAccount and ImpersonatedServiceAccount types, this function
// will create a new, non-impersonated base credential to avoid double
// impersonation when generating the ID token. It does not use the provided
// 'creds' as-is for these types.
func credsFromDefault(creds *auth.Credentials, opts *Options) (*auth.Credentials, error) {
	b := creds.JSON()
	t, err := credsfile.ParseFileType(b)
	if err != nil {
		return nil, err
	}
	switch credentials.CredType(t) {
	case credentials.ServiceAccount:
		f, err := credsfile.ParseServiceAccount(b)
		if err != nil {
			return nil, err
		}
		var tp auth.TokenProvider
		if resolveUniverseDomain(f) == internal.DefaultUniverseDomain {
			tp, err = new2LOTokenProvider(f, opts)
			if err != nil {
				return nil, err
			}
		} else {
			// In case of non-GDU universe domain, use IAM.
			tp = intimpersonate.IDTokenIAMOptions{
				Client: opts.client(),
				Logger: internallog.New(opts.Logger),
				// Pass the credentials universe domain to configure the endpoint.
				UniverseDomain:      auth.CredentialsPropertyFunc(creds.UniverseDomain),
				ServiceAccountEmail: f.ClientEmail,
				GenerateIDTokenRequest: intimpersonate.GenerateIDTokenRequest{
					Audience: opts.Audience,
				},
			}
		}
		tp = auth.NewCachedTokenProvider(tp, nil)
		return auth.NewCredentials(&auth.CredentialsOptions{
			TokenProvider:          tp,
			JSON:                   b,
			ProjectIDProvider:      auth.CredentialsPropertyFunc(creds.ProjectID),
			UniverseDomainProvider: auth.CredentialsPropertyFunc(creds.UniverseDomain),
		}), nil
	case credentials.ImpersonatedServiceAccount, credentials.ExternalAccount:
		type url struct {
			ServiceAccountImpersonationURL string `json:"service_account_impersonation_url"`
		}
		var accountURL url
		if err := json.Unmarshal(b, &accountURL); err != nil {
			return nil, err
		}
		account := filepath.Base(accountURL.ServiceAccountImpersonationURL)
		account = strings.Split(account, ":")[0]

		baseCreds, err := baseCredsForImpersonation(t, b, opts, creds)
		if err != nil {
			return nil, err
		}

		config := impersonate.IDTokenOptions{
			Audience:        opts.Audience,
			TargetPrincipal: account,
			IncludeEmail:    true,
			Client:          opts.client(),
			Credentials:     baseCreds, // Use the non-impersonated base credentials!
			Logger:          internallog.New(opts.Logger),
		}
		idTokenCreds, err := impersonate.NewIDTokenCredentials(&config)
		if err != nil {
			return nil, err
		}
		return auth.NewCredentials(&auth.CredentialsOptions{
			TokenProvider:          idTokenCreds,
			JSON:                   b,
			ProjectIDProvider:      auth.CredentialsPropertyFunc(creds.ProjectID),
			UniverseDomainProvider: auth.CredentialsPropertyFunc(creds.UniverseDomain),
			QuotaProjectIDProvider: auth.CredentialsPropertyFunc(creds.QuotaProjectID),
		}), nil
	default:
		return nil, fmt.Errorf("idtoken: unsupported credentials type: %v", t)
	}
}

func new2LOTokenProvider(f *credsfile.ServiceAccountFile, opts *Options) (auth.TokenProvider, error) {
	opts2LO := &auth.Options2LO{
		Email:        f.ClientEmail,
		PrivateKey:   []byte(f.PrivateKey),
		PrivateKeyID: f.PrivateKeyID,
		TokenURL:     f.TokenURL,
		UseIDToken:   true,
		Logger:       internallog.New(opts.Logger),
	}
	if opts2LO.TokenURL == "" {
		opts2LO.TokenURL = jwtTokenURL
	}

	var customClaims map[string]interface{}
	if opts != nil {
		customClaims = opts.CustomClaims
	}
	if customClaims == nil {
		customClaims = make(map[string]interface{})
	}
	customClaims["target_audience"] = opts.Audience

	opts2LO.PrivateClaims = customClaims
	return auth.New2LOTokenProvider(opts2LO)
}

// resolveUniverseDomain returns the default service domain for a given
// Cloud universe. This is the universe domain configured for the credentials,
// which will be used in endpoint.
func resolveUniverseDomain(f *credsfile.ServiceAccountFile) string {
	if f.UniverseDomain != "" {
		return f.UniverseDomain
	}
	return internal.DefaultUniverseDomain
}

// baseCredsForImpersonation constructs a non-impersonated base credential
// provider to avoid double impersonation. The 'creds' object returned by
// DetectDefault is already impersonated because it faithfully fulfills the
// instructions in the configuration file. However, for the specific context of
// generating an ID token, we must bypass that first layer of impersonation and
// use the source principal directly to call the generateIdToken API. This
// maintains separation of concerns by letting DetectDefault act as a general
// loader while handling the specific needs of idtoken generation here, avoiding
// a leaky abstraction in core packages.
func baseCredsForImpersonation(t string, b []byte, opts *Options, creds *auth.Credentials) (*auth.Credentials, error) {
	var baseCreds *auth.Credentials
	if credentials.CredType(t) == credentials.ExternalAccount {
		f, err := credsfile.ParseExternalAccount(b)
		if err != nil {
			return nil, err
		}
		externalOpts := &intexternalaccount.Options{
			Audience:                 f.Audience,
			SubjectTokenType:         f.SubjectTokenType,
			TokenURL:                 f.TokenURL,
			TokenInfoURL:             f.TokenInfoURL,
			ClientSecret:             f.ClientSecret,
			ClientID:                 f.ClientID,
			CredentialSource:         f.CredentialSource,
			QuotaProjectID:           f.QuotaProjectID,
			Scopes:                   []string{"https://www.googleapis.com/auth/cloud-platform"},
			WorkforcePoolUserProject: f.WorkforcePoolUserProject,
			Client:                   opts.client(),
			Logger:                   opts.Logger,
			IsDefaultClient:          opts.Client == nil,
		}
		// Do NOT set ServiceAccountImpersonationURL here to avoid the first layer of impersonation!
		baseTP, err := intexternalaccount.NewTokenProvider(externalOpts)
		if err != nil {
			return nil, err
		}
		baseCreds = auth.NewCredentials(&auth.CredentialsOptions{
			TokenProvider:          baseTP,
			JSON:                   b,
			UniverseDomainProvider: auth.CredentialsPropertyFunc(creds.UniverseDomain),
		})
	} else {
		// For ImpersonatedServiceAccount
		f, err := credsfile.ParseImpersonatedServiceAccount(b)
		if err != nil {
			return nil, err
		}
		// Extract source credentials
		sourceOpts := &credentials.DetectOptions{
			Scopes:           []string{"https://www.googleapis.com/auth/cloud-platform"},
			CredentialsJSON:  f.CredSource,
			Client:           opts.client(),
			UseSelfSignedJWT: true,
		}
		// Detect credentials for the source
		sourceCreds, err := credentials.DetectDefault(sourceOpts)
		if err != nil {
			return nil, err
		}
		baseCreds = auth.NewCredentials(&auth.CredentialsOptions{
			TokenProvider:          sourceCreds,
			JSON:                   b,
			UniverseDomainProvider: auth.CredentialsPropertyFunc(creds.UniverseDomain),
		})
	}
	return baseCreds, nil
}
