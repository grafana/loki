package core

// (C) Copyright IBM Corp. 2021, 2024.
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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"sync"
	"time"

	"github.com/go-openapi/strfmt"
)

// VpcInstanceAuthenticator implements an authentication scheme in which it
// retrieves an "instance identity token" and exchanges that for an IAM access token using the
// VPC Instance Metadata Service API which is available on the local compute resource (VM).
// The instance identity token is similar to an IAM apikey, except that it is managed
// automatically by the compute resource provider (VPC).
// The resulting IAM access token is then added to outbound requests in an Authorization header
// of the form:
//
//	Authorization: Bearer <access-token>
type VpcInstanceAuthenticator struct {
	// [optional] The CRN of the linked trusted IAM profile to be used as the identity of the compute resource.
	// At most one of IAMProfileCRN or IAMProfileID may be specified.  If neither one is specified, then
	// the default IAM profile defined for the compute resource will be used.
	// Default value: ""
	IAMProfileCRN string

	// [optional] The ID of the linked trusted IAM profile to be used when obtaining the IAM access token.
	// At most one of IAMProfileCRN or IAMProfileID may be specified.  If neither one is specified, then
	// the default IAM profile defined for the compute resource will be used.
	// Default value: ""
	IAMProfileID string

	// [optional] The VPC Instance Metadata Service's base endpoint URL.
	// Default value: "http://169.254.169.254"
	URL     string
	urlInit sync.Once

	// [optional] The http.Client object used to interact with the VPC Instance Metadata Service API.
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
	vpcauthDefaultIMSEndpoint             = "http://169.254.169.254"
	vpcauthOperationPathCreateAccessToken = "/instance_identity/v1/token"
	vpcauthOperationPathCreateIamToken    = "/instance_identity/v1/iam_token"
	vpcauthMetadataFlavor                 = "ibm"
	vpcauthMetadataServiceVersion         = "2022-03-01"
	vpcauthInstanceIdentityTokenLifetime  = 300
	vpcauthDefaultTimeout                 = time.Second * 30
)

// VpcInstanceAuthenticatorBuilder is used to construct an instance of the VpcInstanceAuthenticator
type VpcInstanceAuthenticatorBuilder struct {
	VpcInstanceAuthenticator
}

// NewVpcInstanceAuthenticatorBuilder returns a new builder struct that
// can be used to construct a VpcInstanceAuthenticator instance.
func NewVpcInstanceAuthenticatorBuilder() *VpcInstanceAuthenticatorBuilder {
	return &VpcInstanceAuthenticatorBuilder{}
}

// SetIAMProfileCRN sets the IAMProfileCRN field in the builder.
func (builder *VpcInstanceAuthenticatorBuilder) SetIAMProfileCRN(s string) *VpcInstanceAuthenticatorBuilder {
	builder.VpcInstanceAuthenticator.IAMProfileCRN = s
	return builder
}

// SetIAMProfileID sets the IAMProfileID field in the builder.
func (builder *VpcInstanceAuthenticatorBuilder) SetIAMProfileID(s string) *VpcInstanceAuthenticatorBuilder {
	builder.VpcInstanceAuthenticator.IAMProfileID = s
	return builder
}

// SetURL sets the URL field in the builder.
func (builder *VpcInstanceAuthenticatorBuilder) SetURL(s string) *VpcInstanceAuthenticatorBuilder {
	builder.VpcInstanceAuthenticator.URL = s
	return builder
}

// SetClient sets the Client field in the builder.
func (builder *VpcInstanceAuthenticatorBuilder) SetClient(client *http.Client) *VpcInstanceAuthenticatorBuilder {
	builder.VpcInstanceAuthenticator.Client = client
	return builder
}

// Build() returns a validated instance of the VpcInstanceAuthenticator with the config that was set in the builder.
func (builder *VpcInstanceAuthenticatorBuilder) Build() (*VpcInstanceAuthenticator, error) {
	// Make sure the config is valid.
	err := builder.VpcInstanceAuthenticator.Validate()
	if err != nil {
		return nil, RepurposeSDKProblem(err, "validation-failed")
	}

	return &builder.VpcInstanceAuthenticator, nil
}

// client returns the authenticator's http client after potentially initializing it.
func (authenticator *VpcInstanceAuthenticator) client() *http.Client {
	authenticator.clientInit.Do(func() {
		if authenticator.Client == nil {
			authenticator.Client = DefaultHTTPClient()
			authenticator.Client.Timeout = vpcauthDefaultTimeout
		}
	})
	return authenticator.Client
}

// getUserAgent returns the User-Agent header value to be included in each token request invoked by the authenticator.
func (authenticator *VpcInstanceAuthenticator) getUserAgent() string {
	authenticator.userAgentInit.Do(func() {
		authenticator.userAgent = fmt.Sprintf("%s/%s-%s %s", sdkName, "vpc-instance-authenticator", __VERSION__, SystemInfo())
	})
	return authenticator.userAgent
}

// url returns the authenticator's URL property after potentially initializing it.
func (authenticator *VpcInstanceAuthenticator) url() string {
	authenticator.urlInit.Do(func() {
		if authenticator.URL == "" {
			authenticator.URL = vpcauthDefaultIMSEndpoint
		}
	})
	return authenticator.URL
}

// newVpcInstanceAuthenticatorFromMap constructs a new VpcInstanceAuthenticator instance from a map containing
// configuration properties.
func newVpcInstanceAuthenticatorFromMap(properties map[string]string) (authenticator *VpcInstanceAuthenticator, err error) {
	if properties == nil {
		err = errors.New(ERRORMSG_PROPS_MAP_NIL)
		return nil, SDKErrorf(err, "", "missing-props", getComponentInfo())
	}

	authenticator, err = NewVpcInstanceAuthenticatorBuilder().
		SetIAMProfileCRN(properties[PROPNAME_IAM_PROFILE_CRN]).
		SetIAMProfileID(properties[PROPNAME_IAM_PROFILE_ID]).
		SetURL(properties[PROPNAME_AUTH_URL]).
		Build()

	return
}

// AuthenticationType returns the authentication type for this authenticator.
func (*VpcInstanceAuthenticator) AuthenticationType() string {
	return AUTHTYPE_VPC
}

// Authenticate adds IAM authentication information to the request.
//
// The IAM access token will be added to the request's headers in the form:
//
//	Authorization: Bearer <access-token>
func (authenticator *VpcInstanceAuthenticator) Authenticate(request *http.Request) error {
	token, err := authenticator.GetToken()
	if err != nil {
		return RepurposeSDKProblem(err, "get-token-fail")
	}

	request.Header.Set("Authorization", "Bearer "+token)
	GetLogger().Debug("Authenticated outbound request (type=%s)\n", authenticator.AuthenticationType())
	return nil
}

// getTokenData returns the tokenData field from the authenticator with synchronization.
func (authenticator *VpcInstanceAuthenticator) getTokenData() *iamTokenData {
	authenticator.tokenDataMutex.Lock()
	defer authenticator.tokenDataMutex.Unlock()

	return authenticator.tokenData
}

// setTokenData sets the 'tokenData' field in the authenticator with synchronization.
func (authenticator *VpcInstanceAuthenticator) setTokenData(tokenData *iamTokenData) {
	authenticator.tokenDataMutex.Lock()
	defer authenticator.tokenDataMutex.Unlock()

	authenticator.tokenData = tokenData
}

// Validate the authenticator's configuration.
//
// Ensures that one of IAMProfileName or IAMProfileID are specified, and the ClientId and ClientSecret pair are
// mutually inclusive.
func (authenticator *VpcInstanceAuthenticator) Validate() error {
	// Check to make sure that at most one of IAMProfileCRN or IAMProfileID are specified.
	if authenticator.IAMProfileCRN != "" && authenticator.IAMProfileID != "" {
		err := fmt.Errorf(ERRORMSG_ATMOST_ONE_PROP_ERROR, "IAMProfileCRN", "IAMProfileID")
		return SDKErrorf(err, "", "both-props", getComponentInfo())
	}

	return nil
}

// GetToken returns an IAM access token to be used in an Authorization header.
// Whenever a new IAM access token is needed (when a token doesn't yet exist or the existing token has expired),
// a new IAM access token is fetched from the token server.
func (authenticator *VpcInstanceAuthenticator) GetToken() (string, error) {
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
		err := errors.New("Error while trying to get access token")
		return "", SDKErrorf(err, "", "no-token", getComponentInfo())
	}

	return authenticator.getTokenData().AccessToken, nil
}

// vpcRequestTokenMutex is used to synchronize access to requesting a new IAM access token.
var vpcRequestTokenMutex sync.Mutex

// synchronizedRequestToken will check if the authenticator currently has
// a valid cached access token.
// If yes, then nothing else needs to be done.
// If no, then a blocking request is made to obtain a new IAM access token.
func (authenticator *VpcInstanceAuthenticator) synchronizedRequestToken() error {
	vpcRequestTokenMutex.Lock()
	defer vpcRequestTokenMutex.Unlock()
	// if cached token is still valid, then just continue to use it
	if authenticator.getTokenData() != nil && authenticator.getTokenData().isTokenValid() {
		return nil
	}

	return authenticator.invokeRequestTokenData()
}

// invokeRequestTokenData will invoke RequestToken() to obtain a new IAM access token,
// then caches the resulting "tokenData" on the authenticator.
// Returns nil if successful, or non-nil if an error occurred.
func (authenticator *VpcInstanceAuthenticator) invokeRequestTokenData() error {
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

// RequestToken will use the VPC Instance Metadata Service to (1) retrieve a fresh instance identity token
// and then (2) exchange that for an IAM access token.
func (authenticator *VpcInstanceAuthenticator) RequestToken() (iamTokenResponse *IamTokenServerResponse, err error) {
	// Retrieve the instance identity token from the VPC Instance Metadata Service.
	instanceIdentityToken, err := authenticator.retrieveInstanceIdentityToken()
	if err != nil {
		err = RepurposeSDKProblem(err, "get-ii-token-error")
		return
	}

	// Next, exchange the instance identity token for an IAM access token.
	iamTokenResponse, err = authenticator.retrieveIamAccessToken(instanceIdentityToken)
	if err != nil {
		err = RepurposeSDKProblem(err, "get-ia-token-error")
		return
	}

	return
}

// vpcTokenResponse describes the response body for both the 'create_access_token' and 'create_iam_token'
// operations (i.e. the response body has the same structure for both operations).
// Note: this struct was generated from the VPC metadata service API definition.
type vpcTokenResponse struct {
	// The access token.
	AccessToken *string `json:"access_token" validate:"required"`

	// The date and time that the access token was created.
	CreatedAt *strfmt.DateTime `json:"created_at" validate:"required"`

	// The date and time that the access token will expire.
	ExpiresAt *strfmt.DateTime `json:"expires_at" validate:"required"`

	// Time in seconds before the access token expires.
	ExpiresIn *int64 `json:"expires_in" validate:"required"`
}

// retrieveIamAccessToken will use the VPC "create_iam_token" operation to exchange the
// compute resource's instance identity token for an IAM access token that can be used
// to authenticate outbound REST requests targeting IAM-secured services.
func (authenticator *VpcInstanceAuthenticator) retrieveIamAccessToken(
	instanceIdentityToken string) (iamTokenResponse *IamTokenServerResponse, err error) {
	// Set up the request for the VPC "create_iam_token" operation.
	builder := NewRequestBuilder(POST)
	_, err = builder.ResolveRequestURL(authenticator.url(), vpcauthOperationPathCreateIamToken, nil)
	if err != nil {
		err = authenticationErrorf(err, &DetailedResponse{}, "noop", getComponentInfo())
		return
	}

	// Set the params and request body.
	builder.AddQuery("version", vpcauthMetadataServiceVersion)
	builder.AddHeader(CONTENT_TYPE, APPLICATION_JSON)
	builder.AddHeader(Accept, APPLICATION_JSON)
	builder.AddHeader(headerNameUserAgent, authenticator.getUserAgent())
	builder.AddHeader("Authorization", "Bearer "+instanceIdentityToken)

	// Next, construct the optional request body to specify the linked IAM profile.
	// We previously verified that at most one of IBMProfileCRN or IAMProfileID was specified by the user,
	// so just process them individually here and create the appropriate request body if needed.
	// If neither property was specified by the user, then no request body is sent with the request.
	var requestBody string
	if authenticator.IAMProfileCRN != "" {
		requestBody = fmt.Sprintf(`{"trusted_profile": {"crn": "%s"}}`, authenticator.IAMProfileCRN)
	}
	if authenticator.IAMProfileID != "" {
		requestBody = fmt.Sprintf(`{"trusted_profile": {"id": "%s"}}`, authenticator.IAMProfileID)
	}
	if requestBody != "" {
		_, _ = builder.SetBodyContentString(requestBody)
	}

	// Build the request.
	req, err := builder.Build()
	if err != nil {
		return nil, authenticationErrorf(err, &DetailedResponse{}, "noop", getComponentInfo())
	}

	// If debug is enabled, then dump the request.
	if GetLogger().IsLogLevelEnabled(LevelDebug) {
		buf, dumpErr := httputil.DumpRequestOut(req, req.Body != nil)
		if dumpErr == nil {
			GetLogger().Debug("Request:\n%s\n", string(buf))
		} else {
			GetLogger().Debug(fmt.Sprintf("error while attempting to log outbound request: %s", dumpErr.Error()))
		}
	}

	GetLogger().Debug("Invoking VPC 'create_iam_token' operation: %s", builder.URL)
	resp, err := authenticator.client().Do(req)
	if err != nil {
		return nil, authenticationErrorf(err, &DetailedResponse{}, "noop", getComponentInfo())
	}
	GetLogger().Debug("Returned from VPC 'create_iam_token' operation, received status code %d", resp.StatusCode)

	// If debug is enabled, then dump the response.
	if GetLogger().IsLogLevelEnabled(LevelDebug) {
		buf, dumpErr := httputil.DumpResponse(resp, resp.Body != nil)
		if dumpErr == nil {
			GetLogger().Debug("Response:\n%s\n", string(buf))
		} else {
			GetLogger().Debug(fmt.Sprintf("error while attempting to log inbound response: %s", dumpErr.Error()))
		}
	}

	// Check for a bad status code and handle an operation error.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detailedResponse, responseError := processErrorResponse(resp)
		err = authenticationErrorf(responseError, detailedResponse, "create_iam_token", authenticator.getComponentInfo())

		// The err Summary is typically the message computed for the HTTPError instance in
		// processErrorResponse(). If the response body is non-JSON, the message will be generic
		// text based on the status code but authenticators have always used the stringified
		// RawResult, so update that here for compatilibility.
		vpcErrorMsg := responseError.Summary
		if detailedResponse.RawResult != nil {
			// RawResult is only populated if the response body is
			// non-JSON and we couldn't extract a message.
			vpcErrorMsg = string(detailedResponse.RawResult)
		}

		err.(*AuthenticationError).Summary = fmt.Sprintf(ERRORMSG_VPCMDS_OPERATION_ERROR, detailedResponse.StatusCode, builder.URL, vpcErrorMsg)

		return
	}

	// Good response, so unmarshal the response body into a vpcTokenResponse instance.
	tokenResponse := &vpcTokenResponse{}
	_ = json.NewDecoder(resp.Body).Decode(tokenResponse)
	defer resp.Body.Close() // #nosec G307

	// Finally, convert the vpcTokenResponse instance into an IamTokenServerResponse to maintain
	// consistency with other IAM-based authenticators.
	iamTokenResponse = &IamTokenServerResponse{
		AccessToken: *tokenResponse.AccessToken,
		ExpiresIn:   *tokenResponse.ExpiresIn,
		Expiration:  time.Time(*tokenResponse.ExpiresAt).Unix(),
	}

	return
}

// retrieveInstanceIdentityToken retrieves the local compute resource's instance identity token using
// the "create_access_token" operation of the local VPC Instance Metadata Service API.
func (authenticator *VpcInstanceAuthenticator) retrieveInstanceIdentityToken() (instanceIdentityToken string, err error) {
	// Set up the request to invoke the "create_access_token" operation.
	builder := NewRequestBuilder(PUT)
	_, err = builder.ResolveRequestURL(authenticator.url(), vpcauthOperationPathCreateAccessToken, nil)
	if err != nil {
		err = authenticationErrorf(err, &DetailedResponse{}, "noop", getComponentInfo())
		return
	}

	// Set the params and request body.
	builder.AddQuery("version", vpcauthMetadataServiceVersion)
	builder.AddHeader(CONTENT_TYPE, APPLICATION_JSON)
	builder.AddHeader(Accept, APPLICATION_JSON)
	builder.AddHeader("Metadata-Flavor", vpcauthMetadataFlavor)

	requestBody := fmt.Sprintf(`{"expires_in": %d}`, vpcauthInstanceIdentityTokenLifetime)
	_, _ = builder.SetBodyContentString(requestBody)

	// Build the request.
	req, err := builder.Build()
	if err != nil {
		err = authenticationErrorf(err, &DetailedResponse{}, "noop", getComponentInfo())
		return
	}

	// If debug is enabled, then dump the request.
	if GetLogger().IsLogLevelEnabled(LevelDebug) {
		buf, dumpErr := httputil.DumpRequestOut(req, req.Body != nil)
		if dumpErr == nil {
			GetLogger().Debug("Request:\n%s\n", string(buf))
		} else {
			GetLogger().Debug(fmt.Sprintf("error while attempting to log outbound request: %s", dumpErr.Error()))
		}
	}

	// Invoke the request.
	GetLogger().Debug("Invoking VPC 'create_access_token' operation: %s", builder.URL)
	resp, err := authenticator.client().Do(req)
	if err != nil {
		err = authenticationErrorf(err, &DetailedResponse{}, "noop", getComponentInfo())
		return
	}
	GetLogger().Debug("Returned from VPC 'create_access_token' operation, received status code %d", resp.StatusCode)

	// If debug is enabled, then dump the response.
	if GetLogger().IsLogLevelEnabled(LevelDebug) {
		buf, dumpErr := httputil.DumpResponse(resp, resp.Body != nil)
		if dumpErr == nil {
			GetLogger().Debug("Response:\n%s\n", string(buf))
		} else {
			GetLogger().Debug(fmt.Sprintf("error while attempting to log inbound response: %s", dumpErr.Error()))
		}
	}

	// Check for a bad status code and handle the operation error.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detailedResponse, responseError := processErrorResponse(resp)
		err = authenticationErrorf(responseError, detailedResponse, "create_access_token", authenticator.getComponentInfo())

		// The err Summary is typically the message computed for the HTTPError instance in
		// processErrorResponse(). If the response body is non-JSON, the message will be generic
		// text based on the status code but authenticators have always used the stringified
		// RawResult, so update that here for compatilibility.
		vpcErrorMsg := responseError.Summary
		if detailedResponse.RawResult != nil {
			// RawResult is only populated if the response body is
			// non-JSON and we couldn't extract a message.
			vpcErrorMsg = string(detailedResponse.RawResult)
		}

		err.(*AuthenticationError).Summary = fmt.Sprintf(ERRORMSG_VPCMDS_OPERATION_ERROR, detailedResponse.StatusCode, builder.URL, vpcErrorMsg)

		return
	}

	// VPC "create_access_token" operation must have worked, so unmarshal the operation response body
	// and retrieve the instance identity token value.
	operationResponse := &vpcTokenResponse{}
	_ = json.NewDecoder(resp.Body).Decode(operationResponse)
	defer resp.Body.Close() // #nosec G307

	// The instance identity token is returned in the "access_token" field of the response object.
	instanceIdentityToken = *operationResponse.AccessToken

	return
}

// This should only be used for AuthenticationError instances that actually deal with
// an HTTP error (i.e. do not have a blank DetailedResponse object - they can be scoped
// to the SDK core system).
func (authenticator *VpcInstanceAuthenticator) getComponentInfo() *ProblemComponent {
	return NewProblemComponent("vpc_instance_metadata", vpcauthMetadataServiceVersion)
}
