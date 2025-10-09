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

package credentials

import (
	"errors"
	"fmt"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/auth/credentials/internal/externalaccount"
	"cloud.google.com/go/auth/credentials/internal/externalaccountuser"
	"cloud.google.com/go/auth/credentials/internal/gdch"
	"cloud.google.com/go/auth/credentials/internal/impersonate"
	internalauth "cloud.google.com/go/auth/internal"
	"cloud.google.com/go/auth/internal/credsfile"
	"cloud.google.com/go/auth/internal/trustboundary"
)

func fileCredentials(b []byte, opts *DetectOptions) (*auth.Credentials, error) {
	fileType, err := credsfile.ParseFileType(b)
	if err != nil {
		return nil, err
	}

	var projectID, universeDomain string
	var tp auth.TokenProvider
	switch fileType {
	case credsfile.UnknownCredType:
		return nil, errors.New("credentials: unsupported unidentified file type")
	case credsfile.ServiceAccountKey:
		f, err := credsfile.ParseServiceAccount(b)
		if err != nil {
			return nil, err
		}
		tp, err = handleServiceAccount(f, opts)
		if err != nil {
			return nil, err
		}
		projectID = f.ProjectID
		universeDomain = resolveUniverseDomain(opts.UniverseDomain, f.UniverseDomain)
	case credsfile.UserCredentialsKey:
		f, err := credsfile.ParseUserCredentials(b)
		if err != nil {
			return nil, err
		}
		tp, err = handleUserCredential(f, opts)
		if err != nil {
			return nil, err
		}
		universeDomain = f.UniverseDomain
	case credsfile.ExternalAccountKey:
		f, err := credsfile.ParseExternalAccount(b)
		if err != nil {
			return nil, err
		}
		tp, err = handleExternalAccount(f, opts)
		if err != nil {
			return nil, err
		}
		universeDomain = resolveUniverseDomain(opts.UniverseDomain, f.UniverseDomain)
	case credsfile.ExternalAccountAuthorizedUserKey:
		f, err := credsfile.ParseExternalAccountAuthorizedUser(b)
		if err != nil {
			return nil, err
		}
		tp, err = handleExternalAccountAuthorizedUser(f, opts)
		if err != nil {
			return nil, err
		}
		universeDomain = f.UniverseDomain
	case credsfile.ImpersonatedServiceAccountKey:
		f, err := credsfile.ParseImpersonatedServiceAccount(b)
		if err != nil {
			return nil, err
		}
		tp, err = handleImpersonatedServiceAccount(f, opts)
		if err != nil {
			return nil, err
		}
		universeDomain = resolveUniverseDomain(opts.UniverseDomain, f.UniverseDomain)
	case credsfile.GDCHServiceAccountKey:
		f, err := credsfile.ParseGDCHServiceAccount(b)
		if err != nil {
			return nil, err
		}
		tp, err = handleGDCHServiceAccount(f, opts)
		if err != nil {
			return nil, err
		}
		projectID = f.Project
		universeDomain = f.UniverseDomain
	default:
		return nil, fmt.Errorf("credentials: unsupported filetype %q", fileType)
	}
	return auth.NewCredentials(&auth.CredentialsOptions{
		TokenProvider: auth.NewCachedTokenProvider(tp, &auth.CachedTokenProviderOptions{
			ExpireEarly: opts.EarlyTokenRefresh,
		}),
		JSON:              b,
		ProjectIDProvider: internalauth.StaticCredentialsProperty(projectID),
		// TODO(codyoss): only set quota project here if there was a user override
		UniverseDomainProvider: internalauth.StaticCredentialsProperty(universeDomain),
	}), nil
}

// resolveUniverseDomain returns optsUniverseDomain if non-empty, in order to
// support configuring universe-specific credentials in code. Auth flows
// unsupported for universe domain should not use this func, but should instead
// simply set the file universe domain on the credentials.
func resolveUniverseDomain(optsUniverseDomain, fileUniverseDomain string) string {
	if optsUniverseDomain != "" {
		return optsUniverseDomain
	}
	return fileUniverseDomain
}

func handleServiceAccount(f *credsfile.ServiceAccountFile, opts *DetectOptions) (auth.TokenProvider, error) {
	ud := resolveUniverseDomain(opts.UniverseDomain, f.UniverseDomain)
	if opts.UseSelfSignedJWT {
		return configureSelfSignedJWT(f, opts)
	} else if ud != "" && ud != internalauth.DefaultUniverseDomain {
		// For non-GDU universe domains, token exchange is impossible and services
		// must support self-signed JWTs.
		opts.UseSelfSignedJWT = true
		return configureSelfSignedJWT(f, opts)
	}
	opts2LO := &auth.Options2LO{
		Email:          f.ClientEmail,
		PrivateKey:     []byte(f.PrivateKey),
		PrivateKeyID:   f.PrivateKeyID,
		Scopes:         opts.scopes(),
		TokenURL:       f.TokenURL,
		Subject:        opts.Subject,
		Client:         opts.client(),
		Logger:         opts.logger(),
		UniverseDomain: ud,
	}
	if opts2LO.TokenURL == "" {
		opts2LO.TokenURL = jwtTokenURL
	}

	tp, err := auth.New2LOTokenProvider(opts2LO)
	if err != nil {
		return nil, err
	}

	trustBoundaryEnabled, err := trustboundary.IsEnabled()
	if err != nil {
		return nil, err
	}
	if !trustBoundaryEnabled {
		return tp, nil
	}
	saConfig := trustboundary.NewServiceAccountConfigProvider(opts2LO.Email, opts2LO.UniverseDomain)
	return trustboundary.NewProvider(opts.client(), saConfig, opts.logger(), tp)
}

func handleUserCredential(f *credsfile.UserCredentialsFile, opts *DetectOptions) (auth.TokenProvider, error) {
	opts3LO := &auth.Options3LO{
		ClientID:         f.ClientID,
		ClientSecret:     f.ClientSecret,
		Scopes:           opts.scopes(),
		AuthURL:          googleAuthURL,
		TokenURL:         opts.tokenURL(),
		AuthStyle:        auth.StyleInParams,
		EarlyTokenExpiry: opts.EarlyTokenRefresh,
		RefreshToken:     f.RefreshToken,
		Client:           opts.client(),
		Logger:           opts.logger(),
	}
	return auth.New3LOTokenProvider(opts3LO)
}

func handleExternalAccount(f *credsfile.ExternalAccountFile, opts *DetectOptions) (auth.TokenProvider, error) {
	externalOpts := &externalaccount.Options{
		Audience:                       f.Audience,
		SubjectTokenType:               f.SubjectTokenType,
		TokenURL:                       f.TokenURL,
		TokenInfoURL:                   f.TokenInfoURL,
		ServiceAccountImpersonationURL: f.ServiceAccountImpersonationURL,
		ClientSecret:                   f.ClientSecret,
		ClientID:                       f.ClientID,
		CredentialSource:               f.CredentialSource,
		QuotaProjectID:                 f.QuotaProjectID,
		Scopes:                         opts.scopes(),
		WorkforcePoolUserProject:       f.WorkforcePoolUserProject,
		Client:                         opts.client(),
		Logger:                         opts.logger(),
		IsDefaultClient:                opts.Client == nil,
	}
	if f.ServiceAccountImpersonation != nil {
		externalOpts.ServiceAccountImpersonationLifetimeSeconds = f.ServiceAccountImpersonation.TokenLifetimeSeconds
	}
	tp, err := externalaccount.NewTokenProvider(externalOpts)
	if err != nil {
		return nil, err
	}
	trustBoundaryEnabled, err := trustboundary.IsEnabled()
	if err != nil {
		return nil, err
	}
	if !trustBoundaryEnabled {
		return tp, nil
	}

	ud := resolveUniverseDomain(opts.UniverseDomain, f.UniverseDomain)
	var configProvider trustboundary.ConfigProvider

	if f.ServiceAccountImpersonationURL == "" {
		// No impersonation, this is a direct external account credential.
		// The trust boundary is based on the workload/workforce pool.
		var err error
		configProvider, err = trustboundary.NewExternalAccountConfigProvider(f.Audience, ud)
		if err != nil {
			return nil, err
		}
	} else {
		// Impersonation is used. The trust boundary is based on the target service account.
		targetSAEmail, err := impersonate.ExtractServiceAccountEmail(f.ServiceAccountImpersonationURL)
		if err != nil {
			return nil, fmt.Errorf("credentials: could not extract target service account email for trust boundary: %w", err)
		}
		configProvider = trustboundary.NewServiceAccountConfigProvider(targetSAEmail, ud)
	}

	return trustboundary.NewProvider(opts.client(), configProvider, opts.logger(), tp)
}

func handleExternalAccountAuthorizedUser(f *credsfile.ExternalAccountAuthorizedUserFile, opts *DetectOptions) (auth.TokenProvider, error) {
	externalOpts := &externalaccountuser.Options{
		Audience:     f.Audience,
		RefreshToken: f.RefreshToken,
		TokenURL:     f.TokenURL,
		TokenInfoURL: f.TokenInfoURL,
		ClientID:     f.ClientID,
		ClientSecret: f.ClientSecret,
		Scopes:       opts.scopes(),
		Client:       opts.client(),
		Logger:       opts.logger(),
	}
	tp, err := externalaccountuser.NewTokenProvider(externalOpts)
	if err != nil {
		return nil, err
	}
	trustBoundaryEnabled, err := trustboundary.IsEnabled()
	if err != nil {
		return nil, err
	}
	if !trustBoundaryEnabled {
		return tp, nil
	}

	ud := resolveUniverseDomain(opts.UniverseDomain, f.UniverseDomain)
	configProvider, err := trustboundary.NewExternalAccountConfigProvider(f.Audience, ud)
	if err != nil {
		return nil, err
	}
	return trustboundary.NewProvider(opts.client(), configProvider, opts.logger(), tp)
}

func handleImpersonatedServiceAccount(f *credsfile.ImpersonatedServiceAccountFile, opts *DetectOptions) (auth.TokenProvider, error) {
	if f.ServiceAccountImpersonationURL == "" || f.CredSource == nil {
		return nil, errors.New("missing 'source_credentials' field or 'service_account_impersonation_url' in credentials")
	}

	sourceTP, err := fileCredentials(f.CredSource, opts)
	if err != nil {
		return nil, err
	}
	ud := resolveUniverseDomain(opts.UniverseDomain, f.UniverseDomain)
	impOpts := &impersonate.Options{
		URL:            f.ServiceAccountImpersonationURL,
		Scopes:         opts.scopes(),
		Tp:             sourceTP,
		Delegates:      f.Delegates,
		Client:         opts.client(),
		Logger:         opts.logger(),
		UniverseDomain: ud,
	}
	tp, err := impersonate.NewTokenProvider(impOpts)
	if err != nil {
		return nil, err
	}
	trustBoundaryEnabled, err := trustboundary.IsEnabled()
	if err != nil {
		return nil, err
	}
	if !trustBoundaryEnabled {
		return tp, nil
	}
	targetSAEmail, err := impersonate.ExtractServiceAccountEmail(f.ServiceAccountImpersonationURL)
	if err != nil {
		return nil, fmt.Errorf("credentials: could not extract target service account email for trust boundary: %w", err)
	}
	targetSAConfig := trustboundary.NewServiceAccountConfigProvider(targetSAEmail, ud)
	return trustboundary.NewProvider(opts.client(), targetSAConfig, opts.logger(), tp)
}
func handleGDCHServiceAccount(f *credsfile.GDCHServiceAccountFile, opts *DetectOptions) (auth.TokenProvider, error) {
	return gdch.NewTokenProvider(f, &gdch.Options{
		STSAudience: opts.STSAudience,
		Client:      opts.client(),
		Logger:      opts.logger(),
	})
}
