// Copyright (c) 2016, 2018, 2026, Oracle and/or its affiliates.  All rights reserved.
// This software is dual-licensed to you under the Universal Permissive License (UPL) 1.0 as shown at https://oss.oracle.com/licenses/upl or Apache License 2.0 as shown at http://www.apache.org/licenses/LICENSE-2.0. You may choose either license.

package auth

import (
	"crypto/rsa"
	"fmt"

	"github.com/oracle/oci-go-sdk/v65/common"
)

// OAuth2ConfigurationProvider provides Oauth2 type authentication
type OAuth2ConfigurationProvider struct {
	federationClient   federationClient
	sessionKeySupplier cacheableSessionKeySupplier
	region             string
}

// NewOAuth2ConfigurationProvider builds an OAuth2ConfigurationProvider from an existing config provider, and auth endpoint parameters
// The config provider can be for instance, resource, or service principals.
func NewOAuth2ConfigurationProvider(configProvider common.ConfigurationProvider, scope string, targetCompartment string) (common.ConfigurationProvider, error) {
	sessionKeySupplier := newCacheableSessionKeySupplier()
	region, err := configProvider.Region()
	if err != nil {
		return nil, fmt.Errorf("failed to get region from configProvider: %s", err.Error())
	}
	federationClient, err := newOAuth2FederationClient(configProvider, scope, targetCompartment, sessionKeySupplier)
	if err != nil {
		err = fmt.Errorf("failed to create auth provider: %w", err)
		return nil, err
	}
	return &OAuth2ConfigurationProvider{
		federationClient:   federationClient,
		sessionKeySupplier: sessionKeySupplier,
		region:             region,
	}, nil
}

// KeyID checks if the current security token is valid, and retrieves a new token from Auth Service if not
func (p OAuth2ConfigurationProvider) KeyID() (string, error) {
	var securityToken string
	var err error
	if securityToken, err = p.federationClient.SecurityToken(); err != nil {
		err = fmt.Errorf("failed to get security token: %s", err.Error())
		return "", err
	}
	return fmt.Sprintf("ST$%s", securityToken), nil
}

// PrivateRSAKey returns the private key of the session key supplier created for the OAuth Provider
func (p OAuth2ConfigurationProvider) PrivateRSAKey() (privateKey *rsa.PrivateKey, err error) {
	if privateKey, err = p.federationClient.PrivateKey(); err != nil {
		err = fmt.Errorf("failed to get private key: %s", err.Error())
		return nil, err
	}
	return privateKey, nil
}

func (p OAuth2ConfigurationProvider) SecurityToken() (string, error) {
	return p.federationClient.SecurityToken()
}

func (p OAuth2ConfigurationProvider) TenancyOCID() (string, error) {
	return "", nil
}

func (p OAuth2ConfigurationProvider) UserOCID() (string, error) {
	return "", nil
}

func (p OAuth2ConfigurationProvider) KeyFingerprint() (string, error) {
	return "", nil
}

func (p OAuth2ConfigurationProvider) Region() (string, error) {
	return p.region, nil
}

func (p OAuth2ConfigurationProvider) AuthType() (common.AuthConfig, error) {
	return common.AuthConfig{AuthType: common.OAuthDelegationToken}, nil
}
