//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azidentity

import (
	"context"
	"errors"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/confidential"
)

const credNameAssertion = "ClientAssertionCredential"

// ClientAssertionCredential authenticates an application with assertions provided by a callback function.
// This credential is for advanced scenarios. [ClientCertificateCredential] has a more convenient API for
// the most common assertion scenario, authenticating a service principal with a certificate. See
// [Azure AD documentation] for details of the assertion format.
//
// [Azure AD documentation]: https://docs.microsoft.com/azure/active-directory/develop/active-directory-certificate-credentials#assertion-format
type ClientAssertionCredential struct {
	client *confidentialClient
}

// ClientAssertionCredentialOptions contains optional parameters for ClientAssertionCredential.
type ClientAssertionCredentialOptions struct {
	azcore.ClientOptions

	// AdditionallyAllowedTenants specifies additional tenants for which the credential may acquire tokens.
	// Add the wildcard value "*" to allow the credential to acquire tokens for any tenant in which the
	// application is registered.
	AdditionallyAllowedTenants []string
	// DisableInstanceDiscovery should be set true only by applications authenticating in disconnected clouds, or
	// private clouds such as Azure Stack. It determines whether the credential requests Azure AD instance metadata
	// from https://login.microsoft.com before authenticating. Setting this to true will skip this request, making
	// the application responsible for ensuring the configured authority is valid and trustworthy.
	DisableInstanceDiscovery bool
}

// NewClientAssertionCredential constructs a ClientAssertionCredential. The getAssertion function must be thread safe. Pass nil for options to accept defaults.
func NewClientAssertionCredential(tenantID, clientID string, getAssertion func(context.Context) (string, error), options *ClientAssertionCredentialOptions) (*ClientAssertionCredential, error) {
	if getAssertion == nil {
		return nil, errors.New("getAssertion must be a function that returns assertions")
	}
	if options == nil {
		options = &ClientAssertionCredentialOptions{}
	}
	cred := confidential.NewCredFromAssertionCallback(
		func(ctx context.Context, _ confidential.AssertionRequestOptions) (string, error) {
			return getAssertion(ctx)
		},
	)
	msalOpts := confidentialClientOptions{
		AdditionallyAllowedTenants: options.AdditionallyAllowedTenants,
		ClientOptions:              options.ClientOptions,
		DisableInstanceDiscovery:   options.DisableInstanceDiscovery,
	}
	c, err := newConfidentialClient(tenantID, clientID, credNameAssertion, cred, msalOpts)
	if err != nil {
		return nil, err
	}
	return &ClientAssertionCredential{client: c}, nil
}

// GetToken requests an access token from Azure Active Directory. This method is called automatically by Azure SDK clients.
func (c *ClientAssertionCredential) GetToken(ctx context.Context, opts policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return c.client.GetToken(ctx, opts)
}

var _ azcore.TokenCredential = (*ClientAssertionCredential)(nil)
