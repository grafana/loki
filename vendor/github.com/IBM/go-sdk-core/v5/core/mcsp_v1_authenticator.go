package core

// (C) Copyright IBM Corp. 2023, 2024.
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

// MCSPAuthenticator uses an apikey to obtain an access token,
// and adds the access token to requests via an Authorization header
// of the form:  "Authorization: Bearer <access-token>"
type MCSPAuthenticator struct {
	// [Required] The apikey used to fetch the bearer token from the token server.
	ApiKey string

	// [Required] The endpoint base URL for the token server.
	URL string

	// [Optional] A flag that indicates whether verification of the token server's SSL certificate
	// should be disabled; defaults to false.
	DisableSSLVerification bool

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
	tokenData *mcspTokenData

	// Mutex to make the tokenData field thread safe.
	tokenDataMutex sync.Mutex
}

var (
	mcspRequestTokenMutex sync.Mutex
	mcspNeedsRefreshMutex sync.Mutex
)

const (
	mcspAuthOperationPath = "/siusermgr/api/1.0/apikeys/token"
)

// MCSPAuthenticatorBuilder is used to construct an MCSPAuthenticator instance.
type MCSPAuthenticatorBuilder struct {
	MCSPAuthenticator
}

// NewMCSPAuthenticatorBuilder returns a new builder struct that
// can be used to construct an MCSPAuthenticator instance.
func NewMCSPAuthenticatorBuilder() *MCSPAuthenticatorBuilder {
	return &MCSPAuthenticatorBuilder{}
}

// SetApiKey sets the ApiKey field in the builder.
func (builder *MCSPAuthenticatorBuilder) SetApiKey(s string) *MCSPAuthenticatorBuilder {
	builder.MCSPAuthenticator.ApiKey = s
	return builder
}

// SetURL sets the URL field in the builder.
func (builder *MCSPAuthenticatorBuilder) SetURL(s string) *MCSPAuthenticatorBuilder {
	builder.MCSPAuthenticator.URL = s
	return builder
}

// SetDisableSSLVerification sets the DisableSSLVerification field in the builder.
func (builder *MCSPAuthenticatorBuilder) SetDisableSSLVerification(b bool) *MCSPAuthenticatorBuilder {
	builder.MCSPAuthenticator.DisableSSLVerification = b
	return builder
}

// SetHeaders sets the Headers field in the builder.
func (builder *MCSPAuthenticatorBuilder) SetHeaders(headers map[string]string) *MCSPAuthenticatorBuilder {
	builder.MCSPAuthenticator.Headers = headers
	return builder
}

// SetClient sets the Client field in the builder.
func (builder *MCSPAuthenticatorBuilder) SetClient(client *http.Client) *MCSPAuthenticatorBuilder {
	builder.MCSPAuthenticator.Client = client
	return builder
}

// Build() returns a validated instance of the MCSPAuthenticator with the config that was set in the builder.
func (builder *MCSPAuthenticatorBuilder) Build() (*MCSPAuthenticator, error) {
	// Make sure the config is valid.
	err := builder.MCSPAuthenticator.Validate()
	if err != nil {
		return nil, RepurposeSDKProblem(err, "validation-failed")
	}

	return &builder.MCSPAuthenticator, nil
}

// client returns the authenticator's http client after potentially initializing it.
func (authenticator *MCSPAuthenticator) client() *http.Client {
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
func (authenticator *MCSPAuthenticator) getUserAgent() string {
	authenticator.userAgentInit.Do(func() {
		authenticator.userAgent = fmt.Sprintf("%s/%s-%s %s", sdkName, "mcsp-authenticator", __VERSION__, SystemInfo())
	})
	return authenticator.userAgent
}

// newMCSPAuthenticatorFromMap constructs a new MCSPAuthenticator instance from a map.
func newMCSPAuthenticatorFromMap(properties map[string]string) (authenticator *MCSPAuthenticator, err error) {
	if properties == nil {
		err = errors.New(ERRORMSG_PROPS_MAP_NIL)
		return nil, SDKErrorf(err, "", "missing-props", getComponentInfo())
	}

	disableSSL, err := strconv.ParseBool(properties[PROPNAME_AUTH_DISABLE_SSL])
	if err != nil {
		disableSSL = false
	}

	authenticator, err = NewMCSPAuthenticatorBuilder().
		SetApiKey(properties[PROPNAME_APIKEY]).
		SetURL(properties[PROPNAME_AUTH_URL]).
		SetDisableSSLVerification(disableSSL).
		Build()

	return
}

// AuthenticationType returns the authentication type for this authenticator.
func (*MCSPAuthenticator) AuthenticationType() string {
	return AUTHTYPE_MCSP
}

// Authenticate adds the Authorization header to the request.
// The value will be of the form: "Authorization: Bearer <bearer-token>""
func (authenticator *MCSPAuthenticator) Authenticate(request *http.Request) error {
	token, err := authenticator.GetToken()
	if err != nil {
		return RepurposeSDKProblem(err, "get-token-fail")
	}

	request.Header.Set("Authorization", "Bearer "+token)
	GetLogger().Debug("Authenticated outbound request (type=%s)\n", authenticator.AuthenticationType())
	return nil
}

// getTokenData returns the tokenData field from the authenticator.
func (authenticator *MCSPAuthenticator) getTokenData() *mcspTokenData {
	authenticator.tokenDataMutex.Lock()
	defer authenticator.tokenDataMutex.Unlock()

	return authenticator.tokenData
}

// setTokenData sets the given mcspTokenData to the tokenData field of the authenticator.
func (authenticator *MCSPAuthenticator) setTokenData(tokenData *mcspTokenData) {
	authenticator.tokenDataMutex.Lock()
	defer authenticator.tokenDataMutex.Unlock()

	authenticator.tokenData = tokenData
	GetLogger().Info("setTokenData: expiration=%d, refreshTime=%d",
		authenticator.tokenData.Expiration, authenticator.tokenData.RefreshTime)
}

// Validate the authenticator's configuration.
//
// Ensures that the ApiKey and URL properties are both specified.
func (authenticator *MCSPAuthenticator) Validate() error {
	if authenticator.ApiKey == "" {
		err := fmt.Errorf(ERRORMSG_PROP_MISSING, "ApiKey")
		return SDKErrorf(err, "", "missing-api-key", getComponentInfo())
	}

	if authenticator.URL == "" {
		err := fmt.Errorf(ERRORMSG_PROP_MISSING, "URL")
		return SDKErrorf(err, "", "missing-url", getComponentInfo())
	}

	return nil
}

// GetToken: returns an access token to be used in an Authorization header.
// Whenever a new token is needed (when a token doesn't yet exist, needs to be refreshed,
// or the existing token has expired), a new access token is fetched from the token server.
func (authenticator *MCSPAuthenticator) GetToken() (string, error) {
	if authenticator.getTokenData() == nil || !authenticator.getTokenData().isTokenValid() {
		GetLogger().Debug("Performing synchronous token fetch...")
		// synchronously request the token
		err := authenticator.synchronizedRequestToken()
		if err != nil {
			return "", RepurposeSDKProblem(err, "request-token-fail")
		}
	} else if authenticator.getTokenData().needsRefresh() {
		GetLogger().Debug("Performing background asynchronous token fetch...")
		// If refresh needed, kick off a go routine in the background to get a new token.
		//nolint: errcheck
		go authenticator.invokeRequestTokenData()
	} else {
		GetLogger().Debug("Using cached access token...")
	}

	// return an error if the access token is not valid or was not fetched
	if authenticator.getTokenData() == nil || authenticator.getTokenData().AccessToken == "" {
		err := errors.New("Error while trying to get access token")
		return "", SDKErrorf(err, "", "no-token", getComponentInfo())
	}

	return authenticator.getTokenData().AccessToken, nil
}

// synchronizedRequestToken: synchronously checks if the current token in cache
// is valid. If token is not valid or does not exist, it will fetch a new token.
func (authenticator *MCSPAuthenticator) synchronizedRequestToken() error {
	mcspRequestTokenMutex.Lock()
	defer mcspRequestTokenMutex.Unlock()
	// if cached token is still valid, then just continue to use it
	if authenticator.getTokenData() != nil && authenticator.getTokenData().isTokenValid() {
		return nil
	}

	return authenticator.invokeRequestTokenData()
}

// invokeRequestTokenData: requests a new token from the access server and
// unmarshals the token information to the tokenData cache. Returns
// an error if the token was unable to be fetched, otherwise returns nil
func (authenticator *MCSPAuthenticator) invokeRequestTokenData() error {
	tokenResponse, err := authenticator.RequestToken()
	if err != nil {
		return err
	}

	GetLogger().Info("invokeRequestTokenData(): RequestToken returned tokenResponse:\n%+v", *tokenResponse)
	tokenData, err := newMCSPTokenData(tokenResponse)
	if err != nil {
		tokenData = &mcspTokenData{}
	}

	authenticator.setTokenData(tokenData)

	return nil
}

// RequestToken fetches a new access token from the token server.
func (authenticator *MCSPAuthenticator) RequestToken() (*MCSPTokenServerResponse, error) {
	builder := NewRequestBuilder(POST)
	_, err := builder.ResolveRequestURL(authenticator.URL, mcspAuthOperationPath, nil)
	if err != nil {
		err = RepurposeSDKProblem(err, "url-resolve-error")
		return nil, err
	}

	builder.AddHeader(CONTENT_TYPE, APPLICATION_JSON)
	builder.AddHeader(Accept, APPLICATION_JSON)
	builder.AddHeader(headerNameUserAgent, authenticator.getUserAgent())
	requestBody := fmt.Sprintf(`{"apikey":"%s"}`, authenticator.ApiKey)
	_, _ = builder.SetBodyContentString(requestBody)

	// Add user-defined headers to request.
	for headerName, headerValue := range authenticator.Headers {
		builder.AddHeader(headerName, headerValue)
	}

	req, err := builder.Build()
	if err != nil {
		return nil, RepurposeSDKProblem(err, "request-build-error")
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

	GetLogger().Debug("Invoking MCSP 'get token' operation: %s", builder.URL)
	resp, err := authenticator.client().Do(req)
	if err != nil {
		err = SDKErrorf(err, "", "request-error", getComponentInfo())
		return nil, err
	}
	GetLogger().Debug("Returned from MCSP 'get token' operation, received status code %d", resp.StatusCode)

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
		errorMsg := err.Summary
		if detailedResponse.RawResult != nil {
			// RawResult is only populated if the response body is
			// non-JSON and we couldn't extract a message.
			errorMsg = string(detailedResponse.RawResult)
		}

		authError.Summary = errorMsg

		return nil, authError
	}

	tokenResponse := &MCSPTokenServerResponse{}
	_ = json.NewDecoder(resp.Body).Decode(tokenResponse)
	defer resp.Body.Close() // #nosec G307

	return tokenResponse, nil
}

func (authenticator *MCSPAuthenticator) getComponentInfo() *ProblemComponent {
	return NewProblemComponent("mscp_token_server", "1.0")
}

// MCSPTokenServerResponse : This struct models a response received from the token server.
type MCSPTokenServerResponse struct {
	Token     string `json:"token"`
	TokenType string `json:"token_type"`
	ExpiresIn int64  `json:"expires_in"`
}

// mcspTokenData : This struct represents the cached information related to a fetched access token.
type mcspTokenData struct {
	AccessToken string
	RefreshTime int64
	Expiration  int64
}

// newMCSPTokenData: constructs a new mcspTokenData instance from the specified
// MCSPTokenServerResponse instance.
func newMCSPTokenData(tokenResponse *MCSPTokenServerResponse) (*mcspTokenData, error) {
	if tokenResponse == nil || tokenResponse.Token == "" {
		err := errors.New("Error while trying to parse access token!")
		return nil, SDKErrorf(err, "", "token-parse", getComponentInfo())
	}

	// Need to crack open the access token (a JWT) to get the expiration and issued-at times
	// so that we can compute the refresh time.
	claims, err := parseJWT(tokenResponse.Token)
	if err != nil {
		return nil, err
	}

	// Compute the adjusted refresh time (expiration time - 20% of timeToLive)
	timeToLive := claims.ExpiresAt - claims.IssuedAt
	expireTime := claims.ExpiresAt
	refreshTime := expireTime - int64(float64(timeToLive)*0.2)

	tokenData := &mcspTokenData{
		AccessToken: tokenResponse.Token,
		Expiration:  expireTime,
		RefreshTime: refreshTime,
	}

	GetLogger().Info("newMCSPTokenData: expiration=%d, refreshTime=%d", tokenData.Expiration, tokenData.RefreshTime)

	return tokenData, nil
}

// isTokenValid: returns true iff the mcspTokenData instance represents a valid (non-expired) access token.
func (tokenData *mcspTokenData) isTokenValid() bool {
	if tokenData.AccessToken != "" && GetCurrentTime() < tokenData.Expiration {
		GetLogger().Info("isTokenValid: Token is valid!")
		return true
	}
	GetLogger().Info("isTokenValid: Token is NOT valid!")
	GetLogger().Info("isTokenValid: expiration=%d, refreshTime=%d", tokenData.Expiration, tokenData.RefreshTime)
	GetLogger().Info("GetCurrentTime(): %d\n", GetCurrentTime())
	return false
}

// needsRefresh: synchronously returns true iff the currently stored access token should be refreshed. This method also
// updates the refresh time if it determines the token needs refreshed to prevent other threads from
// making multiple refresh calls.
func (tokenData *mcspTokenData) needsRefresh() bool {
	mcspNeedsRefreshMutex.Lock()
	defer mcspNeedsRefreshMutex.Unlock()

	// Advance refresh by one minute
	if tokenData.RefreshTime >= 0 && GetCurrentTime() > tokenData.RefreshTime {
		tokenData.RefreshTime = GetCurrentTime() + 60
		return true
	}

	return false
}
