// Package aad provides an implementation of an Azure Active Directory JWT provider which implements TokenProvider
// from package auth for use with Azure Event Hubs and Service Bus.
package aad

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
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"time"

	"github.com/Azure/go-autorest/autorest/adal"
	"github.com/Azure/go-autorest/autorest/azure"
	"golang.org/x/crypto/pkcs12"

	"github.com/Azure/azure-amqp-common-go/v3/auth"
)

const (
	eventhubResourceURI = "https://eventhubs.azure.net/"
)

type (
	// TokenProviderConfiguration provides configuration parameters for building JWT AAD providers
	TokenProviderConfiguration struct {
		TenantID            string
		ClientID            string
		ClientSecret        string
		CertificatePath     string
		CertificatePassword string
		ResourceURI         string
		aadToken            *adal.ServicePrincipalToken
		Env                 *azure.Environment
	}

	// TokenProvider provides cbs.TokenProvider functionality for Azure Active Directory JWTs
	TokenProvider struct {
		tokenProvider *adal.ServicePrincipalToken
	}

	// JWTProviderOption provides configuration options for constructing AAD Token Providers
	JWTProviderOption func(provider *TokenProviderConfiguration) error
)

// JWTProviderWithAzureEnvironment configures the token provider to use a specific Azure Environment
func JWTProviderWithAzureEnvironment(env *azure.Environment) JWTProviderOption {
	return func(config *TokenProviderConfiguration) error {
		config.Env = env
		return nil
	}
}

// JWTProviderWithEnvironmentVars configures the TokenProvider using the environment variables available
//
// 1. Client Credentials: attempt to authenticate with a Service Principal via "AZURE_TENANT_ID", "AZURE_CLIENT_ID" and
//    "AZURE_CLIENT_SECRET"
//
// 2. Client Certificate: attempt to authenticate with a Service Principal via "AZURE_TENANT_ID", "AZURE_CLIENT_ID",
//    "AZURE_CERTIFICATE_PATH" and "AZURE_CERTIFICATE_PASSWORD"
//
// 3. Managed Service Identity (MSI): attempt to authenticate via MSI
//
//
// The Azure Environment used can be specified using the name of the Azure Environment set in "AZURE_ENVIRONMENT" var.
func JWTProviderWithEnvironmentVars() JWTProviderOption {
	return func(config *TokenProviderConfiguration) error {
		config.TenantID = os.Getenv("AZURE_TENANT_ID")
		config.ClientID = os.Getenv("AZURE_CLIENT_ID")
		config.ClientSecret = os.Getenv("AZURE_CLIENT_SECRET")
		config.CertificatePath = os.Getenv("AZURE_CERTIFICATE_PATH")
		config.CertificatePassword = os.Getenv("AZURE_CERTIFICATE_PASSWORD")

		if config.Env == nil {
			env, err := azureEnvFromEnvironment()
			if err != nil {
				return err
			}
			config.Env = env
		}
		return nil
	}
}

// JWTProviderWithResourceURI configures the token provider to use a specific eventhubResourceURI URI
func JWTProviderWithResourceURI(resourceURI string) JWTProviderOption {
	return func(config *TokenProviderConfiguration) error {
		config.ResourceURI = resourceURI
		return nil
	}
}

// JWTProviderWithAADToken configures the token provider to use a specific Azure Active Directory Service Principal token
func JWTProviderWithAADToken(aadToken *adal.ServicePrincipalToken) JWTProviderOption {
	return func(config *TokenProviderConfiguration) error {
		config.aadToken = aadToken
		return nil
	}
}

// NewJWTProvider builds an Azure Active Directory claims-based security token provider
func NewJWTProvider(opts ...JWTProviderOption) (*TokenProvider, error) {
	config := &TokenProviderConfiguration{
		ResourceURI: eventhubResourceURI,
	}

	for _, opt := range opts {
		err := opt(config)
		if err != nil {
			return nil, err
		}
	}

	if config.aadToken == nil {
		spToken, err := config.NewServicePrincipalToken()
		if err != nil {
			return nil, err
		}
		config.aadToken = spToken
	}
	return &TokenProvider{tokenProvider: config.aadToken}, nil
}

// NewServicePrincipalToken creates a new Azure Active Directory Service Principal token provider
func (c *TokenProviderConfiguration) NewServicePrincipalToken() (*adal.ServicePrincipalToken, error) {
	oauthConfig, err := adal.NewOAuthConfig(c.Env.ActiveDirectoryEndpoint, c.TenantID)
	if err != nil {
		return nil, err
	}

	// 1.Client Credentials
	if c.ClientSecret != "" {
		spToken, err := adal.NewServicePrincipalToken(*oauthConfig, c.ClientID, c.ClientSecret, c.ResourceURI)
		if err != nil {
			return nil, fmt.Errorf("failed to get oauth token from client credentials: %v", err)
		}
		if err := spToken.Refresh(); err != nil {
			return nil, fmt.Errorf("failed to refersh token: %v", spToken)
		}
		return spToken, nil
	}

	// 2. Client Certificate
	if c.CertificatePath != "" {
		certData, err := ioutil.ReadFile(c.CertificatePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read the certificate file (%s): %v", c.CertificatePath, err)
		}
		certificate, rsaPrivateKey, err := decodePkcs12(certData, c.CertificatePassword)
		if err != nil {
			return nil, fmt.Errorf("failed to decode pkcs12 certificate while creating spt: %v", err)
		}
		spToken, err := adal.NewServicePrincipalTokenFromCertificate(*oauthConfig, c.ClientID, certificate, rsaPrivateKey, c.ResourceURI)
		if err != nil {
			return nil, fmt.Errorf("failed to get oauth token from certificate auth: %v", err)
		}
		if err := spToken.Refresh(); err != nil {
			return nil, fmt.Errorf("failed to refersh token: %v", spToken)
		}
		return spToken, nil
	}

	// 3. By default return MSI
	msiEndpoint, err := adal.GetMSIVMEndpoint()
	if err != nil {
		return nil, err
	}
	spToken, err := adal.NewServicePrincipalTokenFromMSI(msiEndpoint, c.ResourceURI)
	if err != nil {
		return nil, fmt.Errorf("failed to get oauth token from MSI: %v", err)
	}
	if err := spToken.Refresh(); err != nil {
		return nil, fmt.Errorf("failed to refersh token: %v", spToken)
	}
	return spToken, nil
}

// GetToken gets a CBS JWT
func (t *TokenProvider) GetToken(audience string) (*auth.Token, error) {
	token := t.tokenProvider.Token()
	expireTicks, err := strconv.ParseInt(string(token.ExpiresOn), 10, 64)
	if err != nil {
		return nil, err
	}
	expires := time.Unix(expireTicks, 0)

	if expires.Before(time.Now()) {
		if err := t.tokenProvider.Refresh(); err != nil {
			return nil, err
		}
		token = t.tokenProvider.Token()
	}

	return auth.NewToken(auth.CBSTokenTypeJWT, token.AccessToken, string(token.ExpiresOn)), nil
}

func decodePkcs12(pkcs []byte, password string) (*x509.Certificate, *rsa.PrivateKey, error) {
	privateKey, certificate, err := pkcs12.Decode(pkcs, password)
	if err != nil {
		return nil, nil, err
	}

	rsaPrivateKey, isRsaKey := privateKey.(*rsa.PrivateKey)
	if !isRsaKey {
		return nil, nil, fmt.Errorf("PKCS#12 certificate must contain an RSA private key")
	}

	return certificate, rsaPrivateKey, nil
}

func azureEnvFromEnvironment() (*azure.Environment, error) {
	envName := os.Getenv("AZURE_ENVIRONMENT")

	var env azure.Environment
	if envName == "" {
		env = azure.PublicCloud
	} else {
		var err error
		env, err = azure.EnvironmentFromName(envName)
		if err != nil {
			return nil, err
		}
	}
	return &env, nil
}
