// Copyright (c) 2016, 2018, 2026, Oracle and/or its affiliates.  All rights reserved.
// This software is dual-licensed to you under the Universal Permissive License (UPL) 1.0 as shown at https://oss.oracle.com/licenses/upl or Apache License 2.0 as shown at http://www.apache.org/licenses/LICENSE-2.0. You may choose either license.

// Package common provides supporting functions and structs used by service packages
package common

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// DefaultHostURLTemplate The default url template for service hosts
	DefaultHostURLTemplate = "%s.%s.oraclecloud.com"

	// requestHeaderAccept The key for passing a header to indicate Accept
	requestHeaderAccept = "Accept"

	// requestHeaderAuthorization The key for passing a header to indicate Authorization
	requestHeaderAuthorization = "Authorization"

	// requestHeaderContentLength The key for passing a header to indicate Content Length
	requestHeaderContentLength = "Content-Length"

	// requestHeaderContentType The key for passing a header to indicate Content Type
	requestHeaderContentType = "Content-Type"

	// requestHeaderExpect The key for passing a header to indicate Expect/100-Continue
	requestHeaderExpect = "Expect"

	// requestHeaderDate The key for passing a header to indicate Date
	requestHeaderDate = "Date"

	// requestHeaderIfMatch The key for passing a header to indicate If Match
	requestHeaderIfMatch = "if-match"

	// requestHeaderOpcClientInfo The key for passing a header to indicate OPC Client Info
	requestHeaderOpcClientInfo = "opc-client-info"

	// requestHeaderOpcRetryToken The key for passing a header to indicate OPC Retry Token
	requestHeaderOpcRetryToken = "opc-retry-token"

	// requestHeaderOpcRequestID The key for unique Oracle-assigned identifier for the request.
	requestHeaderOpcRequestID = "opc-request-id"

	// requestHeaderOpcClientRequestID The key for unique Oracle-assigned identifier for the request.
	requestHeaderOpcClientRequestID = "opc-client-request-id"

	// requestHeaderUserAgent The key for passing a header to indicate User Agent
	requestHeaderUserAgent = "User-Agent"

	// requestHeaderXContentSHA256 The key for passing a header to indicate SHA256 hash
	requestHeaderXContentSHA256 = "X-Content-SHA256"

	// requestHeaderOpcOboToken The key for passing a header to use obo token
	requestHeaderOpcOboToken = "opc-obo-token"

	// private constants
	defaultScheme            = "https"
	defaultSDKMarker         = "Oracle-GoSDK"
	defaultUserAgentTemplate = "%s/%s (%s/%s; go/%s)" //SDK/SDKVersion (OS/OSVersion; Lang/LangVersion)
	// http.Client.Timeout includes Dial, TLSHandshake, Request, Response header and body
	defaultTimeout           = 60 * time.Second
	defaultConfigFileName    = "config"
	defaultConfigDirName     = ".oci"
	configFilePathEnvVarName = "OCI_CONFIG_FILE"

	secondaryConfigDirName = ".oraclebmc"
	maxBodyLenForDebug     = 1024 * 1000

	// appendUserAgentEnv The key for retrieving append user agent value from env var
	appendUserAgentEnv = "OCI_SDK_APPEND_USER_AGENT"

	// requestHeaderOpcClientRetries The key for passing a header to set client retries info
	requestHeaderOpcClientRetries = "opc-client-retries"

	// isDefaultRetryEnabled The key for set default retry disabled from env var
	isDefaultRetryEnabled = "OCI_SDK_DEFAULT_RETRY_ENABLED"

	// isDefaultCircuitBreakerEnabled is the key for set default circuit breaker disabled from env var
	isDefaultCircuitBreakerEnabled = "OCI_SDK_DEFAULT_CIRCUITBREAKER_ENABLED"

	//circuitBreakerNumberOfHistoryResponseEnv is the number of recorded history responses
	circuitBreakerNumberOfHistoryResponseEnv = "OCI_SDK_CIRCUITBREAKER_NUM_HISTORY_RESPONSE"

	// ociDefaultRefreshIntervalForCustomCerts is the env var for overriding the defaultRefreshIntervalForCustomCerts.
	// The value represents the refresh interval in minutes and has a higher precedence than defaultRefreshIntervalForCustomCerts
	// but has a lower precedence then the refresh interval configured via OciGlobalRefreshIntervalForCustomCerts
	// If the value is negative, then it is assumed that this property is not configured
	// if the value is Zero, then the refresh of custom certs will be disabled
	ociDefaultRefreshIntervalForCustomCerts = "OCI_DEFAULT_REFRESH_INTERVAL_FOR_CUSTOM_CERTS"

	// ociDefaultCertsPath is the env var for the path to the SSL cert file
	ociDefaultCertsPath = "OCI_DEFAULT_CERTS_PATH"

	// ociDefaultClientCertsPath is the env var for the path to the custom client cert
	ociDefaultClientCertsPath = "OCI_DEFAULT_CLIENT_CERTS_PATH"

	// ociDefaultClientCertsPrivateKeyPath is the env var for the path to the custom client cert private key
	ociDefaultClientCertsPrivateKeyPath = "OCI_DEFAULT_CLIENT_CERTS_PRIVATE_KEY_PATH"

	//maxAttemptsForRefreshableRetry is the number of retry when 401 happened on a refreshable auth type
	maxAttemptsForRefreshableRetry = 3

	//defaultRefreshIntervalForCustomCerts is the default refresh interval in minutes
	defaultRefreshIntervalForCustomCerts = 30

	// CustomClientTimeoutEnvVar allows the user to set the timeout in seconds to be used by each service client.
	CustomClientTimeoutEnvVar = "OCI_CUSTOM_CLIENT_TIMEOUT"

	// Environment variable to check whether dual stack endpoints should be enabled
	ociDualStackEndpointEnabledEnvVar = "OCI_DUAL_STACK_ENDPOINT_ENABLED"

	// String representing a single "phrase" of an endpoint template option
	endpointTemplateOptionPhrase = "((\\w|\\.|\\-)+)"

	// Checks for template for endpoint options
	patternForEndpointTemplateOptions = "\\{" + endpointTemplateOptionPhrase + "\\?((" + endpointTemplateOptionPhrase + ":" + endpointTemplateOptionPhrase + ")" +
		"|(" + endpointTemplateOptionPhrase + ":\\s*)|(\\s*:" + endpointTemplateOptionPhrase + "))}"

	dualStackOption = "{dualStack"
)

// OciGlobalRefreshIntervalForCustomCerts is the global policy for overriding the refresh interval in minutes.
// This variable has a higher precedence than the env variable OCI_DEFAULT_REFRESH_INTERVAL_FOR_CUSTOM_CERTS
// and the defaultRefreshIntervalForCustomCerts values.
// If the value is negative, then it is assumed that this property is not configured
// if the value is Zero, then the refresh of custom certs will be disabled
var OciGlobalRefreshIntervalForCustomCerts int = -1

// RequestInterceptor function used to customize the request before calling the underlying service
type RequestInterceptor func(*http.Request) error

// HTTPRequestDispatcher wraps the execution of a http request, it is generally implemented by
// http.Client.Do, but can be customized for testing
type HTTPRequestDispatcher interface {
	Do(req *http.Request) (*http.Response, error)
}

// CustomClientConfiguration contains configurations set at client level
type CustomClientConfiguration struct {

	// Retry policy used on calls made by the client
	RetryPolicy *RetryPolicy

	// The Circuit Breaker used to regulate calls made by the client
	CircuitBreaker *OciCircuitBreaker

	// Allows user to decide if they want to use realm specific endpoints
	RealmSpecificServiceEndpointTemplateEnabled *bool

	// Allows user to decide if they want to use dual stack endpoints
	EnableDualStackEndpoints *bool

	// Set on creation of the client, based on the below flag from the service spec
	// x-obmcs-endpoint-template-options: dualStack: true/false
	ServiceUsesDualStackByDefault *bool
}

// BaseClient struct implements all basic operations to call oci web services.
type BaseClient struct {
	//HTTPClient performs the http network operations
	HTTPClient HTTPRequestDispatcher

	//Signer performs auth operation
	Signer HTTPRequestSigner

	//A request interceptor can be used to customize the request before signing and dispatching
	Interceptor RequestInterceptor

	//The host of the service
	Host string

	//The user agent
	UserAgent string

	//Base path for all operations of this client
	BasePath string

	Configuration CustomClientConfiguration

	//Whether the OCI_INCLUDE_REQUEST_TELEMETRY_DATA environment variable was true at the time of client creation,
	//indicating that x-oci-service-name and x-oci-operation-id headers should be sent.
	ociIncludeRequestTelemetryDataEnabled bool
}

// SetCustomClientConfiguration sets client with retry and other custom configurations
func (client *BaseClient) SetCustomClientConfiguration(config CustomClientConfiguration) {
	client.Configuration = config
}

// RetryPolicy returns the retryPolicy configured for client
func (client *BaseClient) RetryPolicy() *RetryPolicy {
	return client.Configuration.RetryPolicy
}

// Endpoint returns the endpoint configured for client
func (client *BaseClient) Endpoint() string {
	host := client.Host
	if !strings.Contains(host, "http") &&
		!strings.Contains(host, "https") {
		host = fmt.Sprintf("%s://%s", defaultScheme, host)
	}
	return host
}

func UpdateEndpointTemplateForOptions(client *BaseClient) {
	templateRegex := regexp.MustCompile(patternForEndpointTemplateOptions)
	templates := templateRegex.FindAllString(client.Host, -1)
	for _, option := range templates {
		optionParam := ""
		optionEnabledParam := option[strings.Index(option, "?")+1 : strings.Index(option, ":")]
		optionDisabledParam := option[strings.Index(option, ":")+1 : strings.Index(option, "}")]

		// Option case: Dual Stack Endpoints
		if strings.Contains(option, dualStackOption) {
			dualStackEnvVarValue := os.Getenv(ociDualStackEndpointEnabledEnvVar)
			if client.IsServiceDualStackEnabledByDefault() {
				if !client.IsDualStackEndpointEnabled() || (dualStackEnvVarValue != "" && strings.ToLower(dualStackEnvVarValue) == "false") {
					optionParam = optionDisabledParam
				} else {
					optionParam = optionEnabledParam
				}
			} else {
				if client.IsDualStackEndpointEnabled() || (dualStackEnvVarValue != "" && strings.ToLower(dualStackEnvVarValue) == "true") {
					optionParam = optionEnabledParam
				} else {
					optionParam = optionDisabledParam
				}
			}
		}
		client.Host = strings.Replace(client.Host, option, optionParam, -1)
	}
}

// UseDualStackEndpointsByDefault sets whether dual stack endpoints are used by default
func (client *BaseClient) UseDualStackEndpointsByDefault(useByDefault bool) {
	client.Configuration.EnableDualStackEndpoints = &useByDefault
	client.Configuration.ServiceUsesDualStackByDefault = &useByDefault
}

// EnableDualStackEndpoints sets whether dual stack endpoints should be used for this client
func (client *BaseClient) EnableDualStackEndpoints(EnableDualStack bool) {
	client.Configuration.EnableDualStackEndpoints = &EnableDualStack
}

// IsDualStackEndpointEnabled is used to check if Dual Stack Endpoints are Enabled
func (client *BaseClient) IsDualStackEndpointEnabled() bool {
	return client.Configuration.EnableDualStackEndpoints != nil && *client.Configuration.EnableDualStackEndpoints
}

// IsServiceDualStackEnabledByDefault is used to check if Dual Stack Endpoints enabled by default for the service of the client
func (client *BaseClient) IsServiceDualStackEnabledByDefault() bool {
	return client.Configuration.ServiceUsesDualStackByDefault != nil && *client.Configuration.ServiceUsesDualStackByDefault
}

func defaultUserAgent() string {
	userAgent := fmt.Sprintf(defaultUserAgentTemplate, defaultSDKMarker, Version(), runtime.GOOS, runtime.GOARCH, runtime.Version())
	appendUA := os.Getenv(appendUserAgentEnv)
	if appendUA != "" {
		userAgent = fmt.Sprintf("%s %s", userAgent, appendUA)
	}
	return userAgent
}

var clientCounter int64

func getNextSeed() int64 {
	newCounterValue := atomic.AddInt64(&clientCounter, 1)
	return newCounterValue + time.Now().UnixNano()
}

func newBaseClient(signer HTTPRequestSigner, dispatcher HTTPRequestDispatcher) BaseClient {
	rand.Seed(getNextSeed())

	includeTelemetry := strings.EqualFold(os.Getenv("OCI_INCLUDE_REQUEST_TELEMETRY_DATA"), "true")

	baseClient := BaseClient{
		UserAgent:                             defaultUserAgent(),
		Interceptor:                           nil,
		Signer:                                signer,
		HTTPClient:                            dispatcher,
		ociIncludeRequestTelemetryDataEnabled: includeTelemetry,
	}

	// check the default retry environment variable setting
	if IsEnvVarTrue(isDefaultRetryEnabled) {
		defaultRetry := DefaultRetryPolicy()
		baseClient.Configuration.RetryPolicy = &defaultRetry
	} else if IsEnvVarFalse(isDefaultRetryEnabled) {
		policy := NoRetryPolicy()
		baseClient.Configuration.RetryPolicy = &policy
	}
	// check if user defined global retry is configured
	if GlobalRetry != nil {
		baseClient.Configuration.RetryPolicy = GlobalRetry
	}

	baseClient.UseDualStackEndpointsByDefault(false)

	return baseClient
}

func defaultHTTPDispatcher() http.Client {
	var httpClient http.Client
	refreshInterval := getCustomCertRefreshInterval()
	if refreshInterval <= 0 {
		Debug("Custom cert refresh has been disabled")
	}
	var tp = &OciHTTPTransportWrapper{
		RefreshRate:       time.Duration(refreshInterval) * time.Minute,
		TLSConfigProvider: GetTLSConfigTemplateForTransport(),
	}

	// Set client timeout to default or value set in environment variable
	clientTimeout := defaultTimeout
	if customTimeout := os.Getenv(CustomClientTimeoutEnvVar); customTimeout != "" {
		if timeInSeconds, err := strconv.Atoi(customTimeout); err != nil || timeInSeconds < 0 {
			Logf("WARNING: %s set but could not be converted to a postive integer", CustomClientTimeoutEnvVar)
		} else {
			Debugf("Using custom client timeout of %s seconds", customTimeout)
			clientTimeout = time.Duration(timeInSeconds) * time.Second
		}
	}

	// Create the underlying HTTP client
	httpClient = http.Client{
		Timeout:   clientTimeout,
		Transport: tp,
	}
	return httpClient
}

func defaultBaseClient(provider KeyProvider) BaseClient {
	dispatcher := defaultHTTPDispatcher()
	signer := DefaultRequestSigner(provider)
	return newBaseClient(signer, &dispatcher)
}

// DefaultBaseClientWithSigner creates a default base client with a given signer
func DefaultBaseClientWithSigner(signer HTTPRequestSigner) BaseClient {
	dispatcher := defaultHTTPDispatcher()
	return newBaseClient(signer, &dispatcher)
}

// NewClientWithConfig Create a new client with a configuration provider, the configuration provider
// will be used for the default signer as well as reading the region
// This function does not check for valid regions to implement forward compatibility
func NewClientWithConfig(configProvider ConfigurationProvider) (client BaseClient, err error) {
	var ok bool
	if ok, err = IsConfigurationProviderValid(configProvider); !ok {
		err = fmt.Errorf("can not create client, bad configuration: %s", err.Error())
		return
	}

	client = defaultBaseClient(configProvider)

	if authConfig, e := configProvider.AuthType(); e == nil && authConfig.OboToken != nil {
		Debugf("authConfig's authType is %s", authConfig.AuthType)
		signOboToken(&client, *authConfig.OboToken, configProvider)
	}

	return
}

// NewClientWithOboToken Create a new client that will use oboToken for auth
func NewClientWithOboToken(configProvider ConfigurationProvider, oboToken string) (client BaseClient, err error) {
	client, err = NewClientWithConfig(configProvider)
	if err != nil {
		return
	}

	signOboToken(&client, oboToken, configProvider)

	return
}

// Add obo token header to Interceptor and sign to client
func signOboToken(client *BaseClient, oboToken string, configProvider ConfigurationProvider) {
	// Interceptor to add obo token header
	client.Interceptor = func(request *http.Request) error {
		request.Header.Add(requestHeaderOpcOboToken, oboToken)
		return nil
	}
	// Obo token will also be signed
	defaultHeaders := append(DefaultGenericHeaders(), requestHeaderOpcOboToken)
	client.Signer = RequestSigner(configProvider, defaultHeaders, DefaultBodyHeaders())
}

func getHomeFolder() string {
	current, e := user.Current()
	if e != nil {
		//Give up and try to return something sensible
		home := os.Getenv("HOME")
		if home == "" {
			home = os.Getenv("USERPROFILE")
		}
		return home
	}
	return current.HomeDir
}

// DefaultConfigProvider returns the default config provider. The default config provider
// will look for configurations in 3 places: file in $HOME/.oci/config, HOME/.obmcs/config and
// variables names starting with the string TF_VAR. If the same configuration is found in multiple
// places the provider will prefer the first one.
// If the config file is not placed in the default location, the environment variable
// OCI_CONFIG_FILE can provide the config file location.
func DefaultConfigProvider() ConfigurationProvider {
	defaultConfigFile := getDefaultConfigFilePath()
	homeFolder := getHomeFolder()
	secondaryConfigFile := filepath.Join(homeFolder, secondaryConfigDirName, defaultConfigFileName)

	defaultFileProvider, _ := ConfigurationProviderFromFile(defaultConfigFile, "")
	secondaryFileProvider, _ := ConfigurationProviderFromFile(secondaryConfigFile, "")
	environmentProvider := environmentConfigurationProvider{EnvironmentVariablePrefix: "TF_VAR"}

	provider, _ := ComposingConfigurationProvider([]ConfigurationProvider{defaultFileProvider, secondaryFileProvider, environmentProvider})
	Debugf("Configuration provided by: %s", provider)
	return provider
}

// CustomProfileSessionTokenConfigProvider returns the session token config provider of the given profile.
// This will look for the configuration in the given config file path.
func CustomProfileSessionTokenConfigProvider(customConfigPath string, profile string) ConfigurationProvider {
	if customConfigPath == "" {
		customConfigPath = getDefaultConfigFilePath()
	}

	sessionTokenConfigurationProvider, _ := ConfigurationProviderForSessionTokenWithProfile(customConfigPath, profile, "")
	Debugf("Configuration provided by: %s", sessionTokenConfigurationProvider)
	return sessionTokenConfigurationProvider
}

func getDefaultConfigFilePath() string {
	homeFolder := getHomeFolder()
	defaultConfigFile := filepath.Join(homeFolder, defaultConfigDirName, defaultConfigFileName)
	if _, err := os.Stat(defaultConfigFile); err == nil {
		return defaultConfigFile
	}
	Debugf("The %s does not exist, will check env var %s for file path.", defaultConfigFile, configFilePathEnvVarName)
	// Read configuration file path from OCI_CONFIG_FILE env var
	fallbackConfigFile, existed := os.LookupEnv(configFilePathEnvVarName)
	if !existed {
		Debugf("The env var %s does not exist...", configFilePathEnvVarName)
		return defaultConfigFile
	}
	if _, err := os.Stat(fallbackConfigFile); os.IsNotExist(err) {
		Debugf("The specified cfg file path in the env var %s does not exist: %s", configFilePathEnvVarName, fallbackConfigFile)
		return defaultConfigFile
	}
	return fallbackConfigFile
}

// setRawPath sets the Path and RawPath fields of the URL based on the provided
// escaped path p. It maintains the invariant that RawPath is only specified
// when it differs from the default encoding of the path.
// For example:
// - setPath("/foo/bar")   will set Path="/foo/bar" and RawPath=""
// - setPath("/foo%2fbar") will set Path="/foo/bar" and RawPath="/foo%2fbar"
func setRawPath(u *url.URL) error {
	oldPath := u.Path
	path, err := url.PathUnescape(u.Path)
	if err != nil {
		return err
	}
	u.Path = path
	if escp := u.EscapedPath(); oldPath == escp {
		// Default encoding is fine.
		u.RawPath = ""
	} else {
		u.RawPath = oldPath
	}
	return nil
}

// CustomProfileConfigProvider returns the config provider of given profile. The custom profile config provider
// will look for configurations in 2 places: file in $HOME/.oci/config,  and variables names starting with the
// string TF_VAR. If the same configuration is found in multiple places the provider will prefer the first one.
func CustomProfileConfigProvider(customConfigPath string, profile string) ConfigurationProvider {
	homeFolder := getHomeFolder()
	if customConfigPath == "" {
		customConfigPath = filepath.Join(homeFolder, defaultConfigDirName, defaultConfigFileName)
	}
	customFileProvider, _ := ConfigurationProviderFromFileWithProfile(customConfigPath, profile, "")
	defaultFileProvider, _ := ConfigurationProviderFromFileWithProfile(customConfigPath, "DEFAULT", "")
	environmentProvider := environmentConfigurationProvider{EnvironmentVariablePrefix: "TF_VAR"}
	provider, _ := ComposingConfigurationProvider([]ConfigurationProvider{customFileProvider, defaultFileProvider, environmentProvider})
	Debugf("Configuration provided by: %s", provider)
	return provider
}

func (client *BaseClient) prepareRequest(request *http.Request) (err error) {
	if client.UserAgent == "" {
		return fmt.Errorf("user agent can not be blank")
	}

	if request.Header == nil {
		request.Header = http.Header{}
	}
	request.Header.Set(requestHeaderUserAgent, client.UserAgent)
	request.Header.Set(requestHeaderDate, time.Now().UTC().Format(http.TimeFormat))

	if !strings.Contains(client.Host, "http") &&
		!strings.Contains(client.Host, "https") {
		client.Host = fmt.Sprintf("%s://%s", defaultScheme, client.Host)
	}

	clientURL, err := url.Parse(client.Host)
	if err != nil {
		return fmt.Errorf("host is invalid. %s", err.Error())
	}
	if clientURL.Scheme != "http" && clientURL.Scheme != "https" {
		return fmt.Errorf("host is invalid. endpoint scheme must be http or https")
	}
	if clientURL.User != nil || clientURL.Path != "" || clientURL.RawQuery != "" || clientURL.Fragment != "" {
		return fmt.Errorf("host is invalid. endpoint must not contain user info, path, query, or fragment")
	}
	request.URL.Host = clientURL.Host
	request.URL.Scheme = clientURL.Scheme
	currentPath := request.URL.Path
	if !strings.HasPrefix(currentPath, fmt.Sprintf("/%s", client.BasePath)) {
		request.URL.Path = path.Clean(fmt.Sprintf("/%s/%s", client.BasePath, currentPath))
		err := setRawPath(request.URL)
		if err != nil {
			return err
		}
	}
	return
}

func (client BaseClient) intercept(request *http.Request) (err error) {
	if client.Interceptor != nil {
		err = client.Interceptor(request)
	}
	return
}

// checkForSuccessfulResponse checks if the response is successful
// If Error Code is 4XX/5XX and debug level is set to info, will log the request and response
func checkForSuccessfulResponse(res *http.Response, requestBody *io.ReadCloser) error {
	familyStatusCode := res.StatusCode / 100
	if familyStatusCode == 4 || familyStatusCode == 5 {
		IfInfo(func() {
			// If debug level is set to verbose, the request and request body will be dumped and logged under debug level, this is to avoid duplicate logging
			if defaultLogger.LogLevel() < verboseLogging {
				logRequest(res.Request, Logf, noLogging)
				if requestBody != nil && *requestBody != http.NoBody {
					bodyContent, _ := ioutil.ReadAll(*requestBody)
					Logf("Dump Request Body: \n%s", RedactSensitiveStringForLogs(string(bodyContent)))
				}
			}
			logResponse(res, Logf, infoLogging)
		})
		return newServiceFailureFromResponse(res)
	}
	IfDebug(func() {
		logResponse(res, Debugf, verboseLogging)
	})
	return nil
}

func logRequest(request *http.Request, fn func(format string, v ...interface{}), bodyLoggingLevel int) {
	if request == nil {
		return
	}
	dumpBody := true
	if checkBodyLengthExceedLimit(request.ContentLength) {
		fn("not dumping body too big\n")
		dumpBody = false
	}

	dumpBody = dumpBody && defaultLogger.LogLevel() >= bodyLoggingLevel && bodyLoggingLevel != noLogging
	if dump, e := httputil.DumpRequestOut(request, dumpBody); e == nil {
		fn("Dump Request %s", RedactSensitiveStringForLogs(string(dump)))
	} else {
		fn("%v\n", e)
	}
}

func logResponse(response *http.Response, fn func(format string, v ...interface{}), bodyLoggingLevel int) {
	if response == nil {
		return
	}
	dumpBody := true
	if checkBodyLengthExceedLimit(response.ContentLength) {
		fn("not dumping body too big\n")
		dumpBody = false
	}
	dumpBody = dumpBody && defaultLogger.LogLevel() >= bodyLoggingLevel && bodyLoggingLevel != noLogging
	if dump, e := httputil.DumpResponse(response, dumpBody); e == nil {
		fn("Dump Response %s", RedactSensitiveStringForLogs(string(dump)))
	} else {
		fn("%v\n", e)
	}
}

func checkBodyLengthExceedLimit(contentLength int64) bool {
	return contentLength > maxBodyLenForDebug
}

// OCIRequest is any request made to an OCI service.
type OCIRequest interface {
	// HTTPRequest assembles an HTTP request.
	HTTPRequest(method, path string, binaryRequestBody *OCIReadSeekCloser, extraHeaders map[string]string) (http.Request, error)
}

// RequestMetadata is metadata about an OCIRequest. This structure represents the behavior exhibited by the SDK when
// issuing (or reissuing) a request.
type RequestMetadata struct {
	// RetryPolicy is the policy for reissuing the request. If no retry policy is set on the request,
	// then the request will be issued exactly once.
	RetryPolicy *RetryPolicy
}

// OCIReadSeekCloser is a thread-safe io.ReadSeekCloser to prevent racing with retrying binary requests
type OCIReadSeekCloser struct {
	rc       io.ReadCloser
	lock     sync.Mutex
	isClosed bool
}

// NewOCIReadSeekCloser constructs OCIReadSeekCloser, the only input is binary request body
func NewOCIReadSeekCloser(rc io.ReadCloser) *OCIReadSeekCloser {
	rsc := OCIReadSeekCloser{}
	rsc.rc = rc
	return &rsc
}

// Seek is a thread-safe operation, it implements io.seek() interface, if the original request body implements io.seek()
// interface, or implements "well-known" data type like os.File, io.SectionReader, or wrapped by ioutil.NopCloser can be supported
func (rsc *OCIReadSeekCloser) Seek(offset int64, whence int) (int64, error) {
	rsc.lock.Lock()
	defer rsc.lock.Unlock()

	if _, ok := rsc.rc.(io.Seeker); ok {
		return rsc.rc.(io.Seeker).Seek(offset, whence)
	}
	// once the binary request body is wrapped with ioutil.NopCloser:
	if isNopCloser(rsc.rc) {
		unwrappedInterface := reflect.ValueOf(rsc.rc).Field(0).Interface()
		if _, ok := unwrappedInterface.(io.Seeker); ok {
			return unwrappedInterface.(io.Seeker).Seek(offset, whence)
		}
	}
	return 0, fmt.Errorf("current binary request body type is not seekable, if want to use retry feature, please make sure the request body implements seek() method")
}

// Close is a thread-safe operation, it closes the instance of the OCIReadSeekCloser's access to the underlying io.ReadCloser.
func (rsc *OCIReadSeekCloser) Close() error {
	rsc.lock.Lock()
	defer rsc.lock.Unlock()
	rsc.isClosed = true
	return nil
}

// Read is a thread-safe operation, it implements io.Read() interface
func (rsc *OCIReadSeekCloser) Read(p []byte) (n int, err error) {
	rsc.lock.Lock()
	defer rsc.lock.Unlock()

	if rsc.isClosed {
		return 0, io.EOF
	}

	return rsc.rc.Read(p)
}

// Seekable is used for check if the binary request body can be seek or no
func (rsc *OCIReadSeekCloser) Seekable() bool {
	if rsc == nil {
		return false
	}
	if _, ok := rsc.rc.(io.Seeker); ok {
		return true
	}
	// once the binary request body is wrapped with ioutil.NopCloser:
	if isNopCloser(rsc.rc) {
		if _, ok := reflect.ValueOf(rsc.rc).Field(0).Interface().(io.Seeker); ok {
			return true
		}
	}
	return false
}

// OCIResponse is the response from issuing a request to an OCI service.
type OCIResponse interface {
	// HTTPResponse returns the raw HTTP response.
	HTTPResponse() *http.Response
}

// OCIOperation is the generalization of a request-response cycle undergone by an OCI service.
type OCIOperation func(context.Context, OCIRequest, *OCIReadSeekCloser, map[string]string) (OCIResponse, error)

// ClientCallDetails a set of settings used by the a single Call operation of the http Client
type ClientCallDetails struct {
	Signer        HTTPRequestSigner
	ServiceName   string
	OperationName string
}

// Call executes the http request with the given context
func (client BaseClient) Call(ctx context.Context, request *http.Request) (response *http.Response, err error) {
	details := ClientCallDetails{Signer: client.Signer}
	if client.IsRefreshableAuthType() {
		return client.RefreshableTokenWrappedCallWithDetails(ctx, request, details)
	}
	return client.CallWithDetails(ctx, request, details)
}

// CallWithServiceAndOperationName executes the http request with the given context and known service and operation name
func (client BaseClient) CallWithServiceAndOperationName(ctx context.Context, request *http.Request, serviceName string, operationName string) (response *http.Response, err error) {
	details := ClientCallDetails{Signer: client.Signer, ServiceName: serviceName, OperationName: operationName}
	if client.IsRefreshableAuthType() {
		return client.RefreshableTokenWrappedCallWithDetails(ctx, request, details)
	}
	return client.CallWithDetails(ctx, request, details)
}

// RefreshableTokenWrappedCallWithDetails wraps the CallWithDetails with retry on 401 for Refreshable Token (Instance Principal, Resource Principal, etc.)
// This retry reduces transient 401s that can occur due to concurrent token refresh
func (client BaseClient) RefreshableTokenWrappedCallWithDetails(ctx context.Context, request *http.Request, details ClientCallDetails) (response *http.Response, err error) {
	var (
		rsc         *OCIReadSeekCloser
		isSeekable  bool
		curPos      int64
		initialSize int64
	)

	// Prepare request body for potential retries
	if request != nil && request.Body != nil && request.Body != http.NoBody {
		rsc = NewOCIReadSeekCloser(request.Body)
		request.Body = rsc

		if rsc.Seekable() {
			isSeekable = true

			// Capture current position and total size so we can restore Content-Length on retries
			curPos, _ = rsc.Seek(0, io.SeekCurrent)
			if end, seekErr := rsc.Seek(0, io.SeekEnd); seekErr == nil {
				initialSize = end
				_, _ = rsc.Seek(curPos, io.SeekStart)
			}
		}
	}

	for attempt := 0; attempt < maxAttemptsForRefreshableRetry; attempt++ {
		// On retries, rewind request body and restore content length/header if seekable
		if attempt > 0 && request != nil && request.Body != nil && request.Body != http.NoBody {
			if !isSeekable {
				return response, NonSeekableRequestRetryFailure{err}
			}

			rsc = NewOCIReadSeekCloser(rsc.rc)
			_, _ = rsc.Seek(curPos, io.SeekStart)
			request.Body = rsc

			if initialSize > 0 {
				request.ContentLength = initialSize - curPos
				if request.Header == nil {
					request.Header = make(http.Header)
				}
				request.Header.Set(requestHeaderContentLength, strconv.FormatInt(request.ContentLength, 10))
			}
		}

		response, err = client.CallWithDetails(ctx, request, ClientCallDetails{Signer: client.Signer})
		// Retry only on a HTTP 401 response
		if response == nil {
			return nil, err
		}
		if response.StatusCode != http.StatusUnauthorized {
			return response, err
		}
		time.Sleep(1 * time.Second)
	}
	return
}

// CallWithDetails executes the http request, the given context using details specified in the parameters, this function
// provides a way to override some settings present in the client
func (client BaseClient) CallWithDetails(ctx context.Context, request *http.Request, details ClientCallDetails) (response *http.Response, err error) {
	Debugln("Attempting to call downstream service")
	request = request.WithContext(ctx)

	if client.ociIncludeRequestTelemetryDataEnabled {
		if details.ServiceName != "" {
			request.Header.Set("x-oci-service-name", details.ServiceName)
		}
		if details.ServiceName != "" {
			request.Header.Set("x-oci-operation-id", details.OperationName)
		}
	}

	err = client.prepareRequest(request)
	if err != nil {
		return
	}
	//Intercept
	err = client.intercept(request)
	if err != nil {
		return
	}
	//Sign the request
	err = details.Signer.Sign(request)
	if err != nil {
		return
	}

	//Execute the http request
	if ociGoBreaker := client.Configuration.CircuitBreaker; ociGoBreaker != nil {
		resp, cbErr := ociGoBreaker.Cb.Execute(func() (interface{}, error) {
			return client.httpDo(request)
		})
		if httpResp, ok := resp.(*http.Response); ok {
			if httpResp != nil && httpResp.StatusCode != 200 {
				if failure, ok := IsServiceError(cbErr); ok {
					ociGoBreaker.AddToHistory(resp.(*http.Response), failure)
				}
			}
		}
		if cbErr != nil && IsCircuitBreakerError(cbErr) {
			cbErr = getCircuitBreakerError(request, cbErr, ociGoBreaker)
		}
		if _, ok := resp.(*http.Response); !ok {
			return nil, cbErr
		}
		return resp.(*http.Response), cbErr
	}
	return client.httpDo(request)
}

// IsRefreshableAuthType validates if a signer is from a refreshable config provider
func (client BaseClient) IsRefreshableAuthType() bool {
	if signer, ok := client.Signer.(ociRequestSigner); ok {
		if provider, ok := signer.KeyProvider.(RefreshableConfigurationProvider); ok {
			return provider.Refreshable()
		}
	}
	return false
}

func (client BaseClient) httpDo(request *http.Request) (response *http.Response, err error) {

	//Copy request body and save for logging
	dumpRequestBody := ioutil.NopCloser(bytes.NewBuffer(nil))
	if request.Body != nil && !checkBodyLengthExceedLimit(request.ContentLength) {
		if dumpRequestBody, request.Body, err = drainBody(request.Body); err != nil {
			dumpRequestBody = ioutil.NopCloser(bytes.NewBuffer(nil))
		}
	}
	IfDebug(func() {
		logRequest(request, Debugf, verboseLogging)
	})

	//Execute the http request
	response, err = client.HTTPClient.Do(request)

	if err != nil {
		IfInfo(func() {
			Logf("%v\n", err)
		})
		return response, err
	}

	err = checkForSuccessfulResponse(response, &dumpRequestBody)
	return response, err
}

// CloseBodyIfValid closes the body of an http response if the response and the body are valid
func CloseBodyIfValid(httpResponse *http.Response) {
	if httpResponse != nil && httpResponse.Body != nil {
		if httpResponse.Header != nil && strings.ToLower(httpResponse.Header.Get("content-type")) == "text/event-stream" {
			return
		}
		httpResponse.Body.Close()
	}
}

// IsOciRealmSpecificServiceEndpointTemplateEnabled returns true if the client is configured to use realm specific service endpoint template
// it will first check the client configuration, if not set, it will check the environment variable
func (client BaseClient) IsOciRealmSpecificServiceEndpointTemplateEnabled() bool {
	if client.Configuration.RealmSpecificServiceEndpointTemplateEnabled != nil {
		return *client.Configuration.RealmSpecificServiceEndpointTemplateEnabled
	}
	return IsEnvVarTrue(OciRealmSpecificServiceEndpointTemplateEnabledEnvVar)
}

func getCustomCertRefreshInterval() int {
	if OciGlobalRefreshIntervalForCustomCerts >= 0 {
		Debugf("Setting refresh interval as %d for custom certs via OciGlobalRefreshIntervalForCustomCerts", OciGlobalRefreshIntervalForCustomCerts)
		return OciGlobalRefreshIntervalForCustomCerts
	}
	if refreshIntervalValue, ok := os.LookupEnv(ociDefaultRefreshIntervalForCustomCerts); ok {
		refreshInterval, err := strconv.Atoi(refreshIntervalValue)
		if err != nil || refreshInterval < 0 {
			Debugf("The environment variable %s is not a valid int or is a negative value, skipping this configuration", ociDefaultRefreshIntervalForCustomCerts)
		} else {
			Debugf("Setting refresh interval as %d for custom certs via the env variable %s", refreshInterval, ociDefaultRefreshIntervalForCustomCerts)
			return refreshInterval
		}
	}
	Debugf("Setting the default refresh interval %d for custom certs", defaultRefreshIntervalForCustomCerts)
	return defaultRefreshIntervalForCustomCerts
}
