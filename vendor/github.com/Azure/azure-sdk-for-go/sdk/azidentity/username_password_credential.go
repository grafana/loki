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

// UsernamePasswordCredentialOptions contains optional parameters for UsernamePasswordCredential.
type UsernamePasswordCredentialOptions struct {
	azcore.ClientOptions

	// AuthorityHost is the base URL of an Azure Active Directory authority. Defaults
	// to the value of environment variable AZURE_AUTHORITY_HOST, if set, or AzurePublicCloud.
	AuthorityHost AuthorityHost
}

// UsernamePasswordCredential authenticates user with a password. Microsoft doesn't recommend this kind of authentication,
// because it's less secure than other authentication flows. This credential is not interactive, so it isn't compatible
// with any form of multi-factor authentication, and the application must already have user or admin consent.
// This credential can only authenticate work and school accounts; it can't authenticate Microsoft accounts.
type UsernamePasswordCredential struct {
	client   publicClient
	username string
	password string
	account  public.Account
}

// NewUsernamePasswordCredential creates a UsernamePasswordCredential.
// tenantID: The ID of the Azure Active Directory tenant the credential authenticates in.
// clientID: The ID of the application users will authenticate to.
// username: A username (typically an email address).
// password: That user's password.
// options: Optional configuration.
func NewUsernamePasswordCredential(tenantID string, clientID string, username string, password string, options *UsernamePasswordCredentialOptions) (*UsernamePasswordCredential, error) {
	if !validTenantID(tenantID) {
		return nil, errors.New(tenantIDValidationErr)
	}
	if options == nil {
		options = &UsernamePasswordCredentialOptions{}
	}
	authorityHost, err := setAuthorityHost(options.AuthorityHost)
	if err != nil {
		return nil, err
	}
	c, err := public.New(clientID,
		public.WithAuthority(runtime.JoinPaths(authorityHost, tenantID)),
		public.WithHTTPClient(newPipelineAdapter(&options.ClientOptions)),
	)
	if err != nil {
		return nil, err
	}
	return &UsernamePasswordCredential{username: username, password: password, client: c}, nil
}

// GetToken obtains a token from Azure Active Directory. This method is called automatically by Azure SDK clients.
// ctx: Context used to control the request lifetime.
// opts: Options for the token request, in particular the desired scope of the access token.
func (c *UsernamePasswordCredential) GetToken(ctx context.Context, opts policy.TokenRequestOptions) (*azcore.AccessToken, error) {
	ar, err := c.client.AcquireTokenSilent(ctx, opts.Scopes, public.WithSilentAccount(c.account))
	if err == nil {
		logGetTokenSuccess(c, opts)
		return &azcore.AccessToken{Token: ar.AccessToken, ExpiresOn: ar.ExpiresOn.UTC()}, err
	}
	ar, err = c.client.AcquireTokenByUsernamePassword(ctx, opts.Scopes, c.username, c.password)
	if err != nil {
		addGetTokenFailureLogs("Username Password Credential", err, true)
		return nil, newAuthenticationFailedError(err, nil)
	}
	c.account = ar.Account
	logGetTokenSuccess(c, opts)
	return &azcore.AccessToken{Token: ar.AccessToken, ExpiresOn: ar.ExpiresOn.UTC()}, err
}

var _ azcore.TokenCredential = (*UsernamePasswordCredential)(nil)
