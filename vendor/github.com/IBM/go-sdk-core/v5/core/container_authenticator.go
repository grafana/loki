package core

// (C) Copyright IBM Corp. 2021, 2025.
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
	"crypto/tls"
	"encoding/json"
	"errors"
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
// retrieves a "compute resource token" from the local compute resource (IKS pod, or Code Engine application, function, or job)
// and uses that to obtain an IAM access token by invoking the IAM "get token" operation with grant-type=cr-token.
// The resulting IAM access token is then added to outbound requests in an Authorization header
// of the form:
//
//	Authorization: Bearer <access-token>
type ContainerAuthenticator struct {
	// [optional] The name of the file containing the injected CR token value (applies to
	// IKS-managed compute resources, a Code Engine compute resource always uses the third default from below).
	// Default value: (1) "/var/run/secrets/tokens/vault-token" or (2) "/var/run/secrets/tokens/sa-token" or (3) "/var/run/secrets/codeengine.cloud.ibm.com/compute-resource-token/token",
	// whichever is found first.
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

	// The User-Agent header value to be included with each token request.
	userAgent     string
	userAgentInit sync.Once

	// The cached IAM access token and its expiration time.
	tokenData *iamTokenData

	// Mutex to synchronize access to the tokenData field.
	tokenDataMutex sync.Mutex
}

const (
	defaultCRTokenFilename1 = "/var/run/secrets/tokens/vault-token"                                    // #nosec G101
	defaultCRTokenFilename2 = "/var/run/secrets/tokens/sa-token"                                       // #nosec G101
	defaultCRTokenFilename3 = "/var/run/secrets/codeengine.cloud.ibm.com/compute-resource-token/token" // #nosec G101
	iamGrantTypeCRToken     = "urn:ibm:params:oauth:grant-type:cr-token"                               // #nosec G101
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
		return nil, RepurposeSDKProblem(err, "validation-failed")
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
					Proxy:           http.ProxyFromEnvironment,
				}
				authenticator.Client.Transport = transport
			}
		}
	})
	return authenticator.Client
}

// getUserAgent returns the User-Agent header value to be included in each token request invoked by the authenticator.
func (authenticator *ContainerAuthenticator) getUserAgent() string {
	authenticator.userAgentInit.Do(func() {
		authenticator.userAgent = fmt.Sprintf("%s/%s-%s %s", sdkName, "container-authenticator", __VERSION__, SystemInfo())
	})
	return authenticator.userAgent
}

// newContainerAuthenticatorFromMap constructs a new ContainerAuthenticator instance from a map containing
// configuration properties.
func newContainerAuthenticatorFromMap(properties map[string]string) (authenticator *ContainerAuthenticator, err error) {
	if properties == nil {
		err := errors.New(ERRORMSG_PROPS_MAP_NIL)
		return nil, SDKErrorf(err, "", "missing-props", getComponentInfo())
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
		return RepurposeSDKProblem(err, "get-token-fail")
	}

	request.Header.Set("Authorization", "Bearer "+token)
	GetLogger().Debug("Authenticated outbound request (type=%s)\n", authenticator.AuthenticationType())
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
		err := fmt.Errorf(ERRORMSG_ATLEAST_ONE_PROP_ERROR, "IAMProfileName", "IAMProfileID")
		return SDKErrorf(err, "", "both-props", getComponentInfo())
	}

	// Validate ClientId and ClientSecret.  They must both be specified togther or neither should be specified.
	if authenticator.ClientID == "" && authenticator.ClientSecret == "" {
		// Do nothing as this is the valid scenario
	} else {
		// Since it is NOT the case that both properties are empty, make sure BOTH are specified.
		if authenticator.ClientID == "" {
			err := fmt.Errorf(ERRORMSG_PROP_MISSING, "ClientID")
			return SDKErrorf(err, "", "missing-id", getComponentInfo())
		}

		if authenticator.ClientSecret == "" {
			err := fmt.Errorf(ERRORMSG_PROP_MISSING, "ClientSecret")
			return SDKErrorf(err, "", "missing-secret", getComponentInfo())
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
			return "", RepurposeSDKProblem(err, "request-token-fail")
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
		err := fmt.Errorf("Error while trying to get access token")
		return "", SDKErrorf(err, "", "no-token", getComponentInfo())
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
		return nil, authenticationErrorf(err, &DetailedResponse{}, "noop", getComponentInfo())
	}

	// Set up the request for the IAM "get token" invocation.
	builder := NewRequestBuilder(POST)
	_, err = builder.ResolveRequestURL(authenticator.url(), iamAuthOperationPathGetToken, nil)
	if err != nil {
		return nil, authenticationErrorf(err, &DetailedResponse{}, "noop", getComponentInfo())
	}

	builder.AddHeader(CONTENT_TYPE, FORM_URL_ENCODED_HEADER)
	builder.AddHeader(Accept, APPLICATION_JSON)
	builder.AddHeader(headerNameUserAgent, authenticator.getUserAgent())
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
		return nil, authenticationErrorf(err, &DetailedResponse{}, "noop", getComponentInfo())
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
		return nil, authenticationErrorf(err, &DetailedResponse{}, "noop", getComponentInfo())
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
		detailedResponse, err := processErrorResponse(resp)
		authError := authenticationErrorf(err, detailedResponse, "get_token", authenticator.getComponentInfo())

		// The err Summary is typically the message computed for the HTTPError instance in
		// processErrorResponse(). If the response body is non-JSON, the message will be generic
		// text based on the status code but authenticators have always used the stringified
		// RawResult, so update that here for compatilibility.
		iamErrorMsg := err.Summary
		if detailedResponse.RawResult != nil {
			// RawResult is only populated if the response body is
			// non-JSON and we couldn't extract a message.
			iamErrorMsg = string(detailedResponse.RawResult)
		}

		authError.Summary = fmt.Sprintf(ERRORMSG_IAM_GETTOKEN_ERROR, detailedResponse.StatusCode, builder.URL, iamErrorMsg)

		return nil, authError
	}

	// Good response, so unmarshal the response body into an IamTokenServerResponse instance.
	tokenResponse := &IamTokenServerResponse{}
	_ = json.NewDecoder(resp.Body).Decode(tokenResponse)
	defer resp.Body.Close() // #nosec G307

	return tokenResponse, nil
}

// retrieveCRToken tries to read the CR token value from the local file system.
func (authenticator *ContainerAuthenticator) retrieveCRToken() (crToken string, err error) {
	if authenticator.CRTokenFilename != "" {
		// Use the file specified by the user.
		crToken, err = authenticator.readFile(authenticator.CRTokenFilename)
	} else {
		// If the user didn't specify a filename, try our two defaults.
		crToken, err = authenticator.readFile(defaultCRTokenFilename1)
		if err != nil {
			crToken, err = authenticator.readFile(defaultCRTokenFilename2)
			if err != nil {
				crToken, err = authenticator.readFile(defaultCRTokenFilename3)
			}
		}
	}

	if err != nil {
		err = fmt.Errorf(ERRORMSG_UNABLE_RETRIEVE_CRTOKEN, err.Error())
		sdkErr := SDKErrorf(err, "", "no-cr-token", getComponentInfo())
		GetLogger().Debug(sdkErr.GetDebugMessage())
		err = sdkErr
		return
	}

	return
}

// readFile attempts to read the specified cr token file and return its contents as a string.
func (authenticator *ContainerAuthenticator) readFile(filename string) (crToken string, err error) {
	GetLogger().Debug("Attempting to read CR token from file: %s\n", filename)

	// Read the entire file into a byte slice, then convert to string.
	var bytes []byte
	bytes, err = os.ReadFile(filename) // #nosec G304
	if err != nil {
		err = SDKErrorf(err, "", "read-file-error", getComponentInfo())
		GetLogger().Debug(err.(*SDKProblem).GetDebugMessage())
		return
	}

	GetLogger().Debug("Successfully read CR token from file: %s\n", filename)

	crToken = string(bytes)

	return
}

// This should only be used for AuthenticationError instances that actually deal with
// an HTTP error (i.e. do not have a blank DetailedResponse object - they can be scoped
// to the SDK core system).
func (authenticator *ContainerAuthenticator) getComponentInfo() *ProblemComponent {
	return NewProblemComponent("iam_identity_services", "")
}
