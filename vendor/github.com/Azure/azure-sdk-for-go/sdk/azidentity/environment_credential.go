// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azidentity

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/internal/log"
)

// EnvironmentCredentialOptions contains optional parameters for EnvironmentCredential
type EnvironmentCredentialOptions struct {
	azcore.ClientOptions

	// AuthorityHost is the base URL of an Azure Active Directory authority. Defaults
	// to the value of environment variable AZURE_AUTHORITY_HOST, if set, or AzurePublicCloud.
	AuthorityHost AuthorityHost
}

// EnvironmentCredential authenticates a service principal with a secret or certificate, or a user with a password, depending
// on environment variable configuration. It reads configuration from these variables, in the following order:
//
// Service principal:
// - AZURE_TENANT_ID: ID of the service principal's tenant. Also called its "directory" ID.
// - AZURE_CLIENT_ID: the service principal's client ID
// - AZURE_CLIENT_SECRET: one of the service principal's client secrets
//
// Service principal with certificate:
// - AZURE_TENANT_ID: ID of the service principal's tenant. Also called its "directory" ID.
// - AZURE_CLIENT_ID: the service principal's client ID
// - AZURE_CLIENT_CERTIFICATE_PATH: path to a PEM or PKCS12 certificate file including the private key. The
//   certificate must not be password-protected.
//
// User with username and password:
// - AZURE_CLIENT_ID: the application's client ID
// - AZURE_USERNAME: a username (usually an email address)
// - AZURE_PASSWORD: that user's password
// - AZURE_TENANT_ID: (optional) tenant to authenticate in. If not set, defaults to the "organizations" tenant, which
//   can authenticate only Azure Active Directory work or school accounts.
type EnvironmentCredential struct {
	cred azcore.TokenCredential
}

// NewEnvironmentCredential creates an EnvironmentCredential.
// options: Optional configuration.
func NewEnvironmentCredential(options *EnvironmentCredentialOptions) (*EnvironmentCredential, error) {
	if options == nil {
		options = &EnvironmentCredentialOptions{}
	}
	tenantID := os.Getenv("AZURE_TENANT_ID")
	if tenantID == "" {
		return nil, errors.New("missing environment variable AZURE_TENANT_ID")
	}
	clientID := os.Getenv("AZURE_CLIENT_ID")
	if clientID == "" {
		return nil, errors.New("missing environment variable AZURE_CLIENT_ID")
	}
	if clientSecret := os.Getenv("AZURE_CLIENT_SECRET"); clientSecret != "" {
		log.Write(EventAuthentication, "Azure Identity => NewEnvironmentCredential() invoking ClientSecretCredential")
		o := &ClientSecretCredentialOptions{AuthorityHost: options.AuthorityHost, ClientOptions: options.ClientOptions}
		cred, err := NewClientSecretCredential(tenantID, clientID, clientSecret, o)
		if err != nil {
			return nil, err
		}
		return &EnvironmentCredential{cred: cred}, nil
	}
	if certPath := os.Getenv("AZURE_CLIENT_CERTIFICATE_PATH"); certPath != "" {
		log.Write(EventAuthentication, "Azure Identity => NewEnvironmentCredential() invoking ClientCertificateCredential")
		certData, err := os.ReadFile(certPath)
		if err != nil {
			return nil, fmt.Errorf(`failed to read certificate file "%s": %v`, certPath, err)
		}
		certs, key, err := ParseCertificates(certData, nil)
		if err != nil {
			return nil, fmt.Errorf(`failed to load certificate from "%s": %v`, certPath, err)
		}
		o := &ClientCertificateCredentialOptions{AuthorityHost: options.AuthorityHost, ClientOptions: options.ClientOptions}
		cred, err := NewClientCertificateCredential(tenantID, clientID, certs, key, o)
		if err != nil {
			return nil, err
		}
		return &EnvironmentCredential{cred: cred}, nil
	}
	if username := os.Getenv("AZURE_USERNAME"); username != "" {
		if password := os.Getenv("AZURE_PASSWORD"); password != "" {
			log.Write(EventAuthentication, "Azure Identity => NewEnvironmentCredential() invoking UsernamePasswordCredential")
			o := &UsernamePasswordCredentialOptions{AuthorityHost: options.AuthorityHost, ClientOptions: options.ClientOptions}
			cred, err := NewUsernamePasswordCredential(tenantID, clientID, username, password, o)
			if err != nil {
				return nil, err
			}
			return &EnvironmentCredential{cred: cred}, nil
		}
	}
	return nil, errors.New("missing environment variable AZURE_CLIENT_SECRET or AZURE_CLIENT_CERTIFICATE_PATH or AZURE_USERNAME and AZURE_PASSWORD")
}

// GetToken obtains a token from Azure Active Directory. This method is called automatically by Azure SDK clients.
// ctx: Context used to control the request lifetime.
// opts: Options for the token request, in particular the desired scope of the access token.
func (c *EnvironmentCredential) GetToken(ctx context.Context, opts policy.TokenRequestOptions) (*azcore.AccessToken, error) {
	return c.cred.GetToken(ctx, opts)
}

var _ azcore.TokenCredential = (*EnvironmentCredential)(nil)
