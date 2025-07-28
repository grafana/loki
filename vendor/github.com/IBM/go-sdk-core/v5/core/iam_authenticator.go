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
	"strings"
	"sync"
	"time"
)

// IamAuthenticator uses an apikey to obtain an IAM access token,
// and adds the access token to requests via an Authorization header
// of the form:
//
//	Authorization: Bearer <access-token>
type IamAuthenticator struct {
	// The apikey used to fetch the bearer token from the IAM token server.
	// You must specify either ApiKey or RefreshToken.
	ApiKey string

	// The refresh token used to fetch the bearer token from the IAM token server.
	// You must specify either ApiKey or RefreshToken.
	// If this property is specified, then you also must supply appropriate values
	// for the ClientId and ClientSecret properties (i.e. they must be the same
	// values that were used to obtain the refresh token).
	RefreshToken string

	// The URL representing the IAM token server's endpoint; If not specified,
	// a suitable default value will be used [optional].
	URL     string
	urlInit sync.Once

	// The ClientId and ClientSecret fields are used to form a "basic auth"
	// Authorization header for interactions with the IAM token server.

	// If neither field is specified, then no Authorization header will be sent
	// with token server requests [optional]. These fields are optional, but must
	// be specified together.
	ClientId     string
	ClientSecret string

	// A flag that indicates whether verification of the server's SSL certificate
	// should be disabled; defaults to false [optional].
	DisableSSLVerification bool

	// [Optional] The "scope" to use when fetching the bearer token from the
	// IAM token server.   This can be used to obtain an access token
	// with a specific scope.
	Scope string

	// [Optional] A set of key/value pairs that will be sent as HTTP headers in requests
	// made to the token server.
	Headers map[string]string

	// [Optional] The http.Client object used to invoke token server requests.
	// If not specified by the user, a suitable default Client will be constructed.
	Client     *http.Client
	clientInit sync.Once

	// The User-Agent header value to be included with each token request.
	userAgent     string
	userAgentInit sync.Once

	// The cached token and expiration time.
	tokenData *iamTokenData

	// Mutex to make the tokenData field thread safe.
	tokenDataMutex sync.Mutex
}

var (
	iamRequestTokenMutex sync.Mutex
	iamNeedsRefreshMutex sync.Mutex
)

const (
	// The default (prod) IAM token server base endpoint address.
	defaultIamTokenServerEndpoint = "https://iam.cloud.ibm.com" // #nosec G101
	iamAuthOperationPathGetToken  = "/identity/token"
	iamAuthGrantTypeApiKey        = "urn:ibm:params:oauth:grant-type:apikey" // #nosec G101
	iamAuthGrantTypeRefreshToken  = "refresh_token"                          // #nosec G101

	// The number of seconds before the IAM server-assigned (official) expiration time
	// when we'll treat an otherwise valid access token as "expired".
	iamExpirationWindow = 10
)

// IamAuthenticatorBuilder is used to construct an IamAuthenticator instance.
type IamAuthenticatorBuilder struct {
	IamAuthenticator
}

// NewIamAuthenticatorBuilder returns a new builder struct that
// can be used to construct an IamAuthenticator instance.
func NewIamAuthenticatorBuilder() *IamAuthenticatorBuilder {
	return &IamAuthenticatorBuilder{}
}

// SetApiKey sets the ApiKey field in the builder.
func (builder *IamAuthenticatorBuilder) SetApiKey(s string) *IamAuthenticatorBuilder {
	builder.IamAuthenticator.ApiKey = s
	return builder
}

// SetRefreshToken sets the RefreshToken field in the builder.
func (builder *IamAuthenticatorBuilder) SetRefreshToken(s string) *IamAuthenticatorBuilder {
	builder.IamAuthenticator.RefreshToken = s
	return builder
}

// SetURL sets the URL field in the builder.
func (builder *IamAuthenticatorBuilder) SetURL(s string) *IamAuthenticatorBuilder {
	builder.IamAuthenticator.URL = s
	return builder
}

// SetClientIDSecret sets the ClientId and ClientSecret fields in the builder.
func (builder *IamAuthenticatorBuilder) SetClientIDSecret(clientID, clientSecret string) *IamAuthenticatorBuilder {
	builder.IamAuthenticator.ClientId = clientID
	builder.IamAuthenticator.ClientSecret = clientSecret
	return builder
}

// SetDisableSSLVerification sets the DisableSSLVerification field in the builder.
func (builder *IamAuthenticatorBuilder) SetDisableSSLVerification(b bool) *IamAuthenticatorBuilder {
	builder.IamAuthenticator.DisableSSLVerification = b
	return builder
}

// SetScope sets the Scope field in the builder.
func (builder *IamAuthenticatorBuilder) SetScope(s string) *IamAuthenticatorBuilder {
	builder.IamAuthenticator.Scope = s
	return builder
}

// SetHeaders sets the Headers field in the builder.
func (builder *IamAuthenticatorBuilder) SetHeaders(headers map[string]string) *IamAuthenticatorBuilder {
	builder.IamAuthenticator.Headers = headers
	return builder
}

// SetClient sets the Client field in the builder.
func (builder *IamAuthenticatorBuilder) SetClient(client *http.Client) *IamAuthenticatorBuilder {
	builder.IamAuthenticator.Client = client
	return builder
}

// Build() returns a validated instance of the IamAuthenticator with the config that was set in the builder.
func (builder *IamAuthenticatorBuilder) Build() (*IamAuthenticator, error) {
	// Make sure the config is valid.
	err := builder.IamAuthenticator.Validate()
	if err != nil {
		return nil, RepurposeSDKProblem(err, "validation-failed")
	}

	return &builder.IamAuthenticator, nil
}

// client returns the authenticator's http client after potentially initializing it.
func (authenticator *IamAuthenticator) client() *http.Client {
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
func (authenticator *IamAuthenticator) getUserAgent() string {
	authenticator.userAgentInit.Do(func() {
		authenticator.userAgent = fmt.Sprintf("%s/%s-%s %s", sdkName, "iam-authenticator", __VERSION__, SystemInfo())
	})
	return authenticator.userAgent
}

// NewIamAuthenticator constructs a new IamAuthenticator instance.
// Deprecated - use the IamAuthenticatorBuilder instead.
func NewIamAuthenticator(apiKey string, url string, clientId string, clientSecret string,
	disableSSLVerification bool, headers map[string]string) (*IamAuthenticator, error) {
	authenticator, err := NewIamAuthenticatorBuilder().
		SetApiKey(apiKey).
		SetURL(url).
		SetClientIDSecret(clientId, clientSecret).
		SetDisableSSLVerification(disableSSLVerification).
		SetHeaders(headers).
		Build()

	return authenticator, RepurposeSDKProblem(err, "auth-builder-fail")
}

// newIamAuthenticatorFromMap constructs a new IamAuthenticator instance from a map.
func newIamAuthenticatorFromMap(properties map[string]string) (authenticator *IamAuthenticator, err error) {
	if properties == nil {
		err := errors.New(ERRORMSG_PROPS_MAP_NIL)
		return nil, SDKErrorf(err, "", "missing-props", getComponentInfo())
	}

	disableSSL, err := strconv.ParseBool(properties[PROPNAME_AUTH_DISABLE_SSL])
	if err != nil {
		disableSSL = false
	}

	authenticator, err = NewIamAuthenticatorBuilder().
		SetApiKey(properties[PROPNAME_APIKEY]).
		SetRefreshToken(properties[PROPNAME_REFRESH_TOKEN]).
		SetURL(properties[PROPNAME_AUTH_URL]).
		SetClientIDSecret(properties[PROPNAME_CLIENT_ID], properties[PROPNAME_CLIENT_SECRET]).
		SetDisableSSLVerification(disableSSL).
		SetScope(properties[PROPNAME_SCOPE]).
		Build()

	return
}

// AuthenticationType returns the authentication type for this authenticator.
func (*IamAuthenticator) AuthenticationType() string {
	return AUTHTYPE_IAM
}

// Authenticate adds IAM authentication information to the request.
//
// The IAM access token will be added to the request's headers in the form:
//
//	Authorization: Bearer <access-token>
func (authenticator *IamAuthenticator) Authenticate(request *http.Request) error {
	token, err := authenticator.GetToken()
	if err != nil {
		return RepurposeSDKProblem(err, "get-token-fail")
	}

	request.Header.Set("Authorization", "Bearer "+token)
	GetLogger().Debug("Authenticated outbound request (type=%s)\n", authenticator.AuthenticationType())
	return nil
}

// url returns the authenticator's URL property after potentially initializing it.
func (authenticator *IamAuthenticator) url() string {
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

// getTokenData returns the tokenData field from the authenticator.
func (authenticator *IamAuthenticator) getTokenData() *iamTokenData {
	authenticator.tokenDataMutex.Lock()
	defer authenticator.tokenDataMutex.Unlock()

	return authenticator.tokenData
}

// setTokenData sets the given iamTokenData to the tokenData field of the authenticator.
func (authenticator *IamAuthenticator) setTokenData(tokenData *iamTokenData) {
	authenticator.tokenDataMutex.Lock()
	defer authenticator.tokenDataMutex.Unlock()

	authenticator.tokenData = tokenData

	// Next, we should save the just-returned refresh token back to the main
	// authenticator struct.
	// This is done so that if we were originally configured with
	// a refresh token, then we'll be sure to use a "fresh"
	// refresh token next time we invoke the "get token" operation.
	// This was recommended by the IAM team to avoid problems in the future
	// if the token service is changed to invalidate an existing refresh token
	// when a new one is generated and returned in the response.
	if tokenData != nil {
		authenticator.RefreshToken = tokenData.RefreshToken
	}
}

// Validate the authenticator's configuration.
//
// Ensures that the ApiKey and RefreshToken properties are mutually exclusive,
// and that the ClientId and ClientSecret properties are mutually inclusive.
func (authenticator *IamAuthenticator) Validate() error {
	// The user should specify at least one of ApiKey or RefreshToken.
	// Note: We'll allow both ApiKey and RefreshToken to be specified,
	// in which case we'd use ApiKey in the RequestToken() method.
	// Consider this scenario...
	// - An IamAuthenticator instance is configured with an apikey and is initially
	//   declared to be "valid" by the Validate() method.
	// - The authenticator is used to construct a service, then an operation is
	//   invoked which then triggers the very first call to RequestToken().
	// - The authenticator invokes the IAM get_token operation and then receives
	//   the response.  The authenticator copies the refresh_token value from the response
	//   to the authenticator's RefreshToken field.
	// - At this point, the authenticator would have non-empty values in both the
	//   ApiKey and RefreshToken fields.
	// This all means that we must try to make sure that a previously-validated
	// instance of the authenticator doesn't become invalidated simply through
	// normal use.
	//
	if authenticator.ApiKey == "" && authenticator.RefreshToken == "" {
		err := fmt.Errorf(ERRORMSG_EXCLUSIVE_PROPS_ERROR, "ApiKey", "RefreshToken")
		return SDKErrorf(err, "", "exc-props", getComponentInfo())
	}

	if authenticator.ApiKey != "" && HasBadFirstOrLastChar(authenticator.ApiKey) {
		err := fmt.Errorf(ERRORMSG_PROP_INVALID, "ApiKey")
		return SDKErrorf(err, "", "bad-prop", getComponentInfo())
	}

	// Validate ClientId and ClientSecret.
	// Either both or neither should be specified.
	if authenticator.ClientId == "" && authenticator.ClientSecret == "" {
		// Do nothing as this is the valid scenario.
	} else {
		// Since it is NOT the case that both properties are empty, make sure BOTH are specified.
		if authenticator.ClientId == "" {
			err := fmt.Errorf(ERRORMSG_PROP_MISSING, "ClientId")
			return SDKErrorf(err, "", "missing-id", getComponentInfo())
		}

		if authenticator.ClientSecret == "" {
			err := fmt.Errorf(ERRORMSG_PROP_MISSING, "ClientSecret")
			return SDKErrorf(err, "", "missing-secret", getComponentInfo())
		}
	}

	return nil
}

// GetToken: returns an access token to be used in an Authorization header.
// Whenever a new token is needed (when a token doesn't yet exist, needs to be refreshed,
// or the existing token has expired), a new access token is fetched from the token server.
func (authenticator *IamAuthenticator) GetToken() (string, error) {
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
func (authenticator *IamAuthenticator) synchronizedRequestToken() error {
	iamRequestTokenMutex.Lock()
	defer iamRequestTokenMutex.Unlock()
	// if cached token is still valid, then just continue to use it
	if authenticator.getTokenData() != nil && authenticator.getTokenData().isTokenValid() {
		return nil
	}

	return authenticator.invokeRequestTokenData()
}

// invokeRequestTokenData: requests a new token from the access server and
// unmarshals the token information to the tokenData cache. Returns
// an error if the token was unable to be fetched, otherwise returns nil
func (authenticator *IamAuthenticator) invokeRequestTokenData() error {
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

// RequestToken fetches a new access token from the token server.
func (authenticator *IamAuthenticator) RequestToken() (*IamTokenServerResponse, error) {
	builder := NewRequestBuilder(POST)
	_, err := builder.ResolveRequestURL(authenticator.url(), iamAuthOperationPathGetToken, nil)
	if err != nil {
		return nil, RepurposeSDKProblem(err, "url-resolve-error")
	}

	builder.AddHeader(CONTENT_TYPE, "application/x-www-form-urlencoded")
	builder.AddHeader(Accept, APPLICATION_JSON)
	builder.AddHeader(headerNameUserAgent, authenticator.getUserAgent())
	builder.AddFormData("response_type", "", "", "cloud_iam")

	if authenticator.ApiKey != "" {
		// If ApiKey was configured, then use grant_type "apikey" to obtain an access token.
		builder.AddFormData("grant_type", "", "", iamAuthGrantTypeApiKey)
		builder.AddFormData("apikey", "", "", authenticator.ApiKey)
	} else if authenticator.RefreshToken != "" {
		// Otherwise, if RefreshToken was configured then use grant_type "refresh_token".
		builder.AddFormData("grant_type", "", "", iamAuthGrantTypeRefreshToken)
		builder.AddFormData("refresh_token", "", "", authenticator.RefreshToken)
	} else {
		// We shouldn't ever get here due to prior validations, but just in case, let's log an error.
		err := fmt.Errorf(ERRORMSG_EXCLUSIVE_PROPS_ERROR, "ApiKey", "RefreshToken")
		return nil, SDKErrorf(err, "", "iam-exc-props", getComponentInfo())
	}

	// Add any optional parameters to the request.
	if authenticator.Scope != "" {
		builder.AddFormData("scope", "", "", authenticator.Scope)
	}

	// Add user-defined headers to request.
	for headerName, headerValue := range authenticator.Headers {
		builder.AddHeader(headerName, headerValue)
	}

	req, err := builder.Build()
	if err != nil {
		return nil, RepurposeSDKProblem(err, "request-build-error")
	}

	// If client id and secret were configured by the user, then set them on the request
	// as a basic auth header.
	// Our previous validation step would have made sure that both values are specified
	// if the RefreshToken property was specified.
	if authenticator.ClientId != "" && authenticator.ClientSecret != "" {
		req.SetBasicAuth(authenticator.ClientId, authenticator.ClientSecret)
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
		err = SDKErrorf(err, "", "request-error", getComponentInfo())
		return nil, err
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

		authError.Summary = iamErrorMsg

		return nil, authError
	}

	tokenResponse := &IamTokenServerResponse{}
	_ = json.NewDecoder(resp.Body).Decode(tokenResponse)
	defer resp.Body.Close() // #nosec G307
	return tokenResponse, nil
}

func (authenticator *IamAuthenticator) getComponentInfo() *ProblemComponent {
	return NewProblemComponent("iam_identity_services", "")
}

// IamTokenServerResponse : This struct models a response received from the token server.
type IamTokenServerResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Expiration   int64  `json:"expiration"`
}

// iamTokenData : This struct represents the cached information related to a fetched access token.
type iamTokenData struct {
	AccessToken  string
	RefreshToken string
	RefreshTime  int64
	Expiration   int64
}

// newIamTokenData: constructs a new IamTokenData instance from the specified IamTokenServerResponse instance.
func newIamTokenData(tokenResponse *IamTokenServerResponse) (*iamTokenData, error) {
	if tokenResponse == nil {
		err := fmt.Errorf("Error while trying to parse access token!")
		return nil, SDKErrorf(err, "", "token-parse", getComponentInfo())
	}
	// Compute the adjusted refresh time (expiration time - 20% of timeToLive)
	timeToLive := tokenResponse.ExpiresIn
	expireTime := tokenResponse.Expiration
	refreshTime := expireTime - int64(float64(timeToLive)*0.2)

	tokenData := &iamTokenData{
		AccessToken:  tokenResponse.AccessToken,
		RefreshToken: tokenResponse.RefreshToken,
		Expiration:   expireTime,
		RefreshTime:  refreshTime,
	}

	return tokenData, nil
}

// isTokenValid: returns true iff the IamTokenData instance represents a valid (non-expired) access token.
func (td *iamTokenData) isTokenValid() bool {
	// We'll use "exp - 10" so that we'll treat an otherwise valid (unexpired) access token
	// to be expired if we're within 10 seconds of its TTL.
	// This is because some IBM Cloud services reject valid access tokens with a short
	// TTL remaining (TTL < 5 secs) because the access token might be used in a longer-running
	// transaction and could potentially expire in the middle of the transaction.
	if td.AccessToken != "" && GetCurrentTime() < (td.Expiration-iamExpirationWindow) {
		return true
	}
	return false
}

// needsRefresh: synchronously returns true iff the currently stored access token should be refreshed.
// This method also updates the refresh time if it determines the token needs to be refreshed
// to prevent other threads from making multiple refresh calls.
func (td *iamTokenData) needsRefresh() bool {
	iamNeedsRefreshMutex.Lock()
	defer iamNeedsRefreshMutex.Unlock()

	// Advance refresh by one minute
	if td.RefreshTime >= 0 && GetCurrentTime() > td.RefreshTime {
		td.RefreshTime = GetCurrentTime() + 60
		return true
	}

	return false
}
