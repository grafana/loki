// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azidentity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

type managedIdentityIDKind int

const (
	miClientID   managedIdentityIDKind = 0
	miResourceID managedIdentityIDKind = 1
)

// ManagedIDKind identifies the ID of a managed identity as either a client or resource ID
type ManagedIDKind interface {
	fmt.Stringer
	idKind() managedIdentityIDKind
}

// ClientID is an identity's client ID. Use it with ManagedIdentityCredentialOptions, for example:
// ManagedIdentityCredentialOptions{ID: ClientID("7cf7db0d-...")}
type ClientID string

func (ClientID) idKind() managedIdentityIDKind {
	return miClientID
}

func (c ClientID) String() string {
	return string(c)
}

// ResourceID is an identity's resource ID. Use it with ManagedIdentityCredentialOptions, for example:
// ManagedIdentityCredentialOptions{ID: ResourceID("/subscriptions/...")}
type ResourceID string

func (ResourceID) idKind() managedIdentityIDKind {
	return miResourceID
}

func (r ResourceID) String() string {
	return string(r)
}

// ManagedIdentityCredentialOptions contains optional parameters for ManagedIdentityCredential.
type ManagedIdentityCredentialOptions struct {
	azcore.ClientOptions

	// ID is the ID of a managed identity the credential should authenticate. Set this field to use a specific identity
	// instead of the hosting environment's default. The value may be the identity's client ID or resource ID, but note that
	// some platforms don't accept resource IDs.
	ID ManagedIDKind
}

// ManagedIdentityCredential authenticates with an Azure managed identity in any hosting environment which supports managed identities.
// This credential defaults to using a system-assigned identity. Use ManagedIdentityCredentialOptions.ID to specify a user-assigned identity.
// See Azure Active Directory documentation for more information about managed identities:
// https://docs.microsoft.com/azure/active-directory/managed-identities-azure-resources/overview
type ManagedIdentityCredential struct {
	id     ManagedIDKind
	client *managedIdentityClient
}

// NewManagedIdentityCredential creates a ManagedIdentityCredential.
// options: Optional configuration.
func NewManagedIdentityCredential(options *ManagedIdentityCredentialOptions) (*ManagedIdentityCredential, error) {
	if options == nil {
		options = &ManagedIdentityCredentialOptions{}
	}
	client, err := newManagedIdentityClient(options)
	if err != nil {
		logCredentialError("Managed Identity Credential", err)
		return nil, err
	}
	return &ManagedIdentityCredential{id: options.ID, client: client}, nil
}

// GetToken obtains a token from Azure Active Directory. This method is called automatically by Azure SDK clients.
// ctx: Context used to control the request lifetime.
// opts: Options for the token request, in particular the desired scope of the access token.
func (c *ManagedIdentityCredential) GetToken(ctx context.Context, opts policy.TokenRequestOptions) (*azcore.AccessToken, error) {
	if len(opts.Scopes) != 1 {
		err := errors.New("ManagedIdentityCredential.GetToken() requires exactly one scope")
		addGetTokenFailureLogs("Managed Identity Credential", err, true)
		return nil, err
	}
	// managed identity endpoints require an AADv1 resource (i.e. token audience), not a v2 scope, so we remove "/.default" here
	scopes := []string{strings.TrimSuffix(opts.Scopes[0], defaultSuffix)}
	tk, err := c.client.authenticate(ctx, c.id, scopes)
	if err != nil {
		addGetTokenFailureLogs("Managed Identity Credential", err, true)
		return nil, err
	}
	logGetTokenSuccess(c, opts)
	return tk, err
}

var _ azcore.TokenCredential = (*ManagedIdentityCredential)(nil)
