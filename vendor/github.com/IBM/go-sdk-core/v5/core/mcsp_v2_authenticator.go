package core

// (C) Copyright IBM Corp. 2025.
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

// MCSPV2Authenticator invokes the MCSP v2 token-exchange operation (POST /api/2.0/{scopeCollectionType}/{scopeId}/apikeys/token)
// to obtain an access token for an apikey, and adds the access token to requests via an Authorization header
// of the form:  "Authorization: Bearer <access-token>"
type MCSPV2Authenticator struct {
	// [Required] The apikey used to fetch the access token from the token server.
	ApiKey string

	// [Required] The endpoint base URL for the token server.
	URL string

	// [Required] The scope collection type of item(s).
	// Valid values are: "accounts", "subscriptions", "services".
	ScopeCollectionType string

	// [Required] The scope identifier of item(s).
	ScopeID string

	// [Optional] A flag to include builtin actions in the "actions" claim in the MCSP access token (default: false).
	IncludeBuiltinActions bool

	// [Optional] A flag to include custom actions in the "actions" claim in the MCSP access token (default: false).
	IncludeCustomActions bool

	// [Optional] A flag to include the "roles" claim in the MCSP access token (default: true).
	IncludeRoles bool

	// [Optional] A flag to add a prefix with the scope level where the role is defined in the "roles" claim (default: false).
	PrefixRoles bool

	// [Optional] A map containing keys and values to be injected into the access token as the "callerExt" claim.
	// The keys used in this map must be enabled in the apikey by setting the "callerExtClaimNames" property when the apikey is created.
	// This property is typically only used in scenarios involving an apikey with identityType `SERVICEID`.
	CallerExtClaim map[string]string

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
	tokenData *mcspv2TokenData

	// Mutex to make the tokenData field thread safe.
	tokenDataMutex sync.Mutex
}

var (
	mcspv2RequestTokenMutex sync.Mutex
	mcspv2NeedsRefreshMutex sync.Mutex
)

const (
	mcspv2AuthOperationPath = "/api/2.0/{scopeCollectionType}/{scopeId}/apikeys/token"
)

// MCSPV2AuthenticatorBuilder is used to construct an MCSPV2Authenticator instance.
type MCSPV2AuthenticatorBuilder struct {
	MCSPV2Authenticator
}

// NewMCSPV2AuthenticatorBuilder returns a new builder struct that
// can be used to construct an MCSPV2Authenticator instance.
func NewMCSPV2AuthenticatorBuilder() *MCSPV2AuthenticatorBuilder {
	auth := &MCSPV2AuthenticatorBuilder{}

	// Set fields whose default value is not the "zero value".
	auth.MCSPV2Authenticator.IncludeRoles = true

	return auth
}

// SetApiKey sets the ApiKey field in the builder.
func (builder *MCSPV2AuthenticatorBuilder) SetApiKey(s string) *MCSPV2AuthenticatorBuilder {
	builder.MCSPV2Authenticator.ApiKey = s
	return builder
}

// SetURL sets the URL field in the builder.
func (builder *MCSPV2AuthenticatorBuilder) SetURL(s string) *MCSPV2AuthenticatorBuilder {
	builder.MCSPV2Authenticator.URL = s
	return builder
}

// SetScopeCollectionType sets the ScopeCollectionType field in the builder.
func (builder *MCSPV2AuthenticatorBuilder) SetScopeCollectionType(s string) *MCSPV2AuthenticatorBuilder {
	builder.MCSPV2Authenticator.ScopeCollectionType = s
	return builder
}

// SetScopeID sets the ScopeID field in the builder.
func (builder *MCSPV2AuthenticatorBuilder) SetScopeID(s string) *MCSPV2AuthenticatorBuilder {
	builder.MCSPV2Authenticator.ScopeID = s
	return builder
}

// SetIncludeBuiltinActions sets the IncludeBuiltinActions field in the builder.
func (builder *MCSPV2AuthenticatorBuilder) SetIncludeBuiltinActions(b bool) *MCSPV2AuthenticatorBuilder {
	builder.MCSPV2Authenticator.IncludeBuiltinActions = b
	return builder
}

// SetIncludeCustomActions sets the IncludeCustomActions field in the builder.
func (builder *MCSPV2AuthenticatorBuilder) SetIncludeCustomActions(b bool) *MCSPV2AuthenticatorBuilder {
	builder.MCSPV2Authenticator.IncludeCustomActions = b
	return builder
}

// SetIncludeRoles sets the IncludeRoles field in the builder.
func (builder *MCSPV2AuthenticatorBuilder) SetIncludeRoles(b bool) *MCSPV2AuthenticatorBuilder {
	builder.MCSPV2Authenticator.IncludeRoles = b
	return builder
}

// SetPrefixRoles sets the PrefixRoles field in the builder.
func (builder *MCSPV2AuthenticatorBuilder) SetPrefixRoles(b bool) *MCSPV2AuthenticatorBuilder {
	builder.MCSPV2Authenticator.PrefixRoles = b
	return builder
}

// SetCallerExtClaim sets the CallerExtClaim field in the builder.
func (builder *MCSPV2AuthenticatorBuilder) SetCallerExtClaim(m map[string]string) *MCSPV2AuthenticatorBuilder {
	builder.MCSPV2Authenticator.CallerExtClaim = m
	return builder
}

// SetDisableSSLVerification sets the DisableSSLVerification field in the builder.
func (builder *MCSPV2AuthenticatorBuilder) SetDisableSSLVerification(b bool) *MCSPV2AuthenticatorBuilder {
	builder.MCSPV2Authenticator.DisableSSLVerification = b
	return builder
}

// SetHeaders sets the Headers field in the builder.
func (builder *MCSPV2AuthenticatorBuilder) SetHeaders(headers map[string]string) *MCSPV2AuthenticatorBuilder {
	builder.MCSPV2Authenticator.Headers = headers
	return builder
}

// SetClient sets the Client field in the builder.
func (builder *MCSPV2AuthenticatorBuilder) SetClient(client *http.Client) *MCSPV2AuthenticatorBuilder {
	builder.MCSPV2Authenticator.Client = client
	return builder
}

// Build returns a validated instance of the MCSPV2Authenticator with the config that was set in the builder.
func (builder *MCSPV2AuthenticatorBuilder) Build() (*MCSPV2Authenticator, error) {
	// Make sure the config is valid.
	err := builder.MCSPV2Authenticator.Validate()
	if err != nil {
		return nil, RepurposeSDKProblem(err, "validation-failed")
	}

	return &builder.MCSPV2Authenticator, nil
}

// Validate the authenticator's configuration.
func (authenticator *MCSPV2Authenticator) Validate() error {
	if authenticator.ApiKey == "" {
		err := fmt.Errorf(ERRORMSG_PROP_MISSING, "ApiKey")
		return SDKErrorf(err, "", "missing-api-key", getComponentInfo())
	}

	if authenticator.URL == "" {
		err := fmt.Errorf(ERRORMSG_PROP_MISSING, "URL")
		return SDKErrorf(err, "", "missing-url", getComponentInfo())
	}

	if authenticator.ScopeCollectionType == "" {
		err := fmt.Errorf(ERRORMSG_PROP_MISSING, "ScopeCollectionType")
		return SDKErrorf(err, "", "missing-scope-collection-type", getComponentInfo())
	}

	if authenticator.ScopeID == "" {
		err := fmt.Errorf(ERRORMSG_PROP_MISSING, "ScopeID")
		return SDKErrorf(err, "", "missing-scope-id", getComponentInfo())
	}

	return nil
}

// client returns the authenticator's http client after potentially initializing it.
func (authenticator *MCSPV2Authenticator) client() *http.Client {
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
func (authenticator *MCSPV2Authenticator) getUserAgent() string {
	authenticator.userAgentInit.Do(func() {
		authenticator.userAgent = fmt.Sprintf("%s/%s-%s %s", sdkName, "mcspv2-authenticator", __VERSION__, SystemInfo())
	})
	return authenticator.userAgent
}

// newMCSPV2AuthenticatorFromMap constructs a new MCSPV2Authenticator instance from a map.
func newMCSPV2AuthenticatorFromMap(properties map[string]string) (authenticator *MCSPV2Authenticator, err error) {
	if properties == nil {
		err = errors.New(ERRORMSG_PROPS_MAP_NIL)
		return nil, SDKErrorf(err, "", "missing-props", getComponentInfo())
	}

	// Initialize the builder first with the required properties.
	builder := NewMCSPV2AuthenticatorBuilder().
		SetApiKey(properties[PROPNAME_APIKEY]).
		SetURL(properties[PROPNAME_AUTH_URL]).
		SetScopeCollectionType(properties[PROPNAME_SCOPE_COLLECTION_TYPE]).
		SetScopeID(properties[PROPNAME_SCOPE_ID])

	// Now add the optional properties to the builder.
	var strValue string
	var boolValue bool

	strValue = properties[PROPNAME_INCLUDE_BUILTIN_ACTIONS]
	if strValue != "" {
		boolValue, err = strconv.ParseBool(strValue)
		if err != nil {
			err = SDKErrorf(err,
				fmt.Sprintf(ERRORMSG_PROP_PARSE_ERROR, PROPNAME_INCLUDE_BUILTIN_ACTIONS, strValue),
				"validation-error", getComponentInfo())
			return
		}
		builder.SetIncludeBuiltinActions(boolValue)
	}

	strValue = properties[PROPNAME_INCLUDE_CUSTOM_ACTIONS]
	if strValue != "" {
		boolValue, err = strconv.ParseBool(strValue)
		if err != nil {
			err = SDKErrorf(err,
				fmt.Sprintf(ERRORMSG_PROP_PARSE_ERROR, PROPNAME_INCLUDE_CUSTOM_ACTIONS, strValue),
				"validation-error", getComponentInfo())
			return
		}
		builder.SetIncludeCustomActions(boolValue)
	}

	strValue = properties[PROPNAME_INCLUDE_ROLES]
	if strValue != "" {
		boolValue, err = strconv.ParseBool(strValue)
		if err != nil {
			err = SDKErrorf(err,
				fmt.Sprintf(ERRORMSG_PROP_PARSE_ERROR, PROPNAME_INCLUDE_ROLES, strValue),
				"validation-error", getComponentInfo())
			return
		}
		builder.SetIncludeRoles(boolValue)
	}

	strValue = properties[PROPNAME_PREFIX_ROLES]
	if strValue != "" {
		boolValue, err = strconv.ParseBool(strValue)
		if err != nil {
			err = SDKErrorf(err,
				fmt.Sprintf(ERRORMSG_PROP_PARSE_ERROR, PROPNAME_PREFIX_ROLES, strValue),
				"validation-error", getComponentInfo())
			return
		}
		builder.SetPrefixRoles(boolValue)
	}

	strValue = properties[PROPNAME_AUTH_DISABLE_SSL]
	if strValue != "" {
		boolValue, err = strconv.ParseBool(strValue)
		if err != nil {
			err = SDKErrorf(err,
				fmt.Sprintf(ERRORMSG_PROP_PARSE_ERROR, PROPNAME_AUTH_DISABLE_SSL, strValue),
				"validation-error", getComponentInfo())
			return
		}
		builder.SetDisableSSLVerification(boolValue)
	}

	// The CallerExtClaim property is a map[string]string and we allow the
	// user to set it as a JSON string when using an external config property.
	// Here we retrieve it from the config as a string and unmarshal into a map.
	strValue = properties[PROPNAME_CALLER_EXT_CLAIM]
	if strValue != "" {
		var m map[string]string
		err = json.Unmarshal([]byte(strValue), &m)
		if err != nil {
			err = SDKErrorf(err,
				fmt.Sprintf(ERRORMSG_PROP_PARSE_ERROR, PROPNAME_CALLER_EXT_CLAIM, strValue),
				"validation-error", getComponentInfo())
			return
		}
		builder.SetCallerExtClaim(m)
	}

	authenticator, err = builder.Build()

	return
}

// AuthenticationType returns the authentication type for this authenticator.
func (*MCSPV2Authenticator) AuthenticationType() string {
	return AUTHTYPE_MCSPV2
}

// Authenticate adds the Authorization header to the request.
// The value will be of the form: "Authorization: Bearer <bearer-token>""
func (authenticator *MCSPV2Authenticator) Authenticate(request *http.Request) error {
	token, err := authenticator.GetToken()
	if err != nil {
		return RepurposeSDKProblem(err, "get-token-fail")
	}

	request.Header.Set("Authorization", "Bearer "+token)
	GetLogger().Debug("Authenticated outbound request (type=%s)\n", authenticator.AuthenticationType())
	return nil
}

// getTokenData returns the tokenData field from the authenticator.
func (authenticator *MCSPV2Authenticator) getTokenData() *mcspv2TokenData {
	authenticator.tokenDataMutex.Lock()
	defer authenticator.tokenDataMutex.Unlock()

	return authenticator.tokenData
}

// setTokenData sets the given mcspv2TokenData to the tokenData field of the authenticator.
func (authenticator *MCSPV2Authenticator) setTokenData(tokenData *mcspv2TokenData) {
	authenticator.tokenDataMutex.Lock()
	defer authenticator.tokenDataMutex.Unlock()

	authenticator.tokenData = tokenData
	GetLogger().Debug("setTokenData: expiration=%d, refreshTime=%d",
		authenticator.tokenData.Expiration, authenticator.tokenData.RefreshTime)
}

// GetToken: returns an access token to be used in an Authorization header.
// Whenever a new token is needed (when a token doesn't yet exist, needs to be refreshed,
// or the existing token has expired), a new access token is fetched from the token server.
func (authenticator *MCSPV2Authenticator) GetToken() (string, error) {
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
func (authenticator *MCSPV2Authenticator) synchronizedRequestToken() error {
	mcspv2RequestTokenMutex.Lock()
	defer mcspv2RequestTokenMutex.Unlock()
	// if cached token is still valid, then just continue to use it
	if authenticator.getTokenData() != nil && authenticator.getTokenData().isTokenValid() {
		return nil
	}

	return authenticator.invokeRequestTokenData()
}

// invokeRequestTokenData: requests a new token from the access server and
// unmarshals the token information to the tokenData cache. Returns
// an error if the token was unable to be fetched, otherwise returns nil
func (authenticator *MCSPV2Authenticator) invokeRequestTokenData() error {
	tokenResponse, err := authenticator.RequestToken()
	if err != nil {
		return err
	}

	GetLogger().Debug("invokeRequestTokenData(): RequestToken returned tokenResponse:\n%+v", *tokenResponse)
	tokenData, err := newMCSPV2TokenData(tokenResponse)
	if err != nil {
		tokenData = &mcspv2TokenData{}
	}

	authenticator.setTokenData(tokenData)

	return nil
}

// RequestToken fetches a new access token from the token server.
func (authenticator *MCSPV2Authenticator) RequestToken() (*MCSPV2TokenServerResponse, error) {
	builder := NewRequestBuilder(POST)
	pathParams := map[string]string{
		"scopeCollectionType": authenticator.ScopeCollectionType,
		"scopeId":             authenticator.ScopeID,
	}
	_, err := builder.ResolveRequestURL(authenticator.URL, mcspv2AuthOperationPath, pathParams)
	if err != nil {
		err = RepurposeSDKProblem(err, "url-resolve-error")
		return nil, err
	}

	// Add the request headers.
	builder.AddHeader(CONTENT_TYPE, APPLICATION_JSON)
	builder.AddHeader(Accept, APPLICATION_JSON)
	builder.AddHeader(headerNameUserAgent, authenticator.getUserAgent())

	// Add the query params.
	builder.AddQuery("includeBuiltinActions", strconv.FormatBool(authenticator.IncludeBuiltinActions))
	builder.AddQuery("includeCustomActions", strconv.FormatBool(authenticator.IncludeCustomActions))
	builder.AddQuery("includeRoles", strconv.FormatBool(authenticator.IncludeRoles))
	builder.AddQuery("prefixRolesWithDefinitionScope", strconv.FormatBool(authenticator.PrefixRoles))

	// The requestBody will consist of the apikey and (optionally) the callerExtClaim map.
	requestBody := make(map[string]any)
	requestBody["apikey"] = authenticator.ApiKey
	if len(authenticator.CallerExtClaim) > 0 {
		requestBody["callerExtClaim"] = authenticator.CallerExtClaim
	}
	_, _ = builder.SetBodyContentJSON(requestBody)

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

	tokenResponse := &MCSPV2TokenServerResponse{}
	_ = json.NewDecoder(resp.Body).Decode(tokenResponse)
	defer resp.Body.Close() // #nosec G307

	return tokenResponse, nil
}

func (authenticator *MCSPV2Authenticator) getComponentInfo() *ProblemComponent {
	return NewProblemComponent("mscp_token_server", "1.0")
}

// MCSPTokenServerResponse : This struct models a response received from the token server.
type MCSPV2TokenServerResponse struct {
	Token      string `json:"token"`
	TokenType  string `json:"token_type"`
	ExpiresIn  int64  `json:"expires_in"`
	Expiration int64  `json:"expiration"`
}

// mcspv2TokenData : This struct represents the cached information related to a fetched access token.
type mcspv2TokenData struct {
	AccessToken string
	RefreshTime int64
	Expiration  int64
}

// newMCSPV2TokenData: constructs a new mcspv2TokenData instance from the specified
// MCSPV2TokenServerResponse instance.
func newMCSPV2TokenData(tokenResponse *MCSPV2TokenServerResponse) (*mcspv2TokenData, error) {
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

	tokenData := &mcspv2TokenData{
		AccessToken: tokenResponse.Token,
		Expiration:  expireTime,
		RefreshTime: refreshTime,
	}

	GetLogger().Debug("newMCSPV2TokenData: expiration=%d, refreshTime=%d", tokenData.Expiration, tokenData.RefreshTime)

	return tokenData, nil
}

// isTokenValid: returns true iff the mcspv2TokenData instance represents a valid (non-expired) access token.
func (tokenData *mcspv2TokenData) isTokenValid() bool {
	if tokenData.AccessToken != "" && GetCurrentTime() < tokenData.Expiration {
		GetLogger().Debug("isTokenValid: Token is valid!")
		return true
	}
	GetLogger().Debug("isTokenValid: Token is NOT valid!")
	GetLogger().Debug("isTokenValid: expiration=%d, refreshTime=%d", tokenData.Expiration, tokenData.RefreshTime)
	GetLogger().Debug("GetCurrentTime(): %d\n", GetCurrentTime())
	return false
}

// needsRefresh: synchronously returns true iff the currently stored access token should be refreshed. This method also
// updates the refresh time if it determines the token needs refreshed to prevent other threads from
// making multiple refresh calls.
func (tokenData *mcspv2TokenData) needsRefresh() bool {
	mcspv2NeedsRefreshMutex.Lock()
	defer mcspv2NeedsRefreshMutex.Unlock()

	// Advance refresh by one minute
	if tokenData.RefreshTime >= 0 && GetCurrentTime() > tokenData.RefreshTime {
		tokenData.RefreshTime = GetCurrentTime() + 60
		return true
	}

	return false
}
