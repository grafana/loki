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
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	cleanhttp "github.com/hashicorp/go-cleanhttp"
	retryablehttp "github.com/hashicorp/go-retryablehttp"
)

const (
	headerNameUserAgent = "User-Agent"
	sdkName             = "ibm-go-sdk-core"
)

// ServiceOptions is a struct of configuration values for a service.
type ServiceOptions struct {
	// This is the base URL associated with the service instance. This value will
	// be combined with the paths for each operation to form the request URL
	// [required].
	URL string

	// Authenticator holds the authenticator implementation to be used by the
	// service instance to authenticate outbound requests, typically by adding the
	// HTTP "Authorization" header.
	Authenticator Authenticator

	// EnableGzipCompression indicates whether or not request bodies
	// should be gzip-compressed.
	// This field has no effect on response bodies.
	// If enabled, the Body field will be gzip-compressed and
	// the "Content-Encoding" header will be added to the request with the
	// value "gzip".
	EnableGzipCompression bool
}

// BaseService implements the common functionality shared by generated services
// to manage requests and responses, authenticate outbound requests, etc.
type BaseService struct {
	// Configuration values for a service.
	Options *ServiceOptions

	// A set of "default" http headers to be included with each outbound request.
	DefaultHeaders http.Header

	// The HTTP Client used to send requests and receive responses.
	Client *http.Client

	// The value to be used for the "User-Agent" HTTP header that is added to each
	// outbound request. If this value is not set, then a default value will be
	// used for the header.
	UserAgent string
}

// NewBaseService constructs a new instance of BaseService. Validation on input
// parameters and service options will be performed before instance creation.
func NewBaseService(options *ServiceOptions) (*BaseService, error) {
	if HasBadFirstOrLastChar(options.URL) {
		err := fmt.Errorf(ERRORMSG_PROP_INVALID, "URL")
		return nil, SDKErrorf(err, "", "bad-char", getComponentInfo())
	}

	if IsNil(options.Authenticator) {
		err := errors.New(ERRORMSG_NO_AUTHENTICATOR)
		return nil, SDKErrorf(err, "", "missing-auth", getComponentInfo())
	}

	if err := options.Authenticator.Validate(); err != nil {
		err = RepurposeSDKProblem(err, "auth-validation-failed")
		return nil, err
	}

	service := BaseService{
		Options: options,

		Client: DefaultHTTPClient(),
	}

	// Set a default value for the User-Agent http header.
	service.SetUserAgent(service.buildUserAgent())

	return &service, nil
}

// Clone will return a copy of "service" suitable for use by a
// generated service instance to process requests.
func (service *BaseService) Clone() *BaseService {
	if IsNil(service) {
		return nil
	}

	// First, copy the service options struct.
	serviceOptions := *service.Options

	// Next, make a copy the service struct, then use the copy of the service options.
	// Note, we'll re-use the "Client" instance from the original BaseService instance.
	clone := *service
	clone.Options = &serviceOptions

	return &clone
}

// ConfigureService updates the service with external configuration values.
func (service *BaseService) ConfigureService(serviceName string) error {
	GetLogger().Debug("Configuring BaseService instance with service name: %s\n", serviceName)

	// Try to load service properties from external config.
	serviceProps, err := getServiceProperties(serviceName)
	if err != nil {
		err = RepurposeSDKProblem(err, "get-props-error")
		return err
	}

	// If we were able to load any properties for this service, then check to see if the
	// service-level properties were present and set them on the service if so.
	if serviceProps != nil {

		// URL
		if url, ok := serviceProps[PROPNAME_SVC_URL]; ok && url != "" {
			err := service.SetServiceURL(url)
			if err != nil {
				err = RepurposeSDKProblem(err, "set-url-fail")
				return err
			}
		}

		// DISABLE_SSL
		if disableSSL, ok := serviceProps[PROPNAME_SVC_DISABLE_SSL]; ok && disableSSL != "" {
			// Convert the config string to bool.
			boolValue, err := strconv.ParseBool(disableSSL)
			if err != nil {
				boolValue = false
			}

			// If requested, disable SSL.
			if boolValue {
				service.DisableSSLVerification()
			}
		}

		// ENABLE_GZIP
		if enableGzip, ok := serviceProps[PROPNAME_SVC_ENABLE_GZIP]; ok && enableGzip != "" {
			// Convert the config string to bool.
			boolValue, err := strconv.ParseBool(enableGzip)
			if err == nil {
				service.SetEnableGzipCompression(boolValue)
			}
		}

		// ENABLE_RETRIES
		// If "ENABLE_RETRIES" is set to true, then we'll also try to retrieve "MAX_RETRIES" and
		// "RETRY_INTERVAL".  If those are not specified, we'll use 0 to trigger a default value for each.
		if enableRetries, ok := serviceProps[PROPNAME_SVC_ENABLE_RETRIES]; ok && enableRetries != "" {
			boolValue, err := strconv.ParseBool(enableRetries)
			if boolValue && err == nil {
				var maxRetries int = 0
				var retryInterval time.Duration = 0

				var s string
				var ok bool
				if s, ok = serviceProps[PROPNAME_SVC_MAX_RETRIES]; ok && s != "" {
					n, err := strconv.ParseInt(s, 10, 32)
					if err == nil {
						maxRetries = int(n)
					}
				}

				if s, ok = serviceProps[PROPNAME_SVC_RETRY_INTERVAL]; ok && s != "" {
					n, err := strconv.ParseInt(s, 10, 32)
					if err == nil {
						retryInterval = time.Duration(n) * time.Second
					}
				}

				service.EnableRetries(maxRetries, retryInterval)
			}
		}
	}
	return nil
}

// SetURL sets the service URL.
//
// Deprecated: use SetServiceURL instead.
func (service *BaseService) SetURL(url string) error {
	return RepurposeSDKProblem(service.SetServiceURL(url), "set-url-fail")
}

// SetServiceURL sets the service URL.
func (service *BaseService) SetServiceURL(url string) error {
	if HasBadFirstOrLastChar(url) {
		err := fmt.Errorf(ERRORMSG_PROP_INVALID, "URL")
		return SDKErrorf(err, "", "bad-char", getComponentInfo())
	}

	service.Options.URL = url
	GetLogger().Debug("Set service URL: %s\n", url)
	return nil
}

// GetServiceURL returns the service URL.
func (service *BaseService) GetServiceURL() string {
	return service.Options.URL
}

// SetDefaultHeaders sets HTTP headers to be sent in every request.
func (service *BaseService) SetDefaultHeaders(headers http.Header) {
	service.DefaultHeaders = headers
}

// SetHTTPClient will set "client" as the http.Client instance to be used
// to invoke individual HTTP requests.
// If automatic retries are currently enabled on "service", then
// "client" will be set as the embedded client instance within
// the retryable client; otherwise "client" will be stored
// directly on "service".
func (service *BaseService) SetHTTPClient(client *http.Client) {
	setMinimumTLSVersion(client)

	if isRetryableClient(service.Client) {
		// If "service" is currently holding a retryable client,
		// then set "client" as the embedded client used for individual requests.
		tr := service.Client.Transport.(*retryablehttp.RoundTripper)
		tr.Client.HTTPClient = client
	} else {
		// Otherwise, just hang "client" directly off the base service.
		service.Client = client
	}
}

// GetHTTPClient will return the http.Client instance used
// to invoke individual HTTP requests.
// If automatic retries are enabled, the returned value will
// be the http.Client instance embedded within the retryable client.
// If automatic retries are not enabled, then the returned value
// will simply be the "Client" field of the base service.
func (service *BaseService) GetHTTPClient() *http.Client {
	if isRetryableClient(service.Client) {
		tr := service.Client.Transport.(*retryablehttp.RoundTripper)
		return tr.Client.HTTPClient
	}
	return service.Client
}

// DisableSSLVerification will configure the service to
// skip the verification of server certificates and hostnames.
// This will make the client susceptible to "man-in-the-middle"
// attacks. This should be used only for testing or in secure
// environments.
func (service *BaseService) DisableSSLVerification() {
	// Make sure we have a non-nil client hanging off the BaseService.
	if service.Client == nil {
		service.Client = DefaultHTTPClient()
	}

	client := service.GetHTTPClient()
	if tr, ok := client.Transport.(*http.Transport); tr != nil && ok {
		// If no TLS config, then create a new one.
		if tr.TLSClientConfig == nil {
			tr.TLSClientConfig = &tls.Config{} // #nosec G402
		}

		// Disable server ssl cert & hostname verification.
		tr.TLSClientConfig.InsecureSkipVerify = true // #nosec G402
	}
	GetLogger().Debug("Disabled SSL verification in HTTP client")
}

// IsSSLDisabled returns true if and only if the service's http.Client instance
// is configured to skip verification of server SSL certificates.
func (service *BaseService) IsSSLDisabled() bool {
	client := service.GetHTTPClient()
	if client != nil {
		if tr, ok := client.Transport.(*http.Transport); tr != nil && ok {
			if tr.TLSClientConfig != nil {
				return tr.TLSClientConfig.InsecureSkipVerify
			}
		}
	}
	return false
}

// setMinimumTLSVersion sets the minimum TLS version required by the client to TLS v1.2
func setMinimumTLSVersion(client *http.Client) {
	if tr, ok := client.Transport.(*http.Transport); tr != nil && ok {
		if tr.TLSClientConfig == nil {
			tr.TLSClientConfig = &tls.Config{} // #nosec G402
		}

		tr.TLSClientConfig.MinVersion = tls.VersionTLS12
	}
}

// SetEnableGzipCompression sets the service's EnableGzipCompression field
func (service *BaseService) SetEnableGzipCompression(enableGzip bool) {
	service.Options.EnableGzipCompression = enableGzip
}

// GetEnableGzipCompression returns the service's EnableGzipCompression field
func (service *BaseService) GetEnableGzipCompression() bool {
	return service.Options.EnableGzipCompression
}

// buildUserAgent builds the user agent string.
func (service *BaseService) buildUserAgent() string {
	return fmt.Sprintf("%s-%s %s", sdkName, __VERSION__, SystemInfo())
}

// SetUserAgent sets the user agent value.
func (service *BaseService) SetUserAgent(userAgent string) {
	if userAgent == "" {
		userAgent = service.buildUserAgent()
	}
	service.UserAgent = userAgent
	GetLogger().Debug("Set User-Agent: %s\n", userAgent)
}

// Request invokes the specified HTTP request and returns the response.
//
// Parameters:
// req: the http.Request object that holds the request information
//
// result: a pointer to the operation result.  This should be one of:
//   - *io.ReadCloser (for a byte-stream type response)
//   - *<primitive>, *[]<primitive>, *map[string]<primitive>
//   - *map[string]json.RawMessage, *[]json.RawMessage
//
// Return values:
// detailedResponse: a DetailedResponse instance containing the status code, headers, etc.
//
// err: a non-nil error object if an error occurred
func (service *BaseService) Request(req *http.Request, result interface{}) (detailedResponse *DetailedResponse, err error) {
	// Add default headers.
	if service.DefaultHeaders != nil {
		for k, v := range service.DefaultHeaders {
			req.Header.Add(k, strings.Join(v, ""))
		}

		// After adding the default headers, make one final check to see if the user
		// specified the "Host" header within the default headers.
		// This needs to be handled separately because it will be ignored by
		// the Request.Write() method.
		host := service.DefaultHeaders.Get("Host")
		if host != "" {
			req.Host = host
		}
	}

	// Add the default User-Agent header if not already present.
	userAgent := req.Header.Get(headerNameUserAgent)
	if userAgent == "" {
		req.Header.Add(headerNameUserAgent, service.UserAgent)
	}

	// Add authentication to the outbound request.
	if IsNil(service.Options.Authenticator) {
		err = errors.New(ERRORMSG_NO_AUTHENTICATOR)
		err = SDKErrorf(err, "", "missing-auth", getComponentInfo())
		return
	}

	authenticateError := service.Options.Authenticator.Authenticate(req)
	if authenticateError != nil {
		var authErr *AuthenticationError
		var sdkErr *SDKProblem
		if errors.As(authenticateError, &authErr) {
			detailedResponse = authErr.Response
			err = SDKErrorf(authErr.HTTPProblem, fmt.Sprintf(ERRORMSG_AUTHENTICATE_ERROR, authErr.Error()), "auth-request-failed", getComponentInfo())
		} else if errors.As(authenticateError, &sdkErr) {
			sdkErr := RepurposeSDKProblem(authenticateError, "auth-failed")
			// For compatibility.
			sdkErr.(*SDKProblem).Summary = fmt.Sprintf(ERRORMSG_AUTHENTICATE_ERROR, authenticateError.Error())
			err = sdkErr
		} else {
			// External authenticators that implement the core interface might return a standard error.
			// Handle that by wrapping it here.
			err = SDKErrorf(err, fmt.Sprintf(ERRORMSG_AUTHENTICATE_ERROR, authenticateError.Error()), "custom-auth-failed", getComponentInfo())
		}
		return
	}

	// If debug is enabled, then dump the request.
	if GetLogger().IsLogLevelEnabled(LevelDebug) {
		buf, dumpErr := httputil.DumpRequestOut(req, !IsNil(req.Body))
		if dumpErr == nil {
			GetLogger().Debug("Request:\n%s\n", RedactSecrets(string(buf)))
		} else {
			GetLogger().Debug("error while attempting to log outbound request: %s", dumpErr.Error())
		}
	}

	// Invoke the request, then check for errors during the invocation.
	GetLogger().Debug("Sending HTTP request message...")
	var httpResponse *http.Response
	httpResponse, err = service.Client.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), SSL_CERTIFICATION_ERROR) {
			err = errors.New(ERRORMSG_SSL_VERIFICATION_FAILED + "\n" + err.Error())
		}
		err = SDKErrorf(err, "", "no-connection-made", getComponentInfo())
		return
	}
	GetLogger().Debug("Received HTTP response message, status code %d", httpResponse.StatusCode)

	// If debug is enabled, then dump the response.
	if GetLogger().IsLogLevelEnabled(LevelDebug) {
		buf, dumpErr := httputil.DumpResponse(httpResponse, !IsNil(httpResponse.Body))
		if err == nil {
			GetLogger().Debug("Response:\n%s\n", RedactSecrets(string(buf)))
		} else {
			GetLogger().Debug("error while attempting to log inbound response: %s", dumpErr.Error())
		}
	}

	// If the operation was unsuccessful, then set up and return
	// the DetailedResponse and error objects appropriately.
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		detailedResponse, err = processErrorResponse(httpResponse)
		err = RepurposeSDKProblem(err, "error-response")
		return
	}

	// Operation was successful and we are expecting a response, so process the response.
	detailedResponse, contentType := getDetailedResponseAndContentType(httpResponse)
	if !IsNil(result) {
		resultType := reflect.TypeOf(result).String()

		// If 'result' is a io.ReadCloser, then pass the response body back reflectively via 'result'
		// and bypass any further unmarshalling of the response.
		if resultType == "*io.ReadCloser" {
			rResult := reflect.ValueOf(result).Elem()
			rResult.Set(reflect.ValueOf(httpResponse.Body))
			detailedResponse.Result = httpResponse.Body
		} else {

			// First, read the response body into a byte array.
			defer httpResponse.Body.Close() // #nosec G307
			responseBody, readErr := io.ReadAll(httpResponse.Body)
			if readErr != nil {
				err = fmt.Errorf(ERRORMSG_READ_RESPONSE_BODY, readErr.Error())
				err = SDKErrorf(err, "", "cant-read-success-res-body", getComponentInfo())
				return
			}

			// If the response body is empty, then skip any attempt to deserialize and just return
			if len(responseBody) == 0 {
				return
			}

			// If the content-type indicates JSON, then unmarshal the response body as JSON.
			if IsJSONMimeType(contentType) {
				// Decode the byte array as JSON.
				decodeErr := json.NewDecoder(bytes.NewReader(responseBody)).Decode(result)
				if decodeErr != nil {
					// Error decoding the response body.
					// Return the response body in RawResult, along with an error.
					err = fmt.Errorf(ERRORMSG_UNMARSHAL_RESPONSE_BODY, decodeErr.Error())
					err = SDKErrorf(err, "", "res-body-decode-error", getComponentInfo())
					detailedResponse.RawResult = responseBody
					return
				}

				// Decode step was successful. Return the decoded response object in the Result field.
				detailedResponse.Result = reflect.ValueOf(result).Elem().Interface()
				return
			}

			// Check to see if the caller wanted the response body as a string.
			// If the caller passed in 'result' as the address of *string,
			// then we'll reflectively set result to point to it.
			if resultType == "**string" {
				responseString := string(responseBody)
				rResult := reflect.ValueOf(result).Elem()
				rResult.Set(reflect.ValueOf(&responseString))

				// And set the string in the Result field.
				detailedResponse.Result = &responseString
			} else if resultType == "*[]uint8" { // byte is an alias for uint8
				rResult := reflect.ValueOf(result).Elem()
				rResult.Set(reflect.ValueOf(responseBody))

				// And set the byte slice in the Result field.
				detailedResponse.Result = responseBody
			} else {
				// At this point, we don't know how to set the result field, so we have to return an error.
				// But make sure we save the bytes we read in the DetailedResponse for debugging purposes
				detailedResponse.Result = responseBody
				err = fmt.Errorf(ERRORMSG_UNEXPECTED_RESPONSE, contentType, resultType)
				err = SDKErrorf(err, "", "unparsable-result-field", getComponentInfo())
				return
			}
		}
	} else if !IsNil(httpResponse.Body) {
		// We weren't expecting a response, but we have a reponse body,
		// so we need to close it now since we're not going to consume it.
		_ = httpResponse.Body.Close()
	}

	return
}

// getDetailedResponseAndContentType starts to populate the DetailedResponse
// and extracts the Content-Type header value from the response.
func getDetailedResponseAndContentType(httpResponse *http.Response) (detailedResponse *DetailedResponse, contentType string) {
	if httpResponse != nil {
		contentType = httpResponse.Header.Get(CONTENT_TYPE)
		detailedResponse = &DetailedResponse{
			StatusCode: httpResponse.StatusCode,
			Headers:    httpResponse.Header,
		}
	}
	return
}

func processErrorResponse(httpResponse *http.Response) (detailedResponse *DetailedResponse, err *SDKProblem) {
	detailedResponse, contentType := getDetailedResponseAndContentType(httpResponse)

	var responseBody []byte

	// First, read the response body into a byte array.
	if !IsNil(httpResponse.Body) {
		var readErr error

		defer httpResponse.Body.Close() // #nosec G307
		responseBody, readErr = io.ReadAll(httpResponse.Body)
		if readErr != nil {
			httpErr := httpErrorf(http.StatusText(httpResponse.StatusCode), detailedResponse)
			err = SDKErrorf(httpErr, fmt.Sprintf(ERRORMSG_READ_RESPONSE_BODY, readErr.Error()), "cant-read-error-body", getComponentInfo())
			return
		}
	}

	// If the responseBody is empty, then just return a generic error based on the status code.
	if len(responseBody) == 0 {
		httpErr := httpErrorf(http.StatusText(httpResponse.StatusCode), detailedResponse)
		err = SDKErrorf(httpErr, "", "no-error-body", getComponentInfo())
		return
	}

	// For a JSON-based error response body, decode it into a map (generic JSON object).
	if IsJSONMimeType(contentType) {
		// Return the error response body as a map, along with an
		// error object containing our best guess at an error message.
		responseMap, decodeErr := decodeAsMap(responseBody)
		if decodeErr == nil {
			detailedResponse.Result = responseMap
			errorMsg := getErrorMessage(responseMap, detailedResponse.StatusCode)
			httpErr := httpErrorf(errorMsg, detailedResponse)
			err = SDKErrorf(httpErr, "", "json-error-body", getComponentInfo())
			return
		}
	}

	// For a non-JSON response or if we tripped while decoding the JSON response,
	// just return the response body byte array in the RawResult field along with
	// an error object that contains the generic error message for the status code.
	detailedResponse.RawResult = responseBody
	httpErr := httpErrorf(http.StatusText(httpResponse.StatusCode), detailedResponse)
	err = SDKErrorf(httpErr, "", "non-json-error-body", getComponentInfo())
	return
}

// Errors is a struct used to hold an array of errors received in an operation
// response.
type Errors struct {
	Errors []Error `json:"errors,omitempty"`
}

// Error is a struct used to represent a single error received in an operation
// response.
type Error struct {
	Message string `json:"message,omitempty"`
	Code    string `json:"code,omitempty"`
}

// decodeAsMap: Decode the specified JSON byte-stream into a map (akin to a generic JSON object).
// Notes:
//  1. This function will return the map (result of decoding the byte-stream) as well as the raw
//     byte buffer.  We return the byte buffer in addition to the decoded map so that the caller can
//     re-use (if necessary) the stream of bytes after we've consumed them via the JSON decode step.
//  2. The primary return value of this function will be:
//     a) an instance of map[string]interface{} if the specified byte-stream was successfully
//     decoded as JSON.
//     b) the string form of the byte-stream if the byte-stream could not be successfully
//     decoded as JSON.
//  3. This function will close the io.ReadCloser before returning.
func decodeAsMap(byteBuffer []byte) (result map[string]interface{}, err error) {
	err = json.NewDecoder(bytes.NewReader(byteBuffer)).Decode(&result)
	if err != nil {
		err = SDKErrorf(err, "", "decode-error", getComponentInfo())
	}
	return
}

// getErrorMessage: try to retrieve an error message from the decoded response body (map).
func getErrorMessage(responseMap map[string]interface{}, statusCode int) string {
	// If the response contained the "errors" field, then try to deserialize responseMap
	// into an array of Error structs, then return the first entry's "Message" field.
	if _, ok := responseMap["errors"]; ok {
		var errors Errors
		responseBuffer, _ := json.Marshal(responseMap)
		if err := json.Unmarshal(responseBuffer, &errors); err == nil {
			if len(errors.Errors) > 0 {
				return errors.Errors[0].Message
			}

			return ""
		}
	}

	// Return the "error" field if present and is a string.
	if val, ok := responseMap["error"]; ok {
		errorMsg, ok := val.(string)
		if ok {
			return errorMsg
		}
	}

	// Return the "message" field if present and is a string.
	if val, ok := responseMap["message"]; ok {
		errorMsg, ok := val.(string)
		if ok {
			return errorMsg
		}
	}

	// Finally, return the "errorMessage" field if present and is a string.
	if val, ok := responseMap["errorMessage"]; ok {
		errorMsg, ok := val.(string)
		if ok {
			return errorMsg
		}
	}

	// If we couldn't find an error message above, just return the generic text
	// for the status code.
	return http.StatusText(statusCode)
}

// getErrorCode tries to retrieve an error code from the decoded response body (map).
func getErrorCode(responseMap map[string]interface{}) string {
	// If the response contained the "errors" field, then try to deserialize responseMap
	// into an array of Error structs, then return the first entry's "Message" field.
	if _, ok := responseMap["errors"]; ok {
		var errors Errors
		responseBuffer, _ := json.Marshal(responseMap)
		if err := json.Unmarshal(responseBuffer, &errors); err == nil {
			return errors.Errors[0].Code
		}
	}

	// Return the "code" field if present and is a string.
	if val, ok := responseMap["code"]; ok {
		errorCode, ok := val.(string)
		if ok {
			return errorCode
		}
	}

	// Return the "errorCode" field if present and is a string.
	if val, ok := responseMap["errorCode"]; ok {
		errorCode, ok := val.(string)
		if ok {
			return errorCode
		}
	}

	// If we couldn't find a code, return an empty string
	return ""
}

// isRetryableClient() will return true if and only if "client" is
// an http.Client instance that is configured for automatic retries.
// A retryable client is a client whose transport is a
// retryablehttp.RoundTripper instance.
func isRetryableClient(client *http.Client) bool {
	var isRetryable bool = false
	if client != nil && client.Transport != nil {
		_, isRetryable = client.Transport.(*retryablehttp.RoundTripper)
	}
	return isRetryable
}

// EnableRetries will configure the service to perform automatic retries of failed requests.
// If "maxRetries" and/or "maxRetryInterval" are specified as 0, then default values
// are used instead.
//
// In a scenario where retries ARE NOT enabled:
// - BaseService.Client will be a "normal" http.Client instance used to invoke requests
// - BaseService.Client.Transport will be an instance of the default http.RoundTripper
// - BaseService.Client.Do() calls http.RoundTripper.RoundTrip() to invoke the request
// - Only one http.Client instance needed/used (BaseService.Client) in this scenario
// - Result: "normal" request processing without any automatic retries being performed
//
// In a scenario where retries ARE enabled:
//   - BaseService.Client will be a "shim" http.Client instance
//   - BaseService.Client.Transport will be an instance of retryablehttp.RoundTripper
//   - BaseService.Client.Do() calls retryablehttp.RoundTripper.RoundTrip() (via the shim)
//     to invoke the request
//   - The retryablehttp.RoundTripper instance is configured with the retryablehttp.Client
//     instance which holds the various retry config properties (max retries, max interval, etc.)
//   - The retryablehttp.RoundTripper.RoundTrip() method triggers the retry logic in the retryablehttp.Client
//   - The retryablehttp.Client instance's HTTPClient field holds a "normal" http.Client instance,
//     which is used to invoke individual requests within the retry loop.
//   - To summarize, there are three client instances used for request processing in this scenario:
//     1. The "shim" http.Client instance (BaseService.Client)
//     2. The retryablehttp.Client instance that implements the retry logic
//     3. The "normal" http.Client instance embedded in the retryablehttp.Client which is used to invoke
//     individual requests within the retry logic
//   - Result: Each request is invoked such that the automatic retry logic is employed
func (service *BaseService) EnableRetries(maxRetries int, maxRetryInterval time.Duration) {
	if isRetryableClient(service.Client) {
		// If retries are already enabled, then we just need to adjust
		// the retryable client's config using "maxRetries" and "maxRetryInterval".
		tr := service.Client.Transport.(*retryablehttp.RoundTripper)
		if maxRetries > 0 {
			tr.Client.RetryMax = maxRetries
		}
		if maxRetryInterval > 0 {
			tr.Client.RetryWaitMax = maxRetryInterval
		}
	} else {
		// Otherwise, we need to create a new retryable client instance
		// and hang it off the base service.
		client := NewRetryableClientWithHTTPClient(service.Client)
		if maxRetries > 0 {
			client.RetryMax = maxRetries
		}
		if maxRetryInterval > 0 {
			client.RetryWaitMax = maxRetryInterval
		}

		// Hang the retryable client off the base service via the "shim" client.
		service.Client = client.StandardClient()
	}
	GetLogger().Debug("Enabled retries; maxRetries=%d, maxRetryInterval=%s\n", maxRetries, maxRetryInterval.String())
}

// DisableRetries will disable automatic retries in the service.
func (service *BaseService) DisableRetries() {
	if isRetryableClient(service.Client) {
		// If the current client hanging off the base service is retryable,
		// then we need to get ahold of the embedded http.Client instance
		// and set that on the base service and effectively remove
		// the retryable client instance.
		tr := service.Client.Transport.(*retryablehttp.RoundTripper)
		service.Client = tr.Client.HTTPClient

		GetLogger().Debug("Disabled retries\n")
	}
}

// DefaultHTTPClient returns a non-retryable http client with default configuration.
func DefaultHTTPClient() *http.Client {
	client := cleanhttp.DefaultPooledClient()
	setMinimumTLSVersion(client)
	return client
}

// httpLogger is a shim layer used to allow the Go core's logger to be used with the retryablehttp interfaces.
type httpLogger struct{}

func (l *httpLogger) Printf(format string, inserts ...interface{}) {
	if GetLogger().IsLogLevelEnabled(LevelDebug) {
		msg := fmt.Sprintf(format, inserts...)
		GetLogger().Log(LevelDebug, RedactSecrets(msg))
	}
}

// NewRetryableHTTPClient returns a new instance of a retryable client
// with a default configuration that supports Go SDK usage.
func NewRetryableHTTPClient() *retryablehttp.Client {
	return NewRetryableClientWithHTTPClient(nil)
}

// NewRetryableClientWithHTTPClient will return a new instance of a
// retryable client, using "httpClient" as the embedded client used to
// invoke individual requests within the retry logic.
// If "httpClient" is passed in as nil, then a default HTTP client will be
// used as the embedded client instead.
func NewRetryableClientWithHTTPClient(httpClient *http.Client) *retryablehttp.Client {
	client := retryablehttp.NewClient()
	client.Logger = &httpLogger{}
	client.CheckRetry = IBMCloudSDKRetryPolicy
	client.Backoff = IBMCloudSDKBackoffPolicy
	client.ErrorHandler = retryablehttp.PassthroughErrorHandler

	if httpClient != nil {
		// If a non-nil http client was passed in, then let's use that
		// as our embedded client used to invoke individual requests.
		client.HTTPClient = httpClient
	} else {
		// Otherwise, we'll use construct a default HTTP client and use that
		client.HTTPClient = DefaultHTTPClient()
	}

	return client
}

var (
	// A regular expression to match the error returned by net/http when the
	// configured number of redirects is exhausted. This error isn't typed
	// specifically so we resort to matching on the error string.
	redirectsErrorRe = regexp.MustCompile(`stopped after \d+ redirects\z`)

	// A regular expression to match the error returned by net/http when the
	// scheme specified in the URL is invalid. This error isn't typed
	// specifically so we resort to matching on the error string.
	schemeErrorRe = regexp.MustCompile(`unsupported protocol scheme`)

	// A regular expression to match the error returned by net/http when a
	// request header or value is invalid. This error isn't typed
	// specifically so we resort to matching on the error string.
	invalidHeaderErrorRe = regexp.MustCompile(`invalid header`)

	// A regular expression to match the error returned by net/http when the
	// TLS certificate is not trusted. This error isn't typed
	// specifically so we resort to matching on the error string.
	notTrustedErrorRe = regexp.MustCompile(`certificate is not trusted|certificate is signed by unknown authority`)
)

// IBMCloudSDKRetryPolicy provides a default implementation of the CheckRetry interface
// associated with a retryablehttp.Client.
// This function will return true if the specified request/response should be retried.
func IBMCloudSDKRetryPolicy(ctx context.Context, resp *http.Response, err error) (bool, error) {
	// This logic was adapted from go-retryablehttp.ErrorPropagatedRetryPolicy().

	if GetLogger().IsLogLevelEnabled(LevelDebug) {
		// Compile the details to be included in the debug message.
		var details []string
		if resp != nil {
			details = append(details, fmt.Sprintf("status_code=%d", resp.StatusCode))
			if resp.Request != nil {
				details = append(details, fmt.Sprintf("method=%s", resp.Request.Method))
				details = append(details, fmt.Sprintf("url=%s", resp.Request.URL.Redacted()))
			}
		}
		if err != nil {
			details = append(details, fmt.Sprintf("error=%s", err.Error()))
		} else {
			details = append(details, "error=nil")
		}

		GetLogger().Debug("Considering retry attempt; %s\n", strings.Join(details, ", "))
	}

	// Do not retry on a Context-related error (Canceled or DeadlineExceeded).
	if ctx.Err() != nil {
		GetLogger().Debug("No retry, Context error: %s\n", ctx.Err().Error())
		return false, ctx.Err()
	}

	// Next, check for a few non-retryable errors.
	if err != nil {
		if v, ok := err.(*url.Error); ok {
			// Don't retry if the error was due to too many redirects.
			if redirectsErrorRe.MatchString(v.Error()) {
				GetLogger().Debug("No retry, too many redirects: %s\n", v.Error())
				return false, SDKErrorf(v, "", "too-many-redirects", getComponentInfo())
			}

			// Don't retry if the error was due to an invalid protocol scheme.
			if schemeErrorRe.MatchString(v.Error()) {
				GetLogger().Debug("No retry, invalid protocol scheme: %s\n", v.Error())
				return false, SDKErrorf(v, "", "invalid-scheme", getComponentInfo())
			}

			// Don't retry if the error was due to an invalid header.
			if invalidHeaderErrorRe.MatchString(v.Error()) {
				GetLogger().Debug("No retry, invalid header: %s\n", v.Error())
				return false, SDKErrorf(v, "", "invalid-header", getComponentInfo())
			}

			// Don't retry if the error was due to TLS cert verification failure.
			if notTrustedErrorRe.MatchString(v.Error()) {
				GetLogger().Debug("No retry, TLS certificate is not trusted: %s\n", v.Error())
				return false, SDKErrorf(v, "", "cert-not-trusted", getComponentInfo())
			}
			if _, ok := v.Err.(*tls.CertificateVerificationError); ok {
				GetLogger().Debug("No retry, TLS certificate validation error: %s\n", v.Error())
				return false, SDKErrorf(v, "", "cert-failure", getComponentInfo())
			}
		}

		// The error is likely recoverable so retry.
		GetLogger().Debug("Retry will be attempted...")
		return true, nil
	}

	// Now check the status code.

	// A 429 should be retryable.
	// All codes in the 500's range except for 501 (Not Implemented) should be retryable.
	if resp.StatusCode == 429 || (resp.StatusCode >= 500 && resp.StatusCode <= 599 && resp.StatusCode != 501) {
		GetLogger().Debug("Retry will be attempted")
		return true, nil
	}

	GetLogger().Debug("No retry for status code: %d\n", resp.StatusCode)
	return false, nil
}

// IBMCloudSDKBackoffPolicy provides a default implementation of the Backoff interface
// associated with a retryablehttp.Client.
// This function will return the wait time to be associated with the next retry attempt.
func IBMCloudSDKBackoffPolicy(min, max time.Duration, attemptNum int, resp *http.Response) time.Duration {
	// Check for a Retry-After header.
	if resp != nil {
		if s, ok := resp.Header["Retry-After"]; ok {
			GetLogger().Debug("Found Retry-After header: %s\n", s)

			// First, try to parse the value as an integer (number of seconds to wait)
			if sleep, err := strconv.ParseInt(s[0], 10, 64); err == nil {
				return time.Second * time.Duration(sleep)
			}

			// Otherwise, try to parse the value as an HTTP Time value.
			if retryTime, err := http.ParseTime(s[0]); err == nil {
				sleep := time.Until(retryTime)
				if sleep > max {
					sleep = max
				}
				return sleep
			}
		}
	}

	// If no header-based wait time can be determined, then ask DefaultBackoff()
	// to compute an exponential backoff.
	return retryablehttp.DefaultBackoff(min, max, attemptNum, resp)
}
