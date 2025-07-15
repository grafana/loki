package core

// (C) Copyright IBM Corp. 2019, 2024.
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
	"strconv"
	"sync"
	"time"
)

// CloudPakForDataAuthenticator uses either a username/password pair or a
// username/apikey pair to obtain a suitable bearer token from the CP4D authentication service,
// and adds the bearer token to requests via an Authorization header of the form:
//
//	Authorization: Bearer <bearer-token>
type CloudPakForDataAuthenticator struct {
	// The URL representing the Cloud Pak for Data token service endpoint [required].
	URL string

	// The username used to obtain a bearer token [required].
	Username string

	// The password used to obtain a bearer token [required if APIKey not specified].
	// One of Password or APIKey must be specified.
	Password string

	// The apikey used to obtain a bearer token [required if Password not specified].
	// One of Password or APIKey must be specified.
	APIKey string

	// A flag that indicates whether verification of the server's SSL certificate
	// should be disabled; defaults to false [optional].
	DisableSSLVerification bool

	// Default headers to be sent with every CP4D token request [optional].
	Headers map[string]string

	// The http.Client object used to invoke token server requests [optional]. If
	// not specified, a suitable default Client will be constructed.
	Client     *http.Client
	clientInit sync.Once

	// The User-Agent header value to be included with each token request.
	userAgent     string
	userAgentInit sync.Once

	// The cached token and expiration time.
	tokenData *cp4dTokenData

	// Mutex to make the tokenData field thread safe.
	tokenDataMutex sync.Mutex
}

var cp4dRequestTokenMutex sync.Mutex
var cp4dNeedsRefreshMutex sync.Mutex

// NewCloudPakForDataAuthenticator constructs a new CloudPakForDataAuthenticator
// instance from a username/password pair.
// This is the default way to create an authenticator and is a wrapper around
// the NewCloudPakForDataAuthenticatorUsingPassword() function
func NewCloudPakForDataAuthenticator(url string, username string, password string,
	disableSSLVerification bool, headers map[string]string) (*CloudPakForDataAuthenticator, error) {
	auth, err := NewCloudPakForDataAuthenticatorUsingPassword(url, username, password, disableSSLVerification, headers)
	return auth, RepurposeSDKProblem(err, "new-auth-fail")
}

// NewCloudPakForDataAuthenticatorUsingPassword constructs a new CloudPakForDataAuthenticator
// instance from a username/password pair.
func NewCloudPakForDataAuthenticatorUsingPassword(url string, username string, password string,
	disableSSLVerification bool, headers map[string]string) (*CloudPakForDataAuthenticator, error) {
	auth, err := newAuthenticator(url, username, password, "", disableSSLVerification, headers)
	return auth, RepurposeSDKProblem(err, "new-auth-password-fail")
}

// NewCloudPakForDataAuthenticatorUsingAPIKey constructs a new CloudPakForDataAuthenticator
// instance from a username/apikey pair.
func NewCloudPakForDataAuthenticatorUsingAPIKey(url string, username string, apikey string,
	disableSSLVerification bool, headers map[string]string) (*CloudPakForDataAuthenticator, error) {
	auth, err := newAuthenticator(url, username, "", apikey, disableSSLVerification, headers)
	return auth, RepurposeSDKProblem(err, "new-auth-apikey-fail")
}

func newAuthenticator(url string, username string, password string, apikey string,
	disableSSLVerification bool, headers map[string]string) (authenticator *CloudPakForDataAuthenticator, err error) {
	authenticator = &CloudPakForDataAuthenticator{
		Username:               username,
		Password:               password,
		APIKey:                 apikey,
		URL:                    url,
		DisableSSLVerification: disableSSLVerification,
		Headers:                headers,
	}

	// Make sure the config is valid.
	err = authenticator.Validate()
	if err != nil {
		return nil, err
	}

	return
}

// newCloudPakForDataAuthenticatorFromMap : Constructs a new CloudPakForDataAuthenticator instance from a map.
func newCloudPakForDataAuthenticatorFromMap(properties map[string]string) (*CloudPakForDataAuthenticator, error) {
	if properties == nil {
		err := errors.New(ERRORMSG_PROPS_MAP_NIL)
		return nil, SDKErrorf(err, "", "missing-props", getComponentInfo())
	}

	disableSSL, err := strconv.ParseBool(properties[PROPNAME_AUTH_DISABLE_SSL])
	if err != nil {
		disableSSL = false
	}

	return newAuthenticator(properties[PROPNAME_AUTH_URL],
		properties[PROPNAME_USERNAME], properties[PROPNAME_PASSWORD],
		properties[PROPNAME_APIKEY], disableSSL, nil)
}

// AuthenticationType returns the authentication type for this authenticator.
func (*CloudPakForDataAuthenticator) AuthenticationType() string {
	return AUTHTYPE_CP4D
}

// Validate the authenticator's configuration.
//
// Ensures the username, password, and url are not Nil. Additionally, ensures
// they do not contain invalid characters.
func (authenticator *CloudPakForDataAuthenticator) Validate() error {
	if authenticator.Username == "" {
		err := fmt.Errorf(ERRORMSG_PROP_MISSING, "Username")
		return SDKErrorf(err, "", "no-user", getComponentInfo())
	}

	// The user should specify exactly one of APIKey or Password.
	if (authenticator.APIKey == "" && authenticator.Password == "") ||
		(authenticator.APIKey != "" && authenticator.Password != "") {
		err := fmt.Errorf(ERRORMSG_EXCLUSIVE_PROPS_ERROR, "APIKey", "Password")
		return SDKErrorf(err, "", "exc-props", getComponentInfo())
	}

	if authenticator.URL == "" {
		err := fmt.Errorf(ERRORMSG_PROP_MISSING, "URL")
		return SDKErrorf(err, "", "no-url", getComponentInfo())
	}

	return nil
}

// client returns the authenticator's http client after potentially initializing it.
func (authenticator *CloudPakForDataAuthenticator) client() *http.Client {
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

// getUserAgent returns the User-Agent header value to be included in each token request invoked by the authenticator.
func (authenticator *CloudPakForDataAuthenticator) getUserAgent() string {
	authenticator.userAgentInit.Do(func() {
		authenticator.userAgent = fmt.Sprintf("%s/%s-%s %s", sdkName, "cp4d-authenticator", __VERSION__, SystemInfo())
	})
	return authenticator.userAgent
}

// Authenticate adds the bearer token (obtained from the token server) to the
// specified request.
//
// The CP4D bearer token will be added to the request's headers in the form:
//
//	Authorization: Bearer <bearer-token>
func (authenticator *CloudPakForDataAuthenticator) Authenticate(request *http.Request) error {
	token, err := authenticator.GetToken()
	if err != nil {
		return RepurposeSDKProblem(err, "get-token-fail")
	}

	request.Header.Set("Authorization", fmt.Sprintf(`Bearer %s`, token))
	GetLogger().Debug("Authenticated outbound request (type=%s)\n", authenticator.AuthenticationType())
	return nil
}

// getTokenData returns the tokenData field from the authenticator.
func (authenticator *CloudPakForDataAuthenticator) getTokenData() *cp4dTokenData {
	authenticator.tokenDataMutex.Lock()
	defer authenticator.tokenDataMutex.Unlock()

	return authenticator.tokenData
}

// setTokenData sets the given cp4dTokenData to the tokenData field of the authenticator.
func (authenticator *CloudPakForDataAuthenticator) setTokenData(tokenData *cp4dTokenData) {
	authenticator.tokenDataMutex.Lock()
	defer authenticator.tokenDataMutex.Unlock()

	authenticator.tokenData = tokenData
}

// GetToken: returns an access token to be used in an Authorization header.
// Whenever a new token is needed (when a token doesn't yet exist, needs to be refreshed,
// or the existing token has expired), a new access token is fetched from the token server.
func (authenticator *CloudPakForDataAuthenticator) GetToken() (string, error) {
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

// synchronizedRequestToken: synchronously checks if the current token in cache
// is valid. If token is not valid or does not exist, it will fetch a new token
// and set the tokenRefreshTime
func (authenticator *CloudPakForDataAuthenticator) synchronizedRequestToken() error {
	cp4dRequestTokenMutex.Lock()
	defer cp4dRequestTokenMutex.Unlock()
	// if cached token is still valid, then just continue to use it
	if authenticator.getTokenData() != nil && authenticator.getTokenData().isTokenValid() {
		return nil
	}

	return authenticator.invokeRequestTokenData()
}

// invokeRequestTokenData: requests a new token from the token server and
// unmarshals the token information to the tokenData cache. Returns
// an error if the token was unable to be fetched, otherwise returns nil
func (authenticator *CloudPakForDataAuthenticator) invokeRequestTokenData() error {
	tokenResponse, err := authenticator.requestToken()
	if err != nil {
		authenticator.setTokenData(nil)
		return err
	}

	if tokenData, err := newCp4dTokenData(tokenResponse); err != nil {
		authenticator.setTokenData(nil)
		return err
	} else {
		authenticator.setTokenData(tokenData)
	}

	return nil
}

// cp4dRequestBody is a struct used to model the request body for the "POST /v1/authorize" operation.
// Note: we list both Password and APIKey fields, although exactly one of those will be used for
// a specific invocation of the POST /v1/authorize operation.
type cp4dRequestBody struct {
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
}

// requestToken: fetches a new access token from the token server.
func (authenticator *CloudPakForDataAuthenticator) requestToken() (tokenResponse *cp4dTokenServerResponse, err error) {
	// Create the request body (only one of APIKey or Password should be set
	// on the authenticator so only one of them should end up in the serialized JSON).
	body := &cp4dRequestBody{
		Username: authenticator.Username,
		Password: authenticator.Password,
		APIKey:   authenticator.APIKey,
	}

	builder := NewRequestBuilder(POST)
	_, err = builder.ResolveRequestURL(authenticator.URL, "/v1/authorize", nil)
	if err != nil {
		return
	}

	// Add user-defined headers to request.
	for headerName, headerValue := range authenticator.Headers {
		builder.AddHeader(headerName, headerValue)
	}

	// Add the Content-Type and User-Agent headers.
	builder.AddHeader("Content-Type", "application/json")
	builder.AddHeader(headerNameUserAgent, authenticator.getUserAgent())

	// Add the request body to request.
	_, err = builder.SetBodyContentJSON(body)
	if err != nil {
		return
	}

	// Build the request object.
	req, err := builder.Build()
	if err != nil {
		return
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

	GetLogger().Debug("Invoking CP4D token service operation: %s", builder.URL)
	resp, err := authenticator.client().Do(req)
	if err != nil {
		err = SDKErrorf(err, "", "cp4d-request-error", getComponentInfo())
		return
	}
	GetLogger().Debug("Returned from CP4D token service operation, received status code %d", resp.StatusCode)

	// If debug is enabled, then dump the response.
	if GetLogger().IsLogLevelEnabled(LevelDebug) {
		buf, dumpErr := httputil.DumpResponse(resp, req.Body != nil)
		if dumpErr == nil {
			GetLogger().Debug("Response:\n%s\n", RedactSecrets(string(buf)))
		} else {
			GetLogger().Debug(fmt.Sprintf("error while attempting to log inbound response: %s", dumpErr.Error()))
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detailedResponse, responseError := processErrorResponse(resp)
		err = authenticationErrorf(responseError, detailedResponse, "authorize", authenticator.getComponentInfo())

		// The err Summary is typically the message computed for the HTTPError instance in
		// processErrorResponse(). If the response body is non-JSON, the message will be generic
		// text based on the status code but authenticators have always used the stringified
		// RawResult, so update that here for compatilibility.
		errorMsg := responseError.Summary
		if detailedResponse.RawResult != nil {
			// RawResult is only populated if the response body is
			// non-JSON and we couldn't extract a message.
			errorMsg = string(detailedResponse.RawResult)
		}

		err.(*AuthenticationError).Summary = errorMsg

		return
	}

	tokenResponse = &cp4dTokenServerResponse{}
	err = json.NewDecoder(resp.Body).Decode(tokenResponse)
	defer resp.Body.Close() // #nosec G307
	if err != nil {
		err = fmt.Errorf(ERRORMSG_UNMARSHAL_AUTH_RESPONSE, err.Error())
		err = SDKErrorf(err, "", "cp4d-res-unmarshal-error", getComponentInfo())
		tokenResponse = nil
		return
	}

	return
}

func (authenticator *CloudPakForDataAuthenticator) getComponentInfo() *ProblemComponent {
	return NewProblemComponent("cp4d_token_service", "v1")
}

// cp4dTokenServerResponse is a struct that models a response received from the token server.
type cp4dTokenServerResponse struct {
	Token       string `json:"token,omitempty"`
	MessageCode string `json:"_messageCode_,omitempty"`
	Message     string `json:"message,omitempty"`
}

// cp4dTokenData is a struct that represents the cached information related to a fetched access token.
type cp4dTokenData struct {
	AccessToken string
	RefreshTime int64
	Expiration  int64
}

// newCp4dTokenData: constructs a new Cp4dTokenData instance from the specified Cp4dTokenServerResponse instance.
func newCp4dTokenData(tokenResponse *cp4dTokenServerResponse) (*cp4dTokenData, error) {
	// Need to crack open the access token (a JWT) to get the expiration and issued-at times.
	claims, err := parseJWT(tokenResponse.Token)
	if err != nil {
		return nil, err
	}

	// Compute the adjusted refresh time (expiration time - 20% of timeToLive)
	timeToLive := claims.ExpiresAt - claims.IssuedAt
	expireTime := claims.ExpiresAt
	refreshTime := expireTime - int64(float64(timeToLive)*0.2)

	tokenData := &cp4dTokenData{
		AccessToken: tokenResponse.Token,
		Expiration:  expireTime,
		RefreshTime: refreshTime,
	}

	return tokenData, nil
}

// isTokenValid: returns true iff the Cp4dTokenData instance represents a valid (non-expired) access token.
func (tokenData *cp4dTokenData) isTokenValid() bool {
	if tokenData.AccessToken != "" && GetCurrentTime() < tokenData.Expiration {
		return true
	}
	return false
}

// needsRefresh: synchronously returns true iff the currently stored access token should be refreshed. This method also
// updates the refresh time if it determines the token needs refreshed to prevent other threads from
// making multiple refresh calls.
func (tokenData *cp4dTokenData) needsRefresh() bool {
	cp4dNeedsRefreshMutex.Lock()
	defer cp4dNeedsRefreshMutex.Unlock()

	// Advance refresh by one minute
	if tokenData.RefreshTime >= 0 && GetCurrentTime() > tokenData.RefreshTime {
		tokenData.RefreshTime = GetCurrentTime() + 60
		return true
	}
	return false
}
