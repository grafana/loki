// Copyright (c) 2016, 2018, 2025, Oracle and/or its affiliates.  All rights reserved.
// This software is dual-licensed to you under the Universal Permissive License (UPL) 1.0 as shown at https://oss.oracle.com/licenses/upl or Apache License 2.0 as shown at http://www.apache.org/licenses/LICENSE-2.0. You may choose either license.

package auth

import (
	"context"
	"crypto/rsa"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
)

type resourcePrincipalV3Client struct {
	securityToken      securityToken
	mux                sync.Mutex
	sessionKeySupplier sessionKeySupplier
	rptUrl             string
	rpstUrl            string

	leafResourcePrincipalKeyProvider ConfigurationProviderWithClaimAccess

	//ResourcePrincipalTargetServiceClient client that calls the target service to acquire a resource principal token
	//ResourcePrincipalTargetServiceClient common.BaseClient

	//ResourcePrincipalSessionTokenClient. The client used to communicate with identity to exchange a resource principal for
	// resource principal session token
	//ResourcePrincipalSessionTokenClient common.BaseClient
}

// acquireResourcePrincipalToken acquires the resource principal from the target service
func (c *resourcePrincipalV3Client) acquireResourcePrincipalToken(rptClient common.BaseClient, path string, signer common.HTTPRequestSigner) (tokenResponse resourcePrincipalTokenResponse, parentRptURL string, err error) {
	rpServiceClient := rptClient
	rpServiceClient.Signer = signer

	//Create a request with the instanceId
	request := common.MakeDefaultHTTPRequest(http.MethodGet, path)

	//Call the target service
	response, err := rpServiceClient.Call(context.Background(), &request)
	if err != nil {
		return
	}

	defer common.CloseBodyIfValid(response)

	// Extract the opc-parent-rpt-url header value
	parentRptURL = response.Header.Get(OpcParentRptUrlHeader)

	tokenResponse = resourcePrincipalTokenResponse{}
	err = common.UnmarshalResponse(response, &tokenResponse)
	return
}

// exchangeToken exchanges a resource principal token from the target service with a session token from identity
func (c *resourcePrincipalV3Client) exchangeToken(rpstClient common.BaseClient, signer common.HTTPRequestSigner, publicKeyBase64 string, tokenResponse resourcePrincipalTokenResponse) (sessionToken string, err error) {
	rpServiceClient := rpstClient
	rpServiceClient.Signer = signer

	// Call identity service to get resource principal session token
	sessionTokenReq := resourcePrincipalSessionTokenRequest{
		resourcePrincipalSessionTokenRequestBody{
			ServicePrincipalSessionToken: tokenResponse.Body.ServicePrincipalSessionToken,
			ResourcePrincipalToken:       tokenResponse.Body.ResourcePrincipalToken,
			SessionPublicKey:             publicKeyBase64,
		},
	}

	sessionTokenHTTPReq, err := common.MakeDefaultHTTPRequestWithTaggedStruct(http.MethodPost,
		"", sessionTokenReq)
	if err != nil {
		return
	}

	sessionTokenHTTPRes, err := rpServiceClient.Call(context.Background(), &sessionTokenHTTPReq)
	if err != nil {
		return
	}
	defer common.CloseBodyIfValid(sessionTokenHTTPRes)

	sessionTokenRes := x509FederationResponse{}
	err = common.UnmarshalResponse(sessionTokenHTTPRes, &sessionTokenRes)
	if err != nil {
		return
	}

	sessionToken = sessionTokenRes.Token.Token
	return
}

// getSecurityToken makes the appropriate calls to acquire a resource principal security token
func (c *resourcePrincipalV3Client) getSecurityToken() (securityToken, error) {

	//c.leafResourcePrincipalKeyProvider.KeyID()
	//common.Debugf("Refreshing resource principal token")

	//Read the public key from the session supplier.
	pem := c.sessionKeySupplier.PublicKeyPemRaw()
	pemSanitized := sanitizeCertificateString(string(pem))

	return c.getSecurityTokenWithDepth(c.leafResourcePrincipalKeyProvider, 1, c.rptUrl, pemSanitized)

}

func (c *resourcePrincipalV3Client) getSecurityTokenWithDepth(keyProvider ConfigurationProviderWithClaimAccess, depth int, rptUrl, publicKey string) (securityToken, error) {
	//Build the target service client
	rpTargetServiceClient, err := common.NewClientWithConfig(keyProvider)
	if err != nil {
		return nil, err
	}

	rpTokenURL, err := url.Parse(rptUrl)
	if err != nil {
		return nil, err
	}

	common.Debugf("rptURL: %v", rpTokenURL)

	rpTargetServiceClient.Host = rpTokenURL.Scheme + "://" + rpTokenURL.Host

	//Build the identity client for token service
	rpTokenSessionClient, err := common.NewClientWithConfig(keyProvider)
	if err != nil {
		return nil, err
	}

	// Set RPST endpoint if passed in from env var, otherwise create it from region
	if c.rpstUrl != "" {
		rpSessionTokenURL, err := url.Parse(c.rpstUrl)
		if err != nil {
			return nil, err
		}

		rpTokenSessionClient.Host = rpSessionTokenURL.Scheme + "://" + rpSessionTokenURL.Host
	} else {
		regionStr, err := c.leafResourcePrincipalKeyProvider.Region()
		if err != nil {
			return nil, fmt.Errorf("missing RPST env var and cannot determine region: %v", err)
		}
		region := common.StringToRegion(regionStr)
		rpTokenSessionClient.Host = fmt.Sprintf("https://%s", region.Endpoint("auth"))
	}

	rpTokenSessionClient.BasePath = identityResourcePrincipalSessionTokenPath

	//Acquire resource principal token from target service
	common.Debugf("Acquiring resource principal token from target service")
	tokenResponse, parentRptURL, err := c.acquireResourcePrincipalToken(rpTargetServiceClient, rpTokenURL.Path, common.DefaultRequestSigner(keyProvider))
	if err != nil {
		return nil, err
	}

	//Exchange resource principal token for session token from identity
	common.Debugf("Exchanging resource principal token for resource principal session token")
	sessionToken, err := c.exchangeToken(rpTokenSessionClient, common.DefaultRequestSigner(keyProvider), publicKey, tokenResponse)
	if err != nil {
		return nil, err
	}

	// Base condition for recursion
	// return the security token obtained last in the following cases
	// 1. if depth is more than 10
	// 2. if opc-parent-rpt-url header is not passed or is empty
	// 3. if opc-parent-rpt-url matches the last rpt url
	if depth >= 10 || parentRptURL == "" || strings.EqualFold(parentRptURL, rptUrl) {
		return newPrincipalToken(sessionToken)
	}

	fd, err := newStaticFederationClient(sessionToken, c.sessionKeySupplier)

	if err != nil {
		err := fmt.Errorf("can not create resource principal, due to: %s ", err.Error())
		return nil, resourcePrincipalError{err: err}
	}

	region, _ := keyProvider.Region()

	configProviderForNextCall := resourcePrincipalKeyProvider{
		fd, common.Region(region),
	}

	return c.getSecurityTokenWithDepth(&configProviderForNextCall, depth+1, parentRptURL, publicKey)

}

func (c *resourcePrincipalV3Client) renewSecurityToken() (err error) {
	if err = c.sessionKeySupplier.Refresh(); err != nil {
		return fmt.Errorf("failed to refresh session key: %s", err.Error())
	}

	common.Logf("Renewing security token at: %v\n", time.Now().Format("15:04:05.000"))
	if c.securityToken, err = c.getSecurityToken(); err != nil {
		return fmt.Errorf("failed to get security token: %s", err.Error())
	}
	common.Logf("Security token renewed at: %v\n", time.Now().Format("15:04:05.000"))

	return nil
}

func (c *resourcePrincipalV3Client) renewSecurityTokenIfNotValid() (err error) {
	if c.securityToken == nil || !c.securityToken.Valid() {
		if err = c.renewSecurityToken(); err != nil {
			return fmt.Errorf("failed to renew resource principal security token: %s", err.Error())
		}
	}
	return nil
}

func (c *resourcePrincipalV3Client) PrivateKey() (*rsa.PrivateKey, error) {
	c.mux.Lock()
	defer c.mux.Unlock()
	if err := c.renewSecurityTokenIfNotValid(); err != nil {
		return nil, err
	}
	return c.sessionKeySupplier.PrivateKey(), nil
}

func (c *resourcePrincipalV3Client) SecurityToken() (token string, err error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	if err = c.renewSecurityTokenIfNotValid(); err != nil {
		return "", err
	}
	return c.securityToken.String(), nil
}

type resourcePrincipalKeyProviderV3 struct {
	resourcePrincipalClient resourcePrincipalV3Client
}

type resourcePrincipalV30ConfigurationProvider struct {
	keyProvider resourcePrincipalKeyProviderV3
	region      *common.Region
}

func (r *resourcePrincipalV30ConfigurationProvider) Refreshable() bool {
	return true
}

func (r *resourcePrincipalV30ConfigurationProvider) PrivateRSAKey() (*rsa.PrivateKey, error) {
	privateKey, err := r.keyProvider.resourcePrincipalClient.PrivateKey()
	if err != nil {
		err = fmt.Errorf("failed to get resource principal private key: %s", err.Error())
		return nil, err
	}
	return privateKey, nil
}

func (r *resourcePrincipalV30ConfigurationProvider) KeyID() (string, error) {
	var securityToken string
	var err error
	if securityToken, err = r.keyProvider.resourcePrincipalClient.SecurityToken(); err != nil {
		return "", fmt.Errorf("failed to get resource principal security token: %s", err.Error())
	}
	return fmt.Sprintf("ST$%s", securityToken), nil
}

func (r *resourcePrincipalV30ConfigurationProvider) TenancyOCID() (string, error) {
	return r.keyProvider.resourcePrincipalClient.leafResourcePrincipalKeyProvider.TenancyOCID()
}

func (r *resourcePrincipalV30ConfigurationProvider) UserOCID() (string, error) {
	return "", nil
}

func (r *resourcePrincipalV30ConfigurationProvider) KeyFingerprint() (string, error) {
	return "", nil
}

func (r *resourcePrincipalV30ConfigurationProvider) Region() (string, error) {
	if r.region == nil {
		common.Debugf("Region in resource principal configuration provider v30 is nil.")
		return "", nil
	}
	return string(*r.region), nil
}

func (r *resourcePrincipalV30ConfigurationProvider) AuthType() (common.AuthConfig, error) {
	return common.AuthConfig{common.UnknownAuthenticationType, false, nil},
		fmt.Errorf("unsupported, keep the interface")
}

func (r *resourcePrincipalV30ConfigurationProvider) GetClaim(key string) (interface{}, error) {
	//TODO implement me
	panic("implement me")
}

type resourcePrincipalV30ConfiguratorBuilder struct {
	leafResourcePrincipalKeyProvider  ConfigurationProviderWithClaimAccess
	rptUrlForParent, rpstUrlForParent *string
}

// ResourcePrincipalV3ConfiguratorBuilder creates a new resourcePrincipalV30ConfiguratorBuilder.
func ResourcePrincipalV3ConfiguratorBuilder(leafResourcePrincipalKeyProvider ConfigurationProviderWithClaimAccess) *resourcePrincipalV30ConfiguratorBuilder {
	return &resourcePrincipalV30ConfiguratorBuilder{
		leafResourcePrincipalKeyProvider: leafResourcePrincipalKeyProvider,
	}
}

// WithParentRPTURL sets the rptUrlForParent field.
func (b *resourcePrincipalV30ConfiguratorBuilder) WithParentRPTURL(rptUrlForParent string) *resourcePrincipalV30ConfiguratorBuilder {
	b.rptUrlForParent = &rptUrlForParent
	return b
}

// WithParentRPSTURL sets the rpstUrlForParent field.
func (b *resourcePrincipalV30ConfiguratorBuilder) WithParentRPSTURL(rpstUrlForParent string) *resourcePrincipalV30ConfiguratorBuilder {
	b.rpstUrlForParent = &rpstUrlForParent
	return b
}

// Build creates a ConfigurationProviderWithClaimAccess based on the configured values.
func (b *resourcePrincipalV30ConfiguratorBuilder) Build() (ConfigurationProviderWithClaimAccess, error) {

	if b.rptUrlForParent == nil {
		err := fmt.Errorf("can not create resource principal, environment variable: %s, not present",
			ResourcePrincipalRptURLForParent)
		return nil, resourcePrincipalError{err: err}
	}

	if b.rpstUrlForParent == nil {
		common.Debugf("Environment variable %s not present, setting to empty string", ResourcePrincipalRpstEndpointForParent)
		*b.rpstUrlForParent = ""
	}

	rpFedClient := resourcePrincipalV3Client{}
	rpFedClient.rptUrl = *b.rptUrlForParent
	rpFedClient.rpstUrl = *b.rpstUrlForParent
	rpFedClient.sessionKeySupplier = newSessionKeySupplier()
	rpFedClient.leafResourcePrincipalKeyProvider = b.leafResourcePrincipalKeyProvider
	region, _ := b.leafResourcePrincipalKeyProvider.Region()

	return &resourcePrincipalV30ConfigurationProvider{
		keyProvider: resourcePrincipalKeyProviderV3{rpFedClient},
		region:      (*common.Region)(&region),
	}, nil
}

// ResourcePrincipalConfigurationProviderV3 ResourcePrincipalConfigurationProvider is a function that creates and configures a resource principal.
func ResourcePrincipalConfigurationProviderV3(leafResourcePrincipalKeyProvider ConfigurationProviderWithClaimAccess) (ConfigurationProviderWithClaimAccess, error) {
	builder := ResourcePrincipalV3ConfiguratorBuilder(leafResourcePrincipalKeyProvider)
	builder.rptUrlForParent = requireEnv(ResourcePrincipalRptURLForParent)
	builder.rpstUrlForParent = requireEnv(ResourcePrincipalRpstEndpointForParent)
	return builder.Build()
}
