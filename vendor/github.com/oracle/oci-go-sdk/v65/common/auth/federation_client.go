// Copyright (c) 2016, 2018, 2026, Oracle and/or its affiliates.  All rights reserved.
// This software is dual-licensed to you under the Universal Permissive License (UPL) 1.0 as shown at https://oss.oracle.com/licenses/upl or Apache License 2.0 as shown at http://www.apache.org/licenses/LICENSE-2.0. You may choose either license.

// Package auth provides supporting functions and structs for authentication
package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"maps"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
)

// federationClient is a client to retrieve the security token for an instance principal necessary to sign a request.
// It also provides the private key whose corresponding public key is used to retrieve the security token.
type federationClient interface {
	ClaimHolder
	PrivateKey() (*rsa.PrivateKey, error)
	SecurityToken() (string, error)
}

// ClaimHolder is implemented by any token interface that provides access to the security claims embedded in the token.
type ClaimHolder interface {
	GetClaim(key string) (interface{}, error)
}

type genericFederationClient struct {
	SessionKeySupplier   sessionKeySupplier
	RefreshSecurityToken func() (securityToken, error)

	securityToken securityToken
	mux           sync.Mutex
}

var _ federationClient = &genericFederationClient{}

func (c *genericFederationClient) PrivateKey() (*rsa.PrivateKey, error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	if err := c.renewKeyAndSecurityTokenIfNotValid(); err != nil {
		return nil, err
	}
	return c.SessionKeySupplier.PrivateKey(), nil
}

func (c *genericFederationClient) SecurityToken() (token string, err error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	if err = c.renewKeyAndSecurityTokenIfNotValid(); err != nil {
		return "", err
	}
	return c.securityToken.String(), nil
}

func (c *genericFederationClient) renewKeyAndSecurityTokenIfNotValid() (err error) {
	if c.securityToken == nil || !c.securityToken.Valid() {
		if err = c.renewKeyAndSecurityToken(); err != nil {
			return fmt.Errorf("failed to renew security token: %s", err.Error())
		}
	}
	return nil
}

func (c *genericFederationClient) renewKeyAndSecurityToken() (err error) {
	common.Logf("Renewing keys for file based security token at: %v\n", time.Now().Format("15:04:05.000"))
	if err = c.SessionKeySupplier.Refresh(); err != nil {
		return fmt.Errorf("failed to refresh session key: %s", err.Error())
	}

	common.Logf("Renewing security token at: %v\n", time.Now().Format("15:04:05.000"))
	if c.securityToken, err = c.RefreshSecurityToken(); err != nil {
		return fmt.Errorf("failed to refresh security token key: %s", err.Error())
	}
	common.Logf("Security token renewed at: %v\n", time.Now().Format("15:04:05.000"))
	return nil
}

func (c *genericFederationClient) GetClaim(key string) (interface{}, error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	if err := c.renewKeyAndSecurityTokenIfNotValid(); err != nil {
		return nil, err
	}
	return c.securityToken.GetClaim(key)
}

func newFileBasedFederationClient(securityTokenPath string, supplier sessionKeySupplier) (*genericFederationClient, error) {
	return &genericFederationClient{
		SessionKeySupplier: supplier,
		RefreshSecurityToken: func() (token securityToken, err error) {
			var content []byte
			if content, err = ioutil.ReadFile(securityTokenPath); err != nil {
				return nil, fmt.Errorf("failed to read security token from :%s. Due to: %s", securityTokenPath, err.Error())
			}

			var newToken securityToken
			if newToken, err = newPrincipalToken(string(content)); err != nil {
				return nil, fmt.Errorf("failed to read security token from :%s. Due to: %s", securityTokenPath, err.Error())
			}

			return newToken, nil
		},
	}, nil
}

func newStaticFederationClient(sessionToken string, supplier sessionKeySupplier) (*genericFederationClient, error) {
	var newToken securityToken
	var err error
	if newToken, err = newPrincipalToken(string(sessionToken)); err != nil {
		return nil, fmt.Errorf("failed to read security token. Due to: %s", err.Error())
	}

	return &genericFederationClient{
		SessionKeySupplier: supplier,
		RefreshSecurityToken: func() (token securityToken, err error) {
			return newToken, nil
		},
	}, nil
}

// oAuth2FederationClient retrieves a security token from the scoped OAuth endpoint in Auth Service
type oAuth2FederationClient struct {
	sessionKeySupplier    cacheableSessionKeySupplier
	authClientKeyProvider common.KeyProvider
	authClient            *common.BaseClient
	securityToken         securityToken
	lastRefresh           time.Time
	scope                 string
	targetCompartment     string
	mux                   sync.Mutex
}

var OAuthTokenStaleWindow = 20 * time.Minute

// newOAuth2FederationClient creates a new oAuth2FederationClient from the provided configProvider and Auth request parameters
func newOAuth2FederationClient(configProvider common.ConfigurationProvider, scope string, targetCompartment string, sessionKeySupplier cacheableSessionKeySupplier) (federationClient, error) {
	client := &oAuth2FederationClient{}
	client.sessionKeySupplier = sessionKeySupplier
	region, err := configProvider.Region()
	if err != nil {
		return nil, fmt.Errorf("failed to build OAuth Federation Client: %s", err.Error())
	}
	authClient := newAuthClient(common.StringToRegion(region), configProvider, "v1/oauth2/scoped")
	client.authClient = authClient
	client.authClientKeyProvider = configProvider
	client.scope = scope
	client.targetCompartment = targetCompartment
	return client, nil
}

// KeyID calls the KeyID method of the auth provider given to the federation client
func (c *oAuth2FederationClient) KeyID() (string, error) {
	return c.authClientKeyProvider.KeyID()
}

// PrivateRSAKey calls the PrivateRSAKey method of the auth provider given to the federation client
func (c *oAuth2FederationClient) PrivateRSAKey() (*rsa.PrivateKey, error) {
	return c.authClientKeyProvider.PrivateRSAKey()
}

func (c *oAuth2FederationClient) GetClaim(key string) (interface{}, error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	if err := c.renewKeyAndSecurityTokenIfNotValid(); err != nil {
		return nil, err
	}
	return c.securityToken.GetClaim(key)
}

// isTokenStale returns true if the JWT token is older than OAuthTokenStaleWindow
func (c *oAuth2FederationClient) isTokenStale() bool {
	return c.lastRefresh.IsZero() || time.Now().After(c.lastRefresh.Add(OAuthTokenStaleWindow))
}

func (c *oAuth2FederationClient) renewKeyAndSecurityTokenIfNotValid() (err error) {
	return c.renewSecurityTokenIfNotValid()
}

func (c *oAuth2FederationClient) renewSecurityTokenIfNotValid() (err error) {

	// Get a new token if this one is stale (or nil), even if it is still valid
	if c.securityToken == nil || c.isTokenStale() {
		if err = c.renewSecurityToken(); err != nil {
			if c.securityToken != nil && c.securityToken.Valid() {
				// Token is stale but still valid. We failed to get a new token
				// but we can still use the old one
				common.Debugln("failed to refresh OAuth token. Using valid cached token  and cached session keys")
				c.sessionKeySupplier.Revert()
				return nil
			}

			return fmt.Errorf("failed to refresh token: %s", err.Error())
		}
	}

	// Token exists and is not stale,
	// or token was stale and a new one was retrieved
	return nil
}

func (c *oAuth2FederationClient) renewSecurityToken() (err error) {
	if err = c.sessionKeySupplier.Refresh(); err != nil {
		return fmt.Errorf("failed to refresh session key: %s", err.Error())
	}

	common.Logf("Renewing security token at: %v\n", time.Now().Format("15:04:05.000"))
	if newToken, err := c.getSecurityToken(); err != nil {
		return fmt.Errorf("failed to get security token: %s", err.Error())
	} else {
		// only update token if a new one was retrieved.
		c.lastRefresh = time.Now()
		c.securityToken = newToken
	}

	common.Logf("Security token renewed at: %v\n", time.Now().Format("15:04:05.000"))

	return nil

}

func (c *oAuth2FederationClient) getSecurityToken() (securityToken, error) {
	var err error
	var httpRequest http.Request
	var httpResponse *http.Response
	defer common.CloseBodyIfValid(httpResponse)
	for retry := 0; retry < 3; retry++ {
		request := c.makeOAuthFederationRequest()

		if httpRequest, err = common.MakeDefaultHTTPRequestWithTaggedStruct(http.MethodPost, "", request); err != nil {
			return nil, fmt.Errorf("failed to make http request: %s", err.Error())
		}

		if httpResponse, err = c.authClient.Call(context.Background(), &httpRequest); err == nil {
			break
		}
		// Don't retry on 4xx errors
		if httpResponse != nil && httpResponse.StatusCode >= 400 && httpResponse.StatusCode <= 499 {
			return nil, fmt.Errorf("error %s returned by auth service: %s", httpResponse.Status, err.Error())
		}
		nextDuration := time.Duration(1000.0*(math.Pow(2.0, float64(retry)))) * time.Millisecond
		time.Sleep(nextDuration)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to call: %s", err.Error())
	}

	response := oAuthFederationResponse{}
	if err = common.UnmarshalResponse(httpResponse, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal the response: %s", err.Error())
	}

	return newPrincipalToken(response.Token.Token)

}

type oAuthFederationRequest struct {
	OAuthFederationDetails `contributesTo:"body"`
}

// OAuthFederationDetails Scoped Oauth federation details
// The scope type should correspond to the type of config provider used to create
// the OAuth Federation Client
type OAuthFederationDetails struct {
	Scope             string `mandatory:"true" json:"scope,omitempty"`
	PublicKey         string `mandatory:"true" json:"public_key,omitempty"`
	TargetCompartment string `mandatory:"true" json:"target_compartment,omitempty"`
}

type oAuthFederationResponse struct {
	Token `presentIn:"body"`
}

func (c *oAuth2FederationClient) makeOAuthFederationRequest() *oAuthFederationRequest {
	publicKey := sanitizeCertificateString(string(c.sessionKeySupplier.PublicKeyPemRaw()))
	details := OAuthFederationDetails{
		Scope:             c.scope,
		PublicKey:         publicKey,
		TargetCompartment: c.targetCompartment,
	}
	return &oAuthFederationRequest{details}
}

func (c *oAuth2FederationClient) PrivateKey() (*rsa.PrivateKey, error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	if err := c.renewSecurityTokenIfNotValid(); err != nil {
		return nil, err
	}
	return c.sessionKeySupplier.PrivateKey(), nil
}

func (c *oAuth2FederationClient) SecurityToken() (token string, err error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	if err = c.renewSecurityTokenIfNotValid(); err != nil {
		return "", err
	}
	return c.securityToken.String(), nil
}

// x509FederationClient retrieves a security token from Auth service.
type x509FederationClient struct {
	tenancyID                         string
	sessionKeySupplier                sessionKeySupplier
	leafCertificateRetriever          x509CertificateRetriever
	intermediateCertificateRetrievers []x509CertificateRetriever
	securityToken                     securityToken
	authClient                        *common.BaseClient
	mux                               sync.Mutex
}

func newX509FederationClient(region common.Region, tenancyID string, leafCertificateRetriever x509CertificateRetriever, intermediateCertificateRetrievers []x509CertificateRetriever, modifier dispatcherModifier) (federationClient, error) {
	client := &x509FederationClient{
		tenancyID:                         tenancyID,
		leafCertificateRetriever:          leafCertificateRetriever,
		intermediateCertificateRetrievers: intermediateCertificateRetrievers,
	}
	client.sessionKeySupplier = newSessionKeySupplier()
	authClient := newAuthClient(region, client, "v1/x509")

	var err error

	if authClient.HTTPClient, err = modifier.Modify(authClient.HTTPClient); err != nil {
		err = fmt.Errorf("failed to modify client: %s", err.Error())
		return nil, err
	}

	client.authClient = authClient
	return client, nil
}

func newX509FederationClientWithCerts(region common.Region, tenancyID string, leafCertificate, leafPassphrase, leafPrivateKey []byte, intermediateCertificates [][]byte, modifier dispatcherModifier) (federationClient, error) {
	intermediateRetrievers := make([]x509CertificateRetriever, len(intermediateCertificates))
	for i, c := range intermediateCertificates {
		intermediateRetrievers[i] = &staticCertificateRetriever{Passphrase: []byte(""), CertificatePem: c, PrivateKeyPem: nil}
	}

	client := &x509FederationClient{
		tenancyID:                         tenancyID,
		leafCertificateRetriever:          &staticCertificateRetriever{Passphrase: leafPassphrase, CertificatePem: leafCertificate, PrivateKeyPem: leafPrivateKey},
		intermediateCertificateRetrievers: intermediateRetrievers,
	}
	client.sessionKeySupplier = newSessionKeySupplier()
	authClient := newAuthClient(region, client, "v1/x509")

	var err error

	if authClient.HTTPClient, err = modifier.Modify(authClient.HTTPClient); err != nil {
		err = fmt.Errorf("failed to modify client: %s", err.Error())
		return nil, err
	}

	client.authClient = authClient
	return client, nil
}

var (
	genericHeaders = []string{"date", "(request-target)"} // "host" is not needed for the federation endpoint.  Don't ask me why.
	bodyHeaders    = []string{"content-length", "content-type", "x-content-sha256"}
)

func newAuthClient(region common.Region, provider common.KeyProvider, authBasePath string) *common.BaseClient {
	signer := common.RequestSigner(provider, genericHeaders, bodyHeaders)
	client := common.DefaultBaseClientWithSigner(signer)

	if regionURL, ok := os.LookupEnv("OCI_SDK_AUTH_CLIENT_REGION_URL"); ok {
		client.Host = regionURL
	} else {
		client.Host = region.Endpoint("auth")
	}
	client.BasePath = authBasePath

	if common.GlobalAuthClientCircuitBreakerSetting != nil {
		client.Configuration.CircuitBreaker = common.NewCircuitBreaker(common.GlobalAuthClientCircuitBreakerSetting)
	} else if !common.IsEnvVarFalse("OCI_SDK_AUTH_CLIENT_CIRCUIT_BREAKER_ENABLED") {
		common.Logf("Configuring DefaultAuthClientCircuitBreakerSetting for federation client")
		client.Configuration.CircuitBreaker = common.NewCircuitBreaker(common.DefaultAuthClientCircuitBreakerSetting())
	}
	return &client
}

// For authClient to sign requests to X509 Federation Endpoint
func (c *x509FederationClient) KeyID() (string, error) {
	tenancy := c.tenancyID
	fingerprint := fingerprint(c.leafCertificateRetriever.Certificate())
	return fmt.Sprintf("%s/fed-x509-sha256/%s", tenancy, fingerprint), nil
}

// For authClient to sign requests to X509 Federation Endpoint
func (c *x509FederationClient) PrivateRSAKey() (*rsa.PrivateKey, error) {
	key := c.leafCertificateRetriever.PrivateKey()
	if key == nil {
		return nil, fmt.Errorf("can not read private key from leaf certificate. Likely an error in the metadata service")
	}

	return key, nil
}

func (c *x509FederationClient) PrivateKey() (*rsa.PrivateKey, error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	if err := c.renewSecurityTokenIfNotValid(); err != nil {
		return nil, err
	}
	return c.sessionKeySupplier.PrivateKey(), nil
}

func (c *x509FederationClient) SecurityToken() (token string, err error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	if err = c.renewSecurityTokenIfNotValid(); err != nil {
		return "", err
	}
	return c.securityToken.String(), nil
}

func (c *x509FederationClient) renewSecurityTokenIfNotValid() (err error) {
	if c.securityToken == nil || !c.securityToken.Valid() {
		if err = c.renewSecurityToken(); err != nil {
			return fmt.Errorf("failed to renew security token: %s", err.Error())
		}
	}
	return nil
}

func (c *x509FederationClient) renewSecurityToken() (err error) {
	if err = c.sessionKeySupplier.Refresh(); err != nil {
		return fmt.Errorf("failed to refresh session key: %s", err.Error())
	}

	if err = c.leafCertificateRetriever.Refresh(); err != nil {
		return fmt.Errorf("failed to refresh leaf certificate: %s", err.Error())
	}

	updatedTenancyID := extractTenancyIDFromCertificate(c.leafCertificateRetriever.Certificate())
	if c.tenancyID != updatedTenancyID {
		err = fmt.Errorf("unexpected update of tenancy OCID in the leaf certificate. Previous tenancy: %s, Updated: %s", c.tenancyID, updatedTenancyID)
		return
	}

	for _, retriever := range c.intermediateCertificateRetrievers {
		if err = retriever.Refresh(); err != nil {
			return fmt.Errorf("failed to refresh intermediate certificate: %s", err.Error())
		}
	}

	common.Logf("Renewing security token at: %v\n", time.Now().Format("15:04:05.000"))
	if c.securityToken, err = c.getSecurityToken(); err != nil {
		return fmt.Errorf("failed to get security token: %s", err.Error())
	}
	common.Logf("Security token renewed at: %v\n", time.Now().Format("15:04:05.000"))

	return nil
}

func (c *x509FederationClient) getSecurityToken() (securityToken, error) {
	var err error
	var httpRequest http.Request
	var httpResponse *http.Response
	defer common.CloseBodyIfValid(httpResponse)

	for retry := 0; retry < 3; retry++ {
		request := c.makeX509FederationRequest()

		if httpRequest, err = common.MakeDefaultHTTPRequestWithTaggedStruct(http.MethodPost, "", request); err != nil {
			return nil, fmt.Errorf("failed to make http request: %s", err.Error())
		}

		if httpResponse, err = c.authClient.Call(context.Background(), &httpRequest); err == nil {
			break
		}
		// Don't retry on 4xx errors
		if httpResponse != nil && httpResponse.StatusCode >= 400 && httpResponse.StatusCode <= 499 {
			return nil, fmt.Errorf("error %s returned by auth service: %s", httpResponse.Status, err.Error())
		}
		nextDuration := time.Duration(1000.0*(math.Pow(2.0, float64(retry)))) * time.Millisecond
		time.Sleep(nextDuration)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to call: %s", err.Error())
	}

	response := x509FederationResponse{}
	if err = common.UnmarshalResponse(httpResponse, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal the response: %s", err.Error())
	}

	return newPrincipalToken(response.Token.Token)
}

func (c *x509FederationClient) GetClaim(key string) (interface{}, error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	if err := c.renewSecurityTokenIfNotValid(); err != nil {
		return nil, err
	}
	return c.securityToken.GetClaim(key)
}

type x509FederationRequest struct {
	X509FederationDetails `contributesTo:"body"`
}

// X509FederationDetails x509 federation details
type X509FederationDetails struct {
	Certificate              string   `mandatory:"true" json:"certificate,omitempty"`
	PublicKey                string   `mandatory:"true" json:"publicKey,omitempty"`
	IntermediateCertificates []string `mandatory:"false" json:"intermediateCertificates,omitempty"`
	FingerprintAlgorithm     string   `mandatory:"false" json:"fingerprintAlgorithm,omitempty"`
}

type x509FederationResponse struct {
	Token `presentIn:"body"`
}

// Token token
type Token struct {
	Token string `mandatory:"true" json:"token,omitempty"`
}

func (c *x509FederationClient) makeX509FederationRequest() *x509FederationRequest {
	certificate := c.sanitizeCertificateString(string(c.leafCertificateRetriever.CertificatePemRaw()))
	publicKey := c.sanitizeCertificateString(string(c.sessionKeySupplier.PublicKeyPemRaw()))
	var intermediateCertificates []string
	for _, retriever := range c.intermediateCertificateRetrievers {
		intermediateCertificates = append(intermediateCertificates, c.sanitizeCertificateString(string(retriever.CertificatePemRaw())))
	}

	details := X509FederationDetails{
		Certificate:              certificate,
		PublicKey:                publicKey,
		IntermediateCertificates: intermediateCertificates,
		FingerprintAlgorithm:     "SHA256",
	}
	return &x509FederationRequest{details}
}

func (c *x509FederationClient) sanitizeCertificateString(certString string) string {
	certString = strings.Replace(certString, "-----BEGIN CERTIFICATE-----", "", -1)
	certString = strings.Replace(certString, "-----END CERTIFICATE-----", "", -1)
	certString = strings.Replace(certString, "-----BEGIN PUBLIC KEY-----", "", -1)
	certString = strings.Replace(certString, "-----END PUBLIC KEY-----", "", -1)
	certString = strings.Replace(certString, "\n", "", -1)
	return certString
}

// sessionKeySupplier provides an RSA keypair which can be re-generated by calling Refresh().
type sessionKeySupplier interface {
	Refresh() error
	PrivateKey() *rsa.PrivateKey
	PublicKeyPemRaw() []byte
}

// cacheableSessionKeySupplier extends sessionKeySupplier with the ability to revert to the previous key pair.
type cacheableSessionKeySupplier interface {
	sessionKeySupplier
	Revert()
}

// genericKeySupplier implements sessionKeySupplier and provides an arbitrary refresh mechanism
type genericKeySupplier struct {
	RefreshFn func() (*rsa.PrivateKey, []byte, error)

	privateKey      *rsa.PrivateKey
	publicKeyPemRaw []byte
}

func (s genericKeySupplier) PrivateKey() *rsa.PrivateKey {
	if s.privateKey == nil {
		return nil
	}

	c := *s.privateKey
	return &c
}

func (s genericKeySupplier) PublicKeyPemRaw() []byte {
	if s.publicKeyPemRaw == nil {
		return nil
	}

	c := make([]byte, len(s.publicKeyPemRaw))
	copy(c, s.publicKeyPemRaw)
	return c
}

func (s *genericKeySupplier) Refresh() (err error) {
	privateKey, publicPem, err := s.RefreshFn()
	if err != nil {
		return err
	}

	s.privateKey = privateKey
	s.publicKeyPemRaw = publicPem
	return nil
}

// create a sessionKeySupplier that reads keys from file every time it refreshes
func newFileBasedKeySessionSupplier(privateKeyPemPath string, passphrasePath *string) (*genericKeySupplier, error) {
	return &genericKeySupplier{
		RefreshFn: func() (*rsa.PrivateKey, []byte, error) {
			var err error
			var passContent []byte
			if passphrasePath != nil {
				if passContent, err = ioutil.ReadFile(*passphrasePath); err != nil {
					return nil, nil, fmt.Errorf("can not read passphrase from file: %s, due to %s", *passphrasePath, err.Error())
				}
			}

			var keyPemContent []byte
			if keyPemContent, err = ioutil.ReadFile(privateKeyPemPath); err != nil {
				return nil, nil, fmt.Errorf("can not read private privateKey pem from file: %s, due to %s", privateKeyPemPath, err.Error())
			}

			var privateKey *rsa.PrivateKey
			if privateKey, err = common.PrivateKeyFromBytesWithPassword(keyPemContent, passContent); err != nil {
				return nil, nil, fmt.Errorf("can not create private privateKey from contents of: %s, due to: %s", privateKeyPemPath, err.Error())
			}

			var publicKeyAsnBytes []byte
			if publicKeyAsnBytes, err = x509.MarshalPKIXPublicKey(privateKey.Public()); err != nil {
				return nil, nil, fmt.Errorf("failed to marshal the public part of the new keypair: %s", err.Error())
			}
			publicKeyPemRaw := pem.EncodeToMemory(&pem.Block{
				Type:  "PUBLIC KEY",
				Bytes: publicKeyAsnBytes,
			})
			return privateKey, publicKeyPemRaw, nil
		},
	}, nil
}

func newStaticKeySessionSupplier(privateKeyPemContent, passphrase []byte) (*genericKeySupplier, error) {
	var err error
	var privateKey *rsa.PrivateKey

	if privateKey, err = common.PrivateKeyFromBytesWithPassword(privateKeyPemContent, passphrase); err != nil {
		return nil, fmt.Errorf("can not create private privateKey, due to: %s", err.Error())
	}

	var publicKeyAsnBytes []byte
	if publicKeyAsnBytes, err = x509.MarshalPKIXPublicKey(privateKey.Public()); err != nil {
		return nil, fmt.Errorf("failed to marshal the public part of the new keypair: %s", err.Error())
	}
	publicKeyPemRaw := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyAsnBytes,
	})

	return &genericKeySupplier{
		RefreshFn: func() (key *rsa.PrivateKey, bytes []byte, err error) {
			return privateKey, publicKeyPemRaw, nil
		},
	}, nil
}

// inMemorySessionKeySupplier implements sessionKeySupplier to vend an RSA keypair.
// Refresh() generates a new RSA keypair with a random source, and keeps it in memory.
//
// inMemorySessionKeySupplier is not thread-safe.
type inMemorySessionKeySupplier struct {
	keySize         int
	privateKey      *rsa.PrivateKey
	publicKeyPemRaw []byte
}

// newSessionKeySupplier creates and returns a sessionKeySupplier instance which generates key pairs of size 2048.
func newSessionKeySupplier() sessionKeySupplier {
	return &inMemorySessionKeySupplier{keySize: 2048}
}

// Refresh() is failure atomic, i.e., PrivateKey() and PublicKeyPemRaw() would return their previous values
// if Refresh() fails.
func (s *inMemorySessionKeySupplier) Refresh() (err error) {
	common.Debugln("Refreshing session key")

	var privateKey *rsa.PrivateKey
	privateKey, err = rsa.GenerateKey(rand.Reader, s.keySize)
	if err != nil {
		return fmt.Errorf("failed to generate a new keypair: %s", err)
	}

	var publicKeyAsnBytes []byte
	if publicKeyAsnBytes, err = x509.MarshalPKIXPublicKey(privateKey.Public()); err != nil {
		return fmt.Errorf("failed to marshal the public part of the new keypair: %s", err.Error())
	}
	publicKeyPemRaw := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyAsnBytes,
	})

	s.privateKey = privateKey
	s.publicKeyPemRaw = publicKeyPemRaw
	return nil
}

func (s *inMemorySessionKeySupplier) PrivateKey() *rsa.PrivateKey {
	if s.privateKey == nil {
		return nil
	}

	c := *s.privateKey
	return &c
}

func (s *inMemorySessionKeySupplier) PublicKeyPemRaw() []byte {
	if s.publicKeyPemRaw == nil {
		return nil
	}

	c := make([]byte, len(s.publicKeyPemRaw))
	copy(c, s.publicKeyPemRaw)
	return c
}

type inMemoryCacheableSessionKeySupplier struct {
	inMemorySessionKeySupplier
	cachedPublicKeyPemRaw []byte
	cachedPrivateKey      *rsa.PrivateKey
}

// newCacheableSessionKeySupplier creates and returns an inMemoryCacheableSessionKeySupplier instance which generates key pairs of size 2048.
func newCacheableSessionKeySupplier() cacheableSessionKeySupplier {
	return &inMemoryCacheableSessionKeySupplier{inMemorySessionKeySupplier: inMemorySessionKeySupplier{keySize: 2048}}
}

func (s *inMemoryCacheableSessionKeySupplier) Refresh() (err error) {

	common.Debugln("Refreshing cacheable session key")

	// Cache current keys before generating new ones
	s.cachedPrivateKey = s.privateKey
	if s.publicKeyPemRaw != nil {
		s.cachedPublicKeyPemRaw = make([]byte, len(s.publicKeyPemRaw))
		copy(s.cachedPublicKeyPemRaw, s.publicKeyPemRaw)
	} else {
		s.cachedPublicKeyPemRaw = nil
	}
	var privateKey *rsa.PrivateKey
	privateKey, err = rsa.GenerateKey(rand.Reader, s.keySize)
	if err != nil {
		return fmt.Errorf("failed to generate a new keypair: %s", err)
	}
	var publicKeyAsnBytes []byte
	if publicKeyAsnBytes, err = x509.MarshalPKIXPublicKey(privateKey.Public()); err != nil {
		return fmt.Errorf("failed to marshal the public part of the new keypair: %s", err.Error())
	}
	publicKeyPemRaw := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyAsnBytes,
	})
	s.privateKey = privateKey
	s.publicKeyPemRaw = publicKeyPemRaw

	return nil
}

func (s *inMemoryCacheableSessionKeySupplier) Revert() {
	s.privateKey = s.cachedPrivateKey
	if s.cachedPublicKeyPemRaw != nil {
		s.publicKeyPemRaw = make([]byte, len(s.cachedPublicKeyPemRaw))
		copy(s.publicKeyPemRaw, s.cachedPublicKeyPemRaw)
	} else {
		s.publicKeyPemRaw = nil
	}
}

type securityToken interface {
	fmt.Stringer
	Valid() bool

	ClaimHolder
}

type principalToken struct {
	tokenString string
	jwtToken    *jwtToken
}

func newPrincipalToken(tokenString string) (newToken securityToken, err error) {
	var jwtToken *jwtToken
	if jwtToken, err = parseJwt(tokenString); err != nil {
		return nil, fmt.Errorf("failed to parse the token string \"%s\": %s", tokenString, err.Error())
	}
	return &principalToken{tokenString, jwtToken}, nil
}

func (t *principalToken) String() string {
	return t.tokenString
}

func (t *principalToken) Valid() bool {
	return !t.jwtToken.expired()
}

var (
	// ErrNoSuchClaim is returned when a token does not hold the claim sought
	ErrNoSuchClaim = errors.New("no such claim")
)

func (t *principalToken) GetClaim(key string) (interface{}, error) {
	if value, ok := t.jwtToken.payload[key]; ok {
		return value, nil
	}
	return nil, ErrNoSuchClaim
}

// nilSigner is required to avoid common.BaseClient panic.
type nilSigner struct{}

// Sign fulfills the HTTPRequestSigner interface.
func (e nilSigner) Sign(r *http.Request) error {
	return nil
}

// newIDAuthClient returns a BaseClient that does not sign requests and has the auth
// client circuit breaker
func newIDAuthClient(host string, authBasePath string) *common.BaseClient {
	client := common.DefaultBaseClientWithSigner(nilSigner{})
	client.Host = host
	client.BasePath = authBasePath
	if common.GlobalAuthClientCircuitBreakerSetting != nil {
		client.Configuration.CircuitBreaker = common.NewCircuitBreaker(common.GlobalAuthClientCircuitBreakerSetting)
	} else if !common.IsEnvVarFalse("OCI_SDK_AUTH_CLIENT_CIRCUIT_BREAKER_ENABLED") {
		common.Logf("Configuring DefaultAuthClientCircuitBreakerSetting for federation client")
		client.Configuration.CircuitBreaker = common.NewCircuitBreaker(common.DefaultAuthClientCircuitBreakerSetting())
	}
	return &client
}

// tokenExchangeResponse provides a struct for unmarshaling tokens.
type tokenExchangeResponse struct {
	Token `presentIn:"body"`
}

// tokenExchangeFederationClient implements federationClient.
type tokenExchangeFederationClient struct {
	client                    *common.BaseClient
	securityToken             securityToken
	privateKey                *rsa.PrivateKey
	tokenIssuer               TokenIssuer
	domainUrl                 string
	authCode                  string
	requestData               map[string][]string
	instancePrincipalProvider common.ConfigurationProvider
	mux                       sync.Mutex
}

// newTokenExchangeFederationClient creates a federation client.
func newTokenExchangeFederationClient(issuer TokenIssuer, host string,
	authCode string, requestData map[string][]string,
	instancePrincipalProvider common.ConfigurationProvider) *tokenExchangeFederationClient {
	defaultGenericHeaders := []string{"date", "(request-target)", "host"}
	bodyHeaders := []string{"content-length", "content-type", "x-content-sha256"}
	var client *common.BaseClient
	if instancePrincipalProvider != nil {
		signer := common.RequestSigner(instancePrincipalProvider, defaultGenericHeaders, bodyHeaders)
		baseClient := common.DefaultBaseClientWithSigner(signer)
		client = &baseClient
		client.Host = host
		client.BasePath = "oauth2/v1/token"
	} else {
		client = newIDAuthClient(host, "/oauth2/v1/token")
	}
	fc := tokenExchangeFederationClient{
		tokenIssuer:               issuer,
		client:                    client,
		authCode:                  authCode,
		requestData:               requestData,
		instancePrincipalProvider: instancePrincipalProvider,
	}
	return &fc
}

// PrivateKey receiver implements federationClient interface. Safe for concurrent use.
func (fc *tokenExchangeFederationClient) PrivateKey() (*rsa.PrivateKey, error) {
	if err := fc.renewSecurityTokenIfNotValid(); err != nil {
		return nil, err
	}
	return fc.privateKey, nil
}

// SecurityToken receiver implements federationClient interface. Safe for concurrent
// use.
func (fc *tokenExchangeFederationClient) SecurityToken() (string, error) {
	if err := fc.renewSecurityTokenIfNotValid(); err != nil {
		return "", err
	}
	return fmt.Sprintf("ST$%s", fc.securityToken.String()), nil
}

// GetClaim returns claims embedded in the Security Token.
func (fc *tokenExchangeFederationClient) GetClaim(key string) (interface{}, error) {
	if err := fc.renewSecurityTokenIfNotValid(); err != nil {
		return nil, fmt.Errorf("unable to retrieve claim: %w", err)
	}
	return fc.securityToken.GetClaim(key)
}

// renewSecurityTokenIfNotValid checks if token is valid and initiates refresh if needed.
// Mutex is locked here if an operation is needed to prevent concurrency errors.
func (fc *tokenExchangeFederationClient) renewSecurityTokenIfNotValid() error {
	if fc.securityToken == nil || !fc.securityToken.Valid() {
		// Lock here to prevent renewSecurityToken from making surplus calls to the
		// authorization server and identity domain
		fc.mux.Lock()
		defer fc.mux.Unlock()
		// Ensure token is not renewed by previously blocked operation
		if fc.securityToken != nil && fc.securityToken.Valid() {
			return nil
		}
		return fc.renewSecurityToken()
	}
	return nil
}

// renewSecurityToken initiates renewal of the Security Token returned by the
// tokenExchangeFederationClient. Should only be called by renewSecurityTokenIfNotValid.
// Rotates RSA key and updates federation client with fresh Security Token and private key.
func (fc *tokenExchangeFederationClient) renewSecurityToken() (err error) {
	var token string
	// Since we are running arbitrary code, we catch panics and return the cause
	// as an error
	func() {
		// Scope recover around caller-provided code
		common.Logf("attempting to retrieve token from issuer")
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic occurred during token renewal: %v", r)
			}
		}()
		// Get a fresh token from the issuer
		token, err = fc.tokenIssuer.GetToken()
	}()
	if err != nil {
		return fmt.Errorf("unable to refresh JWT: %w", err)
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return fmt.Errorf("unable to generate RSA key: %w", err)
	}
	publicKey, err := privateToPublicDERBase64(privateKey)
	if err != nil {
		return fmt.Errorf("unable to derive public key: %w", err)
	}
	securityToken, err := fc.newTokenExchangeToken(token, publicKey)
	if err != nil {
		return fmt.Errorf("unable to exchange JWT for security token: %w", err)
	}
	// privateKey and securityToken ONLY updated here while under lock from renewSecurityTokenIfNotValid
	fc.privateKey = privateKey
	fc.securityToken = securityToken
	return nil
}

// newTokenExchangeToken assembles and returns a tokenExchangeToken issued by OCI.
func (fc *tokenExchangeFederationClient) newTokenExchangeToken(token string,
	publicKey string) (tokenExchangeToken, error) {
	var t tokenExchangeToken
	var err error
	// Retry and backoff
	maxRetries := 3
	var httpResponse *http.Response
	defer common.CloseBodyIfValid(httpResponse)
	for retry := 1; retry <= maxRetries; retry++ {
		common.Logf("attempt %d to retrieve Security Token", retry)
		form := make(url.Values, 0)
		maps.Copy(form, fc.requestData)
		form.Set("public_key", publicKey)
		if token != "" {
			form.Set("subject_token", token)
		}
		formString := form.Encode()
		formBody := strings.NewReader(formString)
		httpRequest, err := http.NewRequest(http.MethodPost, fc.client.Host, formBody)
		if err != nil {
			return t, fmt.Errorf("failed to make request to token endpoint: %w", err)
		}
		httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if fc.instancePrincipalProvider != nil {
			httpRequest.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
		} else if fc.authCode != "" {
			httpRequest.Header.Set("Authorization", "Basic "+fc.authCode)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		response, err := fc.client.Call(ctx, httpRequest)
		if (err == nil && response.StatusCode == http.StatusOK) ||
			// Do not retry 4XX response codes
			(response != nil && response.StatusCode >= 400 && response.StatusCode <= 499) ||
			// Skip last sleep on max attempts
			(retry == maxRetries) {
			httpResponse = response
			cancel()
			break
		}
		if response != nil {
			common.Logf("invalid response from domain: %s", response.Status)
		} else {
			common.Logf("invalid response from domain: %v", err)
		}
		common.CloseBodyIfValid(response)
		cancel()
		sleep := time.Duration(1000.0*(math.Pow(2.0, float64(retry)))) * time.Millisecond
		time.Sleep(sleep)
	}
	if httpResponse == nil {
		return t, fmt.Errorf("no response from domain")
	}
	if httpResponse.StatusCode != http.StatusOK {
		return t, fmt.Errorf("invalid token endpoint response %s", httpResponse.Status)
	}
	responseBody := tokenExchangeResponse{}
	if err = common.UnmarshalResponse(httpResponse, &responseBody); err != nil {
		return t, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	parsedToken, err := parseJwt(responseBody.Token.Token)
	if err != nil {
		return t, fmt.Errorf("unable to parse token: %w", err)
	}
	t.token = *parsedToken
	return t, nil
}
