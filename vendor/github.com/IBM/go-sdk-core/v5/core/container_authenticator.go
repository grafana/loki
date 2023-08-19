package core

// (C) Copyright IBM Corp. 2021.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ContainerAuthenticator implements an IAM-based authentication schema whereby it
// retrieves a "compute resource token" from the local compute resource (VM)
// and uses that to obtain an IAM access token by invoking the IAM "get token" operation with grant-type=cr-token.
// The resulting IAM access token is then added to outbound requests in an Authorization header
// of the form:
//
//	Authorization: Bearer <access-token>
type ContainerAuthenticator struct {

	// [optional] The name of the file containing the injected CR token value (applies to
	// IKS-managed compute resources).
	// Default value: "/var/run/secrets/tokens/vault-token"
	CRTokenFilename string

	// [optional] The name of the linked trusted IAM profile to be used when obtaining the IAM access token.
	// One of IAMProfileName or IAMProfileID must be specified.
	// Default value: ""
	IAMProfileName string

	// [optional] The id of the linked trusted IAM profile to be used when obtaining the IAM access token.
	// One of IAMProfileName or IAMProfileID must be specified.
	// Default value: ""
	IAMProfileID string

	// [optional] The IAM token server's base endpoint URL.
	// Default value: "https://iam.cloud.ibm.com"
	URL     string
	urlInit sync.Once

	// [optional] The ClientID and ClientSecret fields are used to form a "basic auth"
	// Authorization header for interactions with the IAM token server.
	// If neither field is specified, then no Authorization header will be sent
	// with token server requests.
	// These fields are both optional, but must be specified together.
	// Default value: ""
	ClientID     string
	ClientSecret string

	// [optional] A flag that indicates whether verification of the server's SSL certificate
	// should be disabled.
	// Default value: false
	DisableSSLVerification bool

	// [optional] The "scope" to use when fetching the access token from the IAM token server.
	// This can be used to obtain an access token with a specific scope.
	// Default value: ""
	Scope string

	// [optional] A set of key/value pairs that will be sent as HTTP headers in requests
	// made to the IAM token server.
	// Default value: nil
	Headers map[string]string

	// [optional] The http.Client object used in interacts with the IAM token server.
	// If not specified by the user, a suitable default Client will be constructed.
	Client     *http.Client
	clientInit sync.Once

	// The cached IAM access token and its expiration time.
	tokenData *iamTokenData

	// Mutex to synchronize access to the tokenData field.
	tokenDataMutex sync.Mutex
}

const (
	defaultCRTokenFilename = "/var/run/secrets/tokens/vault-token"      // #nosec G101
	iamGrantTypeCRToken    = "urn:ibm:params:oauth:grant-type:cr-token" // #nosec G101
)

var craRequestTokenMutex sync.Mutex

// ContainerAuthenticatorBuilder is used to construct an instance of the ContainerAuthenticator
type ContainerAuthenticatorBuilder struct {
	ContainerAuthenticator
}

// NewContainerAuthenticatorBuilder returns a new builder struct that
// can be used to construct a ContainerAuthenticator instance.
func NewContainerAuthenticatorBuilder() *ContainerAuthenticatorBuilder {
	return &ContainerAuthenticatorBuilder{}
}

// SetCRTokenFilename sets the CRTokenFilename field in the builder.
func (builder *ContainerAuthenticatorBuilder) SetCRTokenFilename(s string) *ContainerAuthenticatorBuilder {
	builder.ContainerAuthenticator.CRTokenFilename = s
	return builder
}

// SetIAMProfileName sets the IAMProfileName field in the builder.
func (builder *ContainerAuthenticatorBuilder) SetIAMProfileName(s string) *ContainerAuthenticatorBuilder {
	builder.ContainerAuthenticator.IAMProfileName = s
	return builder
}

// SetIAMProfileID sets the IAMProfileID field in the builder.
func (builder *ContainerAuthenticatorBuilder) SetIAMProfileID(s string) *ContainerAuthenticatorBuilder {
	builder.ContainerAuthenticator.IAMProfileID = s
	return builder
}

// SetURL sets the URL field in the builder.
func (builder *ContainerAuthenticatorBuilder) SetURL(s string) *ContainerAuthenticatorBuilder {
	builder.ContainerAuthenticator.URL = s
	return builder
}

// SetClientIDSecret sets the ClientID and ClientSecret fields in the builder.
func (builder *ContainerAuthenticatorBuilder) SetClientIDSecret(clientID, clientSecret string) *ContainerAuthenticatorBuilder {
	builder.ContainerAuthenticator.ClientID = clientID
	builder.ContainerAuthenticator.ClientSecret = clientSecret
	return builder
}

// SetDisableSSLVerification sets the DisableSSLVerification field in the builder.
func (builder *ContainerAuthenticatorBuilder) SetDisableSSLVerification(b bool) *ContainerAuthenticatorBuilder {
	builder.ContainerAuthenticator.DisableSSLVerification = b
	return builder
}

// SetScope sets the Scope field in the builder.
func (builder *ContainerAuthenticatorBuilder) SetScope(s string) *ContainerAuthenticatorBuilder {
	builder.ContainerAuthenticator.Scope = s
	return builder
}

// SetHeaders sets the Headers field in the builder.
func (builder *ContainerAuthenticatorBuilder) SetHeaders(headers map[string]string) *ContainerAuthenticatorBuilder {
	builder.ContainerAuthenticator.Headers = headers
	return builder
}

// SetClient sets the Client field in the builder.
func (builder *ContainerAuthenticatorBuilder) SetClient(client *http.Client) *ContainerAuthenticatorBuilder {
	builder.ContainerAuthenticator.Client = client
	return builder
}

// Build() returns a validated instance of the ContainerAuthenticator with the config that was set in the builder.
func (builder *ContainerAuthenticatorBuilder) Build() (*ContainerAuthenticator, error) {

	// Make sure the config is valid.
	err := builder.ContainerAuthenticator.Validate()
	if err != nil {
		return nil, err
	}

	return &builder.ContainerAuthenticator, nil
}

// client returns the authenticator's http client after potentially initializing it.
func (authenticator *ContainerAuthenticator) client() *http.Client {
	authenticator.clientInit.Do(func() {
		if authenticator.Client == nil {
			authenticator.Client = DefaultHTTPClient()
			authenticator.Client.Timeout = time.Second * 30

			// If the user told us to disable SSL verification, then do it now.
			if authenticator.DisableSSLVerification {
				transport := &http.Transport{
					// #nosec G402
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				}
				authenticator.Client.Transport = transport
			}
		}
	})
	return authenticator.Client
}

// newContainerAuthenticatorFromMap constructs a new ContainerAuthenticator instance from a map containing
// configuration properties.
func newContainerAuthenticatorFromMap(properties map[string]string) (authenticator *ContainerAuthenticator, err error) {
	if properties == nil {
		return nil, fmt.Errorf(ERRORMSG_PROPS_MAP_NIL)
	}

	// Grab the AUTH_DISABLE_SSL string property and convert to a boolean value.
	disableSSL, err := strconv.ParseBool(properties[PROPNAME_AUTH_DISABLE_SSL])
	if err != nil {
		disableSSL = false
	}

	authenticator, err = NewContainerAuthenticatorBuilder().
		SetCRTokenFilename(properties[PROPNAME_CRTOKEN_FILENAME]).
		SetIAMProfileName(properties[PROPNAME_IAM_PROFILE_NAME]).
		SetIAMProfileID(properties[PROPNAME_IAM_PROFILE_ID]).
		SetURL(properties[PROPNAME_AUTH_URL]).
		SetClientIDSecret(properties[PROPNAME_CLIENT_ID], properties[PROPNAME_CLIENT_SECRET]).
		SetDisableSSLVerification(disableSSL).
		SetScope(properties[PROPNAME_SCOPE]).
		Build()

	return
}

// AuthenticationType returns the authentication type for this authenticator.
func (*ContainerAuthenticator) AuthenticationType() string {
	return AUTHTYPE_CONTAINER
}

// Authenticate adds IAM authentication information to the request.
//
// The IAM access token will be added to the request's headers in the form:
//
//	Authorization: Bearer <access-token>
func (authenticator *ContainerAuthenticator) Authenticate(request *http.Request) error {
	token, err := authenticator.GetToken()
	if err != nil {
		return err
	}

	request.Header.Set("Authorization", "Bearer "+token)
	return nil
}

// url returns the authenticator's URL property after potentially initializing it.
func (authenticator *ContainerAuthenticator) url() string {
	authenticator.urlInit.Do(func() {
		if authenticator.URL == "" {
			// If URL was not specified, then use the default IAM endpoint.
			authenticator.URL = defaultIamTokenServerEndpoint
		} else {
			// Canonicalize the URL by removing the operation path if it was specified by the user.
			authenticator.URL = strings.TrimSuffix(authenticator.URL, iamAuthOperationPathGetToken)
		}
	})
	return authenticator.URL
}

// getTokenData returns the tokenData field from the authenticator with synchronization.
func (authenticator *ContainerAuthenticator) getTokenData() *iamTokenData {
	authenticator.tokenDataMutex.Lock()
	defer authenticator.tokenDataMutex.Unlock()

	return authenticator.tokenData
}

// setTokenData sets the 'tokenData' field in the authenticator with synchronization.
func (authenticator *ContainerAuthenticator) setTokenData(tokenData *iamTokenData) {
	authenticator.tokenDataMutex.Lock()
	defer authenticator.tokenDataMutex.Unlock()

	authenticator.tokenData = tokenData
}

// Validate the authenticator's configuration.
//
// Ensures that one of IAMProfileName or IAMProfileID are specified, and the ClientId and ClientSecret pair are
// mutually inclusive.
func (authenticator *ContainerAuthenticator) Validate() error {

	// Check to make sure that one of IAMProfileName or IAMProfileID are specified.
	if authenticator.IAMProfileName == "" && authenticator.IAMProfileID == "" {
		return fmt.Errorf(ERRORMSG_ATLEAST_ONE_PROP_ERROR, "IAMProfileName", "IAMProfileID")
	}

	// Validate ClientId and ClientSecret.  They must both be specified togther or neither should be specified.
	if authenticator.ClientID == "" && authenticator.ClientSecret == "" {
		// Do nothing as this is the valid scenario
	} else {
		// Since it is NOT the case that both properties are empty, make sure BOTH are specified.
		if authenticator.ClientID == "" {
			return fmt.Errorf(ERRORMSG_PROP_MISSING, "ClientID")
		}

		if authenticator.ClientSecret == "" {
			return fmt.Errorf(ERRORMSG_PROP_MISSING, "ClientSecret")
		}
	}

	return nil
}

// GetToken returns an access token to be used in an Authorization header.
// Whenever a new token is needed (when a token doesn't yet exist or the existing token has expired),
// a new access token is fetched from the token server.
func (authenticator *ContainerAuthenticator) GetToken() (string, error) {
	if authenticator.getTokenData() == nil || !authenticator.getTokenData().isTokenValid() {
		GetLogger().Debug("Performing synchronous token fetch...")
		// synchronously request the token
		err := authenticator.synchronizedRequestToken()
		if err != nil {
			return "", err
		}
	} else if authenticator.getTokenData().needsRefresh() {
		GetLogger().Debug("Performing background asynchronous token fetch...")
		// If refresh needed, kick off a go routine in the background to get a new token
		//nolint: errcheck
		go authenticator.invokeRequestTokenData()
	} else {
		GetLogger().Debug("Using cached access token...")
	}

	// return an error if the access token is not valid or was not fetched
	if authenticator.getTokenData() == nil || authenticator.getTokenData().AccessToken == "" {
		return "", fmt.Errorf("Error while trying to get access token")
	}

	return authenticator.getTokenData().AccessToken, nil
}

// synchronizedRequestToken will check if the authenticator currently has
// a valid cached access token.
// If yes, then nothing else needs to be done.
// If no, then a blocking request is made to obtain a new IAM access token.
func (authenticator *ContainerAuthenticator) synchronizedRequestToken() error {
	craRequestTokenMutex.Lock()
	defer craRequestTokenMutex.Unlock()
	// if cached token is still valid, then just continue to use it
	if authenticator.getTokenData() != nil && authenticator.getTokenData().isTokenValid() {
		return nil
	}

	return authenticator.invokeRequestTokenData()
}

// invokeRequestTokenData requests a new token from the IAM token server and
// unmarshals the response to produce the authenticator's 'tokenData' field (cache).
// Returns an error if the token was unable to be fetched, otherwise returns nil.
func (authenticator *ContainerAuthenticator) invokeRequestTokenData() error {
	tokenResponse, err := authenticator.RequestToken()
	if err != nil {
		return err
	}

	if tokenData, err := newIamTokenData(tokenResponse); err != nil {
		return err
	} else {
		authenticator.setTokenData(tokenData)
	}

	return nil
}

// RequestToken first retrieves a CR token value from the current compute resource, then uses
// that to obtain a new IAM access token from the IAM token server.
func (authenticator *ContainerAuthenticator) RequestToken() (*IamTokenServerResponse, error) {
	var err error

	// First, retrieve the CR token value for this compute resource.
	crToken, err := authenticator.retrieveCRToken()
	if crToken == "" {
		if err == nil {
			err = fmt.Errorf(ERRORMSG_UNABLE_RETRIEVE_CRTOKEN, "reason unknown")
		}
		return nil, NewAuthenticationError(&DetailedResponse{}, err)
	}

	// Set up the request for the IAM "get token" invocation.
	builder := NewRequestBuilder(POST)
	_, err = builder.ResolveRequestURL(authenticator.url(), iamAuthOperationPathGetToken, nil)
	if err != nil {
		return nil, NewAuthenticationError(&DetailedResponse{}, err)
	}

	builder.AddHeader(CONTENT_TYPE, FORM_URL_ENCODED_HEADER)
	builder.AddHeader(Accept, APPLICATION_JSON)
	builder.AddFormData("grant_type", "", "", iamGrantTypeCRToken) // #nosec G101
	builder.AddFormData("cr_token", "", "", crToken)

	// We previously verified that one of IBMProfileID or IAMProfileName are specified,
	// so just process them individually here.
	// If both are specified, that's ok too (they must map to the same profile though).
	if authenticator.IAMProfileID != "" {
		builder.AddFormData("profile_id", "", "", authenticator.IAMProfileID)
	}
	if authenticator.IAMProfileName != "" {
		builder.AddFormData("profile_name", "", "", authenticator.IAMProfileName)
	}

	// If the scope was specified, add that form param to the request.
	if authenticator.Scope != "" {
		builder.AddFormData("scope", "", "", authenticator.Scope)
	}

	// Add user-defined headers to request.
	for headerName, headerValue := range authenticator.Headers {
		builder.AddHeader(headerName, headerValue)
	}

	req, err := builder.Build()
	if err != nil {
		return nil, NewAuthenticationError(&DetailedResponse{}, err)
	}

	// If client id and secret were configured by the user, then set them on the request
	// as a basic auth header.
	if authenticator.ClientID != "" && authenticator.ClientSecret != "" {
		req.SetBasicAuth(authenticator.ClientID, authenticator.ClientSecret)
	}

	// If debug is enabled, then dump the request.
	if GetLogger().IsLogLevelEnabled(LevelDebug) {
		buf, dumpErr := httputil.DumpRequestOut(req, req.Body != nil)
		if dumpErr == nil {
			GetLogger().Debug("Request:\n%s\n", RedactSecrets(string(buf)))
		} else {
			GetLogger().Debug(fmt.Sprintf("error while attempting to log outbound request: %s", dumpErr.Error()))
		}
	}

	GetLogger().Debug("Invoking IAM 'get token' operation: %s", builder.URL)
	resp, err := authenticator.client().Do(req)
	if err != nil {
		return nil, NewAuthenticationError(&DetailedResponse{}, err)
	}
	GetLogger().Debug("Returned from IAM 'get token' operation, received status code %d", resp.StatusCode)

	// If debug is enabled, then dump the response.
	if GetLogger().IsLogLevelEnabled(LevelDebug) {
		buf, dumpErr := httputil.DumpResponse(resp, req.Body != nil)
		if dumpErr == nil {
			GetLogger().Debug("Response:\n%s\n", RedactSecrets(string(buf)))
		} else {
			GetLogger().Debug(fmt.Sprintf("error while attempting to log inbound response: %s", dumpErr.Error()))
		}
	}

	// Check for a bad status code and handle an operation error.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buff := new(bytes.Buffer)
		_, _ = buff.ReadFrom(resp.Body)
		resp.Body.Close() // #nosec G104

		// Create a DetailedResponse to be included in the error below.
		detailedResponse := &DetailedResponse{
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			RawResult:  buff.Bytes(),
		}

		iamErrorMsg := string(detailedResponse.RawResult)
		if iamErrorMsg == "" {
			iamErrorMsg = "IAM error response not available"
		}
		err = fmt.Errorf(ERRORMSG_IAM_GETTOKEN_ERROR, detailedResponse.StatusCode, builder.URL, iamErrorMsg)
		return nil, NewAuthenticationError(detailedResponse, err)
	}

	// Good response, so unmarshal the response body into an IamTokenServerResponse instance.
	tokenResponse := &IamTokenServerResponse{}
	_ = json.NewDecoder(resp.Body).Decode(tokenResponse)
	defer resp.Body.Close() // #nosec G307

	return tokenResponse, nil
}

// retrieveCRToken tries to read the CR token value from the local file system.
func (authenticator *ContainerAuthenticator) retrieveCRToken() (crToken string, err error) {

	// Use the default filename if one wasn't supplied by the user.
	crTokenFilename := authenticator.CRTokenFilename
	if crTokenFilename == "" {
		crTokenFilename = defaultCRTokenFilename
	}

	GetLogger().Debug("Attempting to read CR token from file: %s\n", crTokenFilename)

	// Read the entire file into a byte slice, then convert to string.
	var bytes []byte
	bytes, err = os.ReadFile(crTokenFilename) // #nosec G304
	if err != nil {
		err = fmt.Errorf(ERRORMSG_UNABLE_RETRIEVE_CRTOKEN, err.Error())
		GetLogger().Debug(err.Error())
		return
	}

	crToken = string(bytes)
	GetLogger().Debug("Successfully read CR token from file: %s\n", crTokenFilename)

	return
}
