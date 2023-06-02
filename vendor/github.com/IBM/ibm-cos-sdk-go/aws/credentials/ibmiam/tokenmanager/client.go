package tokenmanager

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/IBM/ibm-cos-sdk-go/aws"
)

var (
	// NewIBMClient client constructor
	NewIBMClient = newIBMClient

	// DefaultIBMClient client constructor with default values
	DefaultIBMClient = defaultIBMClient
)

// IBMClientDo wrapper type to the Do operation
type IBMClientDo interface {

	// HTTP Client Do op
	Do(req *http.Request) (*http.Response, error)
}

// IBM Client Implementation typer wrapper
type defaultIBMCImplementation struct {

	// Internal client
	Client *http.Client

	// Sets maximum number of retries
	MaxRetries int

	// Times the initial of back off
	InitialBackOff time.Duration

	// Times the duration of back off progress
	BackOffProgression func(duration time.Duration) time.Duration

	// Logs the client implementation
	logger aws.Logger

	// Log level for the client implementation
	logLevel *aws.LogLevelType
}

// newIBMClient constructor
// Parameters:
// 		AWS Config
//		Initial Backoff of the refresh
//		Duration of backoff progression
// Returns:
//		Default IBM Client Implementation
func newIBMClient(config *aws.Config, initialBackOff time.Duration,
	backOffProgression func(time.Duration) time.Duration) *defaultIBMCImplementation {
	var httpClient *http.Client
	if config != nil && config.HTTPClient != nil {
		httpClient = config.HTTPClient
	} else {
		httpClient = http.DefaultClient
	}

	// Initialize number of maximum retries
	maxRetries := 0
	if config != nil && config.MaxRetries != nil && *config.MaxRetries > maxRetries {
		maxRetries = *config.MaxRetries
	}

	// Sets the loglevel
	logLevel := aws.LogLevel(aws.LogOff)
	if config != nil && config.LogLevel != nil && config.Logger != nil {
		logLevel = config.LogLevel
	}

	// If initial backoff is less than zero - sets it to zero
	if initialBackOff < time.Duration(0) {
		initialBackOff = time.Duration(0)
	}

	// If back off progressoin is nil, set it to time duration of zero
	if backOffProgression == nil {
		backOffProgression = func(_ time.Duration) time.Duration { return time.Duration(0) }
	}

	return &defaultIBMCImplementation{
		Client:             httpClient,
		MaxRetries:         maxRetries,
		InitialBackOff:     initialBackOff,
		BackOffProgression: backOffProgression,
		logger:             config.Logger,
		logLevel:           logLevel,
	}
}

// Default IBM Client
// Parameter:
//		AWS Config
// Returns:
//		A HTTP Client with IBM IAM Credentials in the config
func defaultIBMClient(config *aws.Config) *defaultIBMCImplementation {
	f := func(duration time.Duration) time.Duration {
		return time.Duration(float64(duration.Nanoseconds())*1.75) * time.Nanosecond
	}
	return newIBMClient(config, 500*time.Millisecond, f)
}

// Internal IBM Client HTTP Client request execution
// Parameter:
//		An HTTP Request Object
// Returns:
//		An HTTP Response Object
//		Error
func (c *defaultIBMCImplementation) Do(req *http.Request) (r *http.Response, e error) {

	// Enablese Log if Debugger is turned on
	if c.logLevel.Matches(aws.LogDebug) {
		c.logger.Log(debugLog, defaultIBMCImpLog, req.Method, req.URL)
	}
	r, e = c.Client.Do(req)
	if e == nil && isSuccess(r) {
		return
	}

	// Sets the current status if request is nil
	var status string
	if r != nil {
		status = r.Status
	}

	// Sets logger to track request
	if c.logLevel.Matches(aws.LogDebugWithRequestErrors) {
		c.logger.Log(debugLog, defaultIBMCImpLog, req.Method, req.URL, "Status:", status, "Error:", e)
	}

	// Needs explanation -- RDS
	for i, sleep := 0, c.InitialBackOff; i < c.MaxRetries; i, sleep = i+1, c.BackOffProgression(sleep) {
		if c.logLevel.Matches(aws.LogDebugWithRequestRetries) {
			c.logger.Log(debugLog, defaultIBMCImpLog, req.Method, req.URL, "Retry:", i+1)
		}
		time.Sleep(sleep)
		req = copyRequest(req)
		r, e = c.Client.Do(req)
		if e == nil && isSuccess(r) {
			return
		}

		if r != nil {
			status = r.Status
		}
		if c.logLevel.Matches(aws.LogDebugWithRequestErrors) {
			c.logger.Log(debugLog, defaultIBMCImpLog, req.Method,
				req.URL, "Retry:", i+1, "Status:", status, "Error:", e)
		}
	}
	return
}

// only copies method, url, body , headers
// tight coupled to the token manager and the way request is build
// Paramter:
//		An HTTP Request object
// Returns:
//		A built HTTP Request object with header
func copyRequest(r *http.Request) *http.Request {
	buf, _ := ioutil.ReadAll(r.Body)
	newReader := ioutil.NopCloser(bytes.NewBuffer(buf))
	req, _ := http.NewRequest(r.Method, r.URL.String(), newReader)
	for k, lv := range r.Header {
		for _, v := range lv {
			req.Header.Add(k, v)
		}
	}
	return req
}
