// Copyright (c) 2016, 2018, 2025, Oracle and/or its affiliates.  All rights reserved.
// This software is dual-licensed to you under the Universal Permissive License (UPL) 1.0 as shown at https://oss.oracle.com/licenses/upl or Apache License 2.0 as shown at http://www.apache.org/licenses/LICENSE-2.0. You may choose either license.

package auth

import (
	"crypto/rsa"
	"fmt"

	"github.com/oracle/oci-go-sdk/v65/common"
)

type resourcePrincipalDelegationTokenConfigurationProvider struct {
	resourcePrincipalKeyProvider ConfigurationProviderWithClaimAccess
	delegationToken              string
	region                       *common.Region
}

func (r resourcePrincipalDelegationTokenConfigurationProvider) PrivateRSAKey() (*rsa.PrivateKey, error) {
	return r.resourcePrincipalKeyProvider.PrivateRSAKey()
}

func (r resourcePrincipalDelegationTokenConfigurationProvider) KeyID() (string, error) {
	return r.resourcePrincipalKeyProvider.KeyID()
}

func (r resourcePrincipalDelegationTokenConfigurationProvider) TenancyOCID() (string, error) {
	return r.resourcePrincipalKeyProvider.TenancyOCID()
}

func (r resourcePrincipalDelegationTokenConfigurationProvider) UserOCID() (string, error) {
	return "", nil
}

func (r resourcePrincipalDelegationTokenConfigurationProvider) KeyFingerprint() (string, error) {
	return "", nil
}

func (r resourcePrincipalDelegationTokenConfigurationProvider) Region() (string, error) {
	if r.region == nil {
		common.Debugf("Region in resource principal delegation token configuration provider is nil. Returning configuration provider region: %s", r.region)
		return r.resourcePrincipalKeyProvider.Region()
	}
	return string(*r.region), nil
}

func (r resourcePrincipalDelegationTokenConfigurationProvider) AuthType() (common.AuthConfig, error) {
	token := r.delegationToken
	return common.AuthConfig{AuthType: common.ResourcePrincipalDelegationToken, OboToken: &token}, nil
}

func (r resourcePrincipalDelegationTokenConfigurationProvider) GetClaim(key string) (interface{}, error) {
	return r.resourcePrincipalKeyProvider.GetClaim(key)
}

type resourcePrincipalDelegationTokenError struct {
	err error
}

func (rpe resourcePrincipalDelegationTokenError) Error() string {
	return fmt.Sprintf("%s\nResource principals delegation token authentication can only be used on specific OCI services. Please confirm this code is running on the correct environment", rpe.err.Error())
}

// ResourcePrincipalDelegationTokenConfigurationProvider returns a configuration for obo token resource principals
func ResourcePrincipalDelegationTokenConfigurationProvider(delegationToken *string) (ConfigurationProviderWithClaimAccess, error) {
	if delegationToken == nil || len(*delegationToken) == 0 {
		return nil, resourcePrincipalDelegationTokenError{err: fmt.Errorf("failed to create a delagationTokenConfigurationProvider: token is a mandatory input parameter")}
	}
	return newResourcePrincipalDelegationTokenConfigurationProvider(delegationToken, "", nil)
}

// ResourcePrincipalDelegationTokenConfigurationProviderForRegion returns a configuration for obo token resource principals with a given region
func ResourcePrincipalDelegationTokenConfigurationProviderForRegion(delegationToken *string, region common.Region) (ConfigurationProviderWithClaimAccess, error) {
	if delegationToken == nil || len(*delegationToken) == 0 {
		return nil, resourcePrincipalDelegationTokenError{err: fmt.Errorf("failed to create a delagationTokenConfigurationProvider: token is a mandatory input parameter")}
	}
	return newResourcePrincipalDelegationTokenConfigurationProvider(delegationToken, region, nil)
}

func newResourcePrincipalDelegationTokenConfigurationProvider(delegationToken *string, region common.Region, modifier func(common.HTTPRequestDispatcher) (common.HTTPRequestDispatcher, error)) (ConfigurationProviderWithClaimAccess, error) {

	keyProvider, err := ResourcePrincipalConfigurationProvider()
	if err != nil {
		return nil, resourcePrincipalDelegationTokenError{err: fmt.Errorf("failed to create a new key provider for resource principal: %s", err.Error())}
	}
	if len(region) > 0 {
		return resourcePrincipalDelegationTokenConfigurationProvider{keyProvider, *delegationToken, &region}, err
	}
	return resourcePrincipalDelegationTokenConfigurationProvider{keyProvider, *delegationToken, nil}, err
}
