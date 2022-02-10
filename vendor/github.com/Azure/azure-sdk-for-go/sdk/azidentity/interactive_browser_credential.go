// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azidentity

import (
	"context"
	"errors"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/public"
)

// InteractiveBrowserCredentialOptions contains optional parameters for InteractiveBrowserCredential.
type InteractiveBrowserCredentialOptions struct {
	azcore.ClientOptions

	// TenantID is the Azure Active Directory tenant the credential authenticates in. Defaults to the
	// "organizations" tenant, which can authenticate work and school accounts.
	TenantID string
	// ClientID is the ID of the application users will authenticate to.
	// Defaults to the ID of an Azure development application.
	ClientID string
	// RedirectURL will be supported in a future version but presently doesn't work: https://github.com/Azure/azure-sdk-for-go/issues/15632.
	// Applications which have "http://localhost" registered as a redirect URL need not set this option.
	RedirectURL string
	// AuthorityHost is the base URL of an Azure Active Directory authority. Defaults
	// to the value of environment variable AZURE_AUTHORITY_HOST, if set, or AzurePublicCloud.
	AuthorityHost AuthorityHost
}

func (o *InteractiveBrowserCredentialOptions) init() {
	if o.TenantID == "" {
		o.TenantID = organizationsTenantID
	}
	if o.ClientID == "" {
		o.ClientID = developerSignOnClientID
	}
}

// InteractiveBrowserCredential opens a browser to interactively authenticate a user.
type InteractiveBrowserCredential struct {
	client  publicClient
	options InteractiveBrowserCredentialOptions
	account public.Account
}

// NewInteractiveBrowserCredential constructs a new InteractiveBrowserCredential.
// options: Optional configuration.
func NewInteractiveBrowserCredential(options *InteractiveBrowserCredentialOptions) (*InteractiveBrowserCredential, error) {
	cp := InteractiveBrowserCredentialOptions{}
	if options != nil {
		cp = *options
	}
	cp.init()
	if !validTenantID(cp.TenantID) {
		return nil, errors.New(tenantIDValidationErr)
	}
	authorityHost, err := setAuthorityHost(cp.AuthorityHost)
	if err != nil {
		return nil, err
	}
	c, err := public.New(cp.ClientID,
		public.WithAuthority(runtime.JoinPaths(authorityHost, cp.TenantID)),
		public.WithHTTPClient(newPipelineAdapter(&cp.ClientOptions)),
	)
	if err != nil {
		return nil, err
	}
	return &InteractiveBrowserCredential{options: cp, client: c}, nil
}

// GetToken obtains a token from Azure Active Directory. This method is called automatically by Azure SDK clients.
// ctx: Context used to control the request lifetime.
// opts: Options for the token request, in particular the desired scope of the access token.
func (c *InteractiveBrowserCredential) GetToken(ctx context.Context, opts policy.TokenRequestOptions) (*azcore.AccessToken, error) {
	ar, err := c.client.AcquireTokenSilent(ctx, opts.Scopes, public.WithSilentAccount(c.account))
	if err == nil {
		logGetTokenSuccess(c, opts)
		return &azcore.AccessToken{Token: ar.AccessToken, ExpiresOn: ar.ExpiresOn.UTC()}, err
	}

	o := []public.InteractiveAuthOption{}
	if c.options.RedirectURL != "" {
		o = append(o, public.WithRedirectURI(c.options.RedirectURL))
	}
	ar, err = c.client.AcquireTokenInteractive(ctx, opts.Scopes, o...)
	if err != nil {
		addGetTokenFailureLogs("Interactive Browser Credential", err, true)
		return nil, newAuthenticationFailedError(err, nil)
	}
	c.account = ar.Account
	logGetTokenSuccess(c, opts)
	return &azcore.AccessToken{Token: ar.AccessToken, ExpiresOn: ar.ExpiresOn.UTC()}, err
}

var _ azcore.TokenCredential = (*InteractiveBrowserCredential)(nil)
