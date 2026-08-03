// Copyright (c) 2016, 2018, 2026, Oracle and/or its affiliates.  All rights reserved.
// This software is dual-licensed to you under the Universal Permissive License (UPL) 1.0 as shown at https://oss.oracle.com/licenses/upl or Apache License 2.0 as shown at http://www.apache.org/licenses/LICENSE-2.0. You may choose either license.

package auth

import (
	"bytes"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/common/utils"
)

const (
	rpstValidForRatio float64 = 0.5
)

// Workload RPST Issuance Service (WRIS)
// x509FederationClientForOkeWorkloadIdentity retrieves a security token from Auth service.
type x509FederationClientForOkeWorkloadIdentity struct {
	tenancyID                    string
	sessionKeySupplier           sessionKeySupplier
	securityToken                securityToken
	authClient                   *common.BaseClient
	httpClient                   *http.Client
	mux                          sync.Mutex
	proxymuxEndpoint             string
	saTokenProvider              ServiceAccountTokenProvider
	kubernetesServiceAccountCert *x509.CertPool
}

func newX509FederationClientForOkeWorkloadIdentity(endpoint string, saTokenProvider ServiceAccountTokenProvider,
	kubernetesServiceAccountCert *x509.CertPool) (federationClient, error) {
	client := &x509FederationClientForOkeWorkloadIdentity{
		proxymuxEndpoint:             endpoint,
		saTokenProvider:              saTokenProvider,
		kubernetesServiceAccountCert: kubernetesServiceAccountCert,
	}

	client.sessionKeySupplier = newSessionKeySupplier()
	client.httpClient = newOkeWorkloadIdentityHTTPClient(kubernetesServiceAccountCert)

	return client, nil
}

func newOkeWorkloadIdentityHTTPClient(kubernetesServiceAccountCert *x509.CertPool) *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: kubernetesServiceAccountCert,
			},
		},
	}
}

func (c *x509FederationClientForOkeWorkloadIdentity) proxymuxHTTPClient() *http.Client {
	if c.httpClient == nil {
		c.httpClient = newOkeWorkloadIdentityHTTPClient(c.kubernetesServiceAccountCert)
	}
	return c.httpClient
}

func (c *x509FederationClientForOkeWorkloadIdentity) renewSecurityToken() (err error) {
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

type workloadIdentityRequestPayload struct {
	Podkey string `json:"podKey"`
}
type token struct {
	Token string
}

// getSecurityToken get security token from Proxymux
func (c *x509FederationClientForOkeWorkloadIdentity) getSecurityToken() (securityToken, error) {
	publicKey := string(c.sessionKeySupplier.PublicKeyPemRaw())
	rawPayload := workloadIdentityRequestPayload{Podkey: publicKey}
	payload, err := json.Marshal(rawPayload)
	if err != nil {
		return nil, fmt.Errorf("error getting security token%s", err)
	}

	request, err := http.NewRequest(http.MethodPost, c.proxymuxEndpoint, bytes.NewBuffer(payload))

	if err != nil {
		common.Logf("error %s", err)
		return nil, fmt.Errorf("error getting security token %s", err)
	}

	kubernetesServiceAccountToken, err := c.saTokenProvider.ServiceAccountToken()
	if err != nil {
		common.Logf("error %s", err)
		return nil, fmt.Errorf("error getting service account token %s", err)
	}

	request.Header.Add("Authorization", "Bearer "+kubernetesServiceAccountToken)
	request.Header.Set("Content-Type", "application/json")
	opcRequestID := utils.GenerateOpcRequestID()
	request.Header.Set("opc-request-id", opcRequestID)

	response, err := c.proxymuxHTTPClient().Do(request)
	if err != nil {
		return nil, fmt.Errorf("error %s", err)
	}

	var body bytes.Buffer
	defer func(body io.ReadCloser) {
		err := body.Close()
		if err != nil {
			common.Logf("error %s", err)
		}
	}(response.Body)

	// Ensure body is read before returning, to allow connection reuse.
	if _, err = body.ReadFrom(response.Body); err != nil {
		return nil, fmt.Errorf("error reading Workload Identity token generation response: %s. Please contact OKE team", err)
	}

	statusCode := response.StatusCode
	if statusCode != http.StatusOK {
		if statusCode == http.StatusForbidden {
			return nil, fmt.Errorf("please ensure the cluster type is enhanced: Status: %s, Message: %s",
				response.Status, body.String())
		} else {
			return nil, fmt.Errorf("failed to get a Workload Identity token. Status: %s, Message: %s. Please contact OKE team",
				response.Status, body.String())
		}

	}

	rawBody := body.String()
	rawBody = rawBody[1 : len(rawBody)-1]
	decodedBodyStr, err := base64.StdEncoding.DecodeString(rawBody)
	if err != nil {
		return nil, fmt.Errorf("error decoding Workload Identity token: %s. Please contact OKE team", err)
	}

	var parsedBody token
	err = json.Unmarshal(decodedBodyStr, &parsedBody)
	if err != nil {
		return nil, fmt.Errorf("error parsing Workload Identity token: %s. Please contact OKE team", err)
	}

	token := parsedBody.Token
	if len(token) == 0 {
		return nil, fmt.Errorf("invalid (empty) Workload Identity token received. Please contact OKE team")
	}
	if len(token) < 3 {
		return nil, fmt.Errorf("invalid Workload Identity token received. Please contact OKE team")
	}

	return newPrincipalToken(token[3:])
}

func (c *x509FederationClientForOkeWorkloadIdentity) PrivateKey() (*rsa.PrivateKey, error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	if err := c.renewSecurityTokenIfNotValid(); err != nil {
		return nil, err
	}
	return c.sessionKeySupplier.PrivateKey(), nil
}

func (c *x509FederationClientForOkeWorkloadIdentity) SecurityToken() (token string, err error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	if err = c.renewSecurityTokenIfNotValid(); err != nil {
		return "", err
	}
	return c.securityToken.String(), nil
}

func (c *x509FederationClientForOkeWorkloadIdentity) renewSecurityTokenIfNotValid() (err error) {
	if c.securityToken == nil || !c.securityToken.Valid() {
		if err = c.renewSecurityToken(); err != nil {
			return fmt.Errorf("failed to renew security token: %s", err.Error())
		}
	}
	return nil
}

type workloadIdentityPrincipalToken struct {
	principalToken
}

func (t *workloadIdentityPrincipalToken) Valid() bool {
	// TODO: read rpstValidForRatio from rpst token
	issuedAt := int64(t.jwtToken.payload["iat"].(float64))
	expiredAt := int64(t.jwtToken.payload["exp"].(float64))
	softExpiredAt := issuedAt + int64(float64(expiredAt-issuedAt)*rpstValidForRatio)
	softExpiredAtTime := time.Unix(softExpiredAt, 0)
	now := time.Now().Unix() + int64(bufferTimeBeforeTokenExpiration.Seconds())
	expired := softExpiredAt <= now
	if expired {
		common.Debugf("Token expired at: %v", softExpiredAtTime.Format("15:04:05.000"))
	}
	return !expired
}

func (c *x509FederationClientForOkeWorkloadIdentity) GetClaim(key string) (interface{}, error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	if err := c.renewSecurityTokenIfNotValid(); err != nil {
		return nil, err
	}
	return c.securityToken.GetClaim(key)
}
