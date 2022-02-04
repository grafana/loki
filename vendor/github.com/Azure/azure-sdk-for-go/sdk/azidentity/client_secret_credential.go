// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azidentity

import (
	"context"
	"errors"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/confidential"
)

// ClientSecretCredentialOptions contains optional parameters for ClientSecretCredential.
type ClientSecretCredentialOptions struct {
	azcore.ClientOptions

	// AuthorityHost is the base URL of an Azure Active Directory authority. Defaults
	// to the value of environment variable AZURE_AUTHORITY_HOST, if set, or AzurePublicCloud.
	AuthorityHost AuthorityHost
}

// ClientSecretCredential authenticates an application with a client secret.
type ClientSecretCredential struct {
	client confidentialClient
}

// NewClientSecretCredential constructs a ClientSecretCredential.
// tenantID: The application's Azure Active Directory tenant or directory ID.
// clientID: The application's client ID.
// clientSecret: One of the application's client secrets.
// options: Optional configuration.
func NewClientSecretCredential(tenantID string, clientID string, clientSecret string, options *ClientSecretCredentialOptions) (*ClientSecretCredential, error) {
	if !validTenantID(tenantID) {
		return nil, errors.New(tenantIDValidationErr)
	}
	if options == nil {
		options = &ClientSecretCredentialOptions{}
	}
	authorityHost, err := setAuthorityHost(options.AuthorityHost)
	if err != nil {
		return nil, err
	}
	cred, err := confidential.NewCredFromSecret(clientSecret)
	if err != nil {
		return nil, err
	}
	c, err := confidential.New(clientID, cred,
		confidential.WithAuthority(runtime.JoinPaths(authorityHost, tenantID)),
		confidential.WithHTTPClient(newPipelineAdapter(&options.ClientOptions)),
	)
	if err != nil {
		return nil, err
	}
	return &ClientSecretCredential{client: c}, nil
}

// GetToken obtains a token from Azure Active Directory. This method is called automatically by Azure SDK clients.
// ctx: Context used to control the request lifetime.
// opts: Options for the token request, in particular the desired scope of the access token.
func (c *ClientSecretCredential) GetToken(ctx context.Context, opts policy.TokenRequestOptions) (*azcore.AccessToken, error) {
	ar, err := c.client.AcquireTokenSilent(ctx, opts.Scopes)
	if err == nil {
		logGetTokenSuccess(c, opts)
		return &azcore.AccessToken{Token: ar.AccessToken, ExpiresOn: ar.ExpiresOn.UTC()}, err
	}

	ar, err = c.client.AcquireTokenByCredential(ctx, opts.Scopes)
	if err != nil {
		addGetTokenFailureLogs("Client Secret Credential", err, true)
		return nil, newAuthenticationFailedError(err, nil)
	}
	logGetTokenSuccess(c, opts)
	return &azcore.AccessToken{Token: ar.AccessToken, ExpiresOn: ar.ExpiresOn.UTC()}, err
}

var _ azcore.TokenCredential = (*ClientSecretCredential)(nil)
