package tokenmanager

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/IBM/ibm-cos-sdk-go/aws/awserr"

	"github.com/IBM/ibm-cos-sdk-go/aws"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials/ibmiam/token"
)

// Constants used to retrieve the initial token and to refresh tokens
const (
	iamClientID     = "bx"
	iamClientSecret = "bx"

	grantAPIKey       = "urn:ibm:params:oauth:grant-type:apikey"
	grantRefreshToken = "refresh_token"
)

var (
	// minimum time before expiration to refresh
	minimumDelta = time.Duration(3) * time.Second

	// minimum time between refresh daemons calls,
	// to avoid background thread flooding and
	// starvation of the Get when the token does not renew
	minimumWait = time.Duration(1) * time.Second

	// DefaultAdvisoryTimeoutFunc set the advisory  timeout to 25% of remaining time - usually 15 minutes on 1 hour expiry
	DefaultAdvisoryTimeoutFunc = func(ttl time.Duration) time.Duration {
		return time.Duration(float64(ttl.Nanoseconds())*0.25) * time.Nanosecond
	}

	// DefaultMandatoryTimeoutFunc set the mandatory timeout to 17% of remaining time - usually 10 minutes on 1 hour expiry
	DefaultMandatoryTimeoutFunc = func(ttl time.Duration) time.Duration {
		return time.Duration(float64(ttl.Nanoseconds())*0.17) * time.Nanosecond
	}

	// ErrFetchingIAMTokenFn returns the error fetching token for Token Manager
	ErrFetchingIAMTokenFn = func(err error) awserr.Error {
		return awserr.New("ErrFetchingIAMToken", "error fetching token", err)
	}
)

type defaultTMImplementation struct {
	// endpoint used to retrieve tokens
	authEndPoint string
	// client used to retrieve tokens, implements the Retry behaviour
	client IBMClientDo

	// timeout used by background thread
	advisoryRefreshTimeout func(ttl time.Duration) time.Duration
	// timeout used by get token to decide if blocks and refresh token
	// and by background refresh when advisory not set or smaller than mandatory
	mandatoryRefreshTimeout func(ttl time.Duration) time.Duration
	// time provider used to get current time
	timeProvider func() time.Time
	// original time to live of the token at the moment it was retrieved
	tokenTTL time.Duration

	// token value kept for its TTL
	Cache *token.Token
	// timer used to refresh the Token
	timer *time.Timer
	// nullable boolean used to enable disable the background refresh
	enableBackgroundRefresh *bool
	// read write mutex to sync access
	mutex sync.RWMutex
	// function used to retrieve initial token
	initFunc func() (*token.Token, error)

	// logger where the logging is sent
	logger aws.Logger
	// level of logging enabled
	logLevel *aws.LogLevelType
}

// function to create a new token manager using an APIKey to retrieve first token
func newTokenManagerFromAPIKey(config *aws.Config, apiKey, authEndPoint string, advisoryRefreshTimeout,
	mandatoryRefreshTimeout func(time.Duration) time.Duration, timeFunc func() time.Time,
	client IBMClientDo) *defaultTMImplementation {
	// when the client is nil creates a new one using the config passed as argument
	if client == nil {
		client = defaultIBMClient(config)
	}

	// set the function to get the initial token the defaultInit that uses the APIKey passed as argument
	initFunc := defaultInit(apiKey, authEndPoint, client)
	return newTokenManager(config, initFunc, authEndPoint, advisoryRefreshTimeout, mandatoryRefreshTimeout, timeFunc,
		client)
}

// default init function,
// uses the APIKey passed as argument to obtain the first token
func defaultInit(apiKey string, authEndPoint string, client IBMClientDo) func() (*token.Token, error) {
	return func() (*token.Token, error) {
		data := url.Values{
			"apikey": {apiKey},
		}
		// build the http request
		req, err := buildRequest(authEndPoint, grantAPIKey, data)
		// checks for errors
		if err != nil {
			return nil, ErrFetchingIAMTokenFn(err)
		}
		// calls the end point
		response, err := client.Do(req)
		// checks for errors
		if err != nil {
			return nil, ErrFetchingIAMTokenFn(err)
		}
		// parse the response
		tokenValue, err := processResponse(response)
		// checks for errors
		if err != nil {
			return nil, ErrFetchingIAMTokenFn(err)
		}
		// returns the token
		return tokenValue, nil
	}
}

// creates a token manager,
// the initial token is obtained using a custom function
func newTokenManager(config *aws.Config, initFunc func() (*token.Token, error), authEndPoint string,
	advisoryRefreshTimeout, mandatoryRefreshTimeout func(time.Duration) time.Duration, timeFunc func() time.Time,
	client IBMClientDo) *defaultTMImplementation {
	// if no time function passed uses the time.Now
	if timeFunc == nil {
		timeFunc = time.Now
	}

	// if no value passed use the one stored as global
	if advisoryRefreshTimeout == nil {
		advisoryRefreshTimeout = DefaultAdvisoryTimeoutFunc
	}

	// if no value passed use the one stored as global
	if mandatoryRefreshTimeout == nil {
		mandatoryRefreshTimeout = DefaultMandatoryTimeoutFunc
	}

	// checks the logLevel and logger,
	// only sets the loveLevel when logLevel and logger are not ZERO values
	// helps reducing the logic since logLevel needs to be checked
	logLevel := aws.LogLevel(aws.LogOff)
	if config != nil && config.LogLevel != nil && config.Logger != nil {
		logLevel = config.LogLevel
	}

	// builds a defaultTMImplementation using the provided parameters
	tm := &defaultTMImplementation{
		authEndPoint:            authEndPoint,
		client:                  client,
		advisoryRefreshTimeout:  advisoryRefreshTimeout,
		mandatoryRefreshTimeout: mandatoryRefreshTimeout,
		timeProvider:            timeFunc,
		initFunc:                initFunc,

		logLevel: logLevel,
		logger:   config.Logger,
	}
	return tm
}

// function to obtain to initialize the token manager in a concurrent safe way
func (tm *defaultTMImplementation) init() (*token.Token, error) {
	// checks logLevel and logs
	if tm.logLevel.Matches(aws.LogDebug) {
		tm.logger.Log(debugLog, defaultTMImpLog, "INIT")
	}
	// fetches the initial vale using the init function
	tokenValue, err := tm.initFunc()
	if err != nil {
		// checks logLevel and logs
		if tm.logLevel.Matches(aws.LogDebug) {
			tm.logger.Log(debugLog, defaultTMImpLog, "INIT FAILED", err)
		}
		return nil, err
	}
	// sets current cache value the value fetched by the init call
	tm.Cache = tokenValue
	result := *tm.Cache
	// sets token time to live
	tm.tokenTTL = getTTL(tokenValue.Expiration, tm.timeProvider)
	// checks and sets if background thread is enabled
	if tm.enableBackgroundRefresh == nil {
		tm.enableBackgroundRefresh = aws.Bool(true)
	}
	// resets the time
	tm.resetTimer()
	// checks logLevel and logs
	if tm.logLevel.Matches(aws.LogDebug) {
		tm.logger.Log(debugLog, defaultTMImpLog, "INIT SUCCEEDED")
	}
	return &result, nil
}

// function to call the init operation in a concurrent safe way, managing the RWLock
func retrieveInit(tm *defaultTMImplementation) (unlockOP func(), tk *token.Token, err error) {
	// escalate the READ lock to a WRITE lock
	now := time.Now()
	tm.mutex.RUnlock()
	tm.mutex.Lock()
	// set unlock Operation to Write Unlock
	unlockOP = tm.mutex.Unlock

	// checks logLevel and logs
	if tm.logLevel.Matches(aws.LogDebug) {
		tm.logger.Log(debugLog, defaultTMImpLog, getOpsLog,
			"TOKEN MANAGER NOT INITIALIZED - ACQUIRED FULL LOCK IN", time.Now().Sub(now))
	}

	// since another routine could be scheduled between the release of Read mutex and the acquire of Write mutex
	// re-check the init is still required
	if tm.Cache == nil {
		tk, err = tm.init()
	} else {
		tk = retrieveCheckGet(tm)
	}
	return
}

// function to call the refresh operation in a concurrent safe way, managing the RWLock
func retrieveFetch(tm *defaultTMImplementation) (unlockOP func(), tk *token.Token, err error) {

	// escalate the READ lock to a WRITE lock
	now := time.Now()
	tm.mutex.RUnlock()
	tm.mutex.Lock()
	// set unlock Operation to Write Unlock
	unlockOP = tm.mutex.Unlock

	// checks logLevel and logs
	if tm.logLevel.Matches(aws.LogDebug) {
		tm.logger.Log(debugLog, defaultTMImpLog, getOpsLog,
			"TOKEN REFRESH - ACQUIRED FULL LOCK IN", time.Now().Sub(now))
	}

	// since another routine could be scheduled between the release of Read mutex and the acquire of Write mutex
	// re-check the refresh is still required
	tk = retrieveCheckGet(tm)
	for tk == nil {
		err := tm.refresh()
		if err != nil {
			// checks logLevel and logs
			if tm.logLevel.Matches(aws.LogDebug) {
				tm.logger.Log(debugLog, defaultTMImpLog, getOpsLog, "REFRESH FAILED", err)
			}
			return unlockOP, nil, err
		}
		tk = retrieveCheckGet(tm)
	}

	return
}

// function to check the the token in cache and get it if valid
func retrieveCheckGet(tm *defaultTMImplementation) (tk *token.Token) {
	// calculates the TTL of the token in the cache
	tokenTTL := waitingTime(tm.tokenTTL, tm.Cache.Expiration, nil, tm.mandatoryRefreshTimeout, tm.timeProvider)
	// check if token is valid
	if tokenTTL == nil || *tokenTTL > minimumDelta {
		// set result to be cache content
		tk = tm.Cache
	}
	return
}

// Get retrieves the value of the auth token, checks the cache if the token is valid returns it,
// if not valid does a refresh and then returns it
func (tm *defaultTMImplementation) Get() (tk *token.Token, err error) {

	// holder for the func to be called in the defer
	var unlockOP func()
	// defer the call of the unlock operation
	defer func() {
		unlockOP()
	}()

	now := time.Now()

	// acquire Read lock
	tm.mutex.RLock()
	// set unlock operation to ReadUnlock
	unlockOP = tm.mutex.RUnlock

	// checks logLevel and logs
	if tm.logLevel.Matches(aws.LogDebug) {
		tm.logger.Log(debugLog, defaultTMImpLog, getOpsLog, "ACQUIRED RLOCK IN", time.Now().Sub(now))
	}

	//check if cache was initialized
	if tm.Cache == nil {
		// if cache not initialized, initialize it
		unlockOP, tk, err = retrieveInit(tm)
		return
	}

	// check and retrieves content of cache
	tk = retrieveCheckGet(tm)
	// check if the content of cache is valid
	if tk == nil {
		// content of the cache invalid
		// refresh cache content
		unlockOP, tk, err = retrieveFetch(tm)
	}

	return
}

// function to do the refresh operation calls
func (tm *defaultTMImplementation) refresh() error {
	// stop the timer
	tm.stopTimer()
	// defer timer reset
	defer tm.resetTimer()
	// set the refresh token parameter of the request
	data := url.Values{
		"refresh_token": {tm.Cache.RefreshToken},
	}
	// build the request
	req, err := buildRequest(tm.authEndPoint, grantRefreshToken, data)
	if err != nil {
		return ErrFetchingIAMTokenFn(err)
	}
	// call the endpoint
	response, err := tm.client.Do(req)
	if err != nil {
		return ErrFetchingIAMTokenFn(err)
	}
	// parse the response
	tokenValue, err := processResponse(response)
	if err != nil {
		if response.StatusCode == 400 {
			// Initialize new token when REFRESH TOKEN got invalid
			if tm.logLevel.Matches(aws.LogDebug) {
				tm.logger.Log(debugLog, defaultTMImpLog, "REFRESH TOKEN INVALID. NEW TOKEN INITIALIZED", err, response.Header["Transaction-Id"], response.Body)
			}
			tm.init()
			return nil
		} else {
			if tm.logLevel.Matches(aws.LogDebug) {
				tm.logger.Log(debugLog, defaultTMImpLog, "REFRESH TOKEN EXCHANGE FAILED", err, response.Header["Transaction-Id"], response.Body)
			}
			return ErrFetchingIAMTokenFn(err)
		}
	}
	// sets current token to the value fetched
	tm.Cache = tokenValue
	// sets TTL
	tm.tokenTTL = getTTL(tokenValue.Expiration, tm.timeProvider)
	return nil
}

// Refresh forces the refresh of the token in the cache in a concurrent safe way
func (tm *defaultTMImplementation) Refresh() error {
	// acquire a Write lock
	tm.mutex.Lock()
	// defer the release of the write lock
	defer tm.mutex.Unlock()
	// checks logLevel and logs
	if tm.logLevel.Matches(aws.LogDebug) {
		tm.logger.Log(debugLog, defaultTMImpLog, "MANUAL TRIGGER BACKGROUND REFRESH")
	}
	return tm.refresh()
}

// callback function used by to timer to refresh tokens in background
func (tm *defaultTMImplementation) backgroundRefreshFunc() {
	now := time.Now()
	// acquire a Write lock
	tm.mutex.Lock()
	// defer the release of the write lock
	defer tm.mutex.Unlock()

	// checks logLevel and logs
	if tm.logLevel.Matches(aws.LogDebug) {
		tm.logger.Log(debugLog, defaultTMImpLog, backgroundRefreshLog,
			"ACQUIRED FULL LOCK IN", time.Now().Sub(now))
	}
	wait := waitingTime(tm.tokenTTL, tm.Cache.Expiration, tm.advisoryRefreshTimeout,
		tm.mandatoryRefreshTimeout, tm.timeProvider)
	// checks logLevel and logs
	if tm.logLevel.Matches(aws.LogDebug) {
		tm.logger.Log(debugLog, defaultTMImpLog, backgroundRefreshLog, "TOKEN TTL", wait)
	}
	if wait != nil && *wait < minimumDelta {
		// checks logLevel and logs
		if tm.logLevel.Matches(aws.LogDebug) {
			tm.logger.Log(debugLog, defaultTMImpLog, backgroundRefreshLog, "TOKEN NEED UPDATE")
		}
		tm.refresh()
	} else {
		// checks logLevel and logs
		if tm.logLevel.Matches(aws.LogDebug) {
			tm.logger.Log(debugLog, defaultTMImpLog, backgroundRefreshLog, "TOKEN UPDATE SKIPPED")
		}
		tm.resetTimer()
	}
}

// StopBackgroundRefresh force the stop of the refresh background token in a concurrent safe way
func (tm *defaultTMImplementation) StopBackgroundRefresh() {
	// acquire a Write lock
	tm.mutex.Lock()
	// defer the release of the write lock
	defer tm.mutex.Unlock()
	tm.stopTimer()
	tm.enableBackgroundRefresh = aws.Bool(false)
	// checks logLevel and logs
	if tm.logLevel.Matches(aws.LogDebug) {
		tm.logger.Log(debugLog, defaultTMImpLog, "STOP BACKGROUND REFRESH")
	}
}

// StartBackgroundRefresh starts the background refresh thread in a concurrent sage way
func (tm *defaultTMImplementation) StartBackgroundRefresh() {
	// acquire a Write lock
	tm.mutex.Lock()
	// defer the release of the write lock
	defer tm.mutex.Unlock()
	tm.enableBackgroundRefresh = aws.Bool(true)
	tm.resetTimer()
	// checks logLevel and logs
	if tm.logLevel.Matches(aws.LogDebug) {
		tm.logger.Log(debugLog, defaultTMImpLog, "START BACKGROUND REFRESH")
	}
}

// helper function to stop the timer in the token manager used to trigger token background refresh
func (tm *defaultTMImplementation) stopTimer() {
	if tm.timer != nil {
		tm.timer.Stop()
	}
}

// helper function used to reset the timer
func (tm *defaultTMImplementation) resetTimer() {
	// checks if background refresh is enabled
	if tm.enableBackgroundRefresh != nil && *tm.enableBackgroundRefresh {
		// calculates the how long tpo wait for next refresh
		refreshIn := waitingTime(tm.tokenTTL, tm.Cache.Expiration, tm.advisoryRefreshTimeout,
			tm.mandatoryRefreshTimeout, tm.timeProvider)
		// checks if waiting time is not nil,
		// no nedd to refresh
		if refreshIn != nil {
			// checks if timer exists
			// rest time of the existing timer
			if tm.timer != nil {
				if minimumWait > *refreshIn {
					*refreshIn = minimumWait
				}
				tm.timer.Reset(*refreshIn)
			} else {
				// if timer not exists
				// create a new timer
				tm.timer = time.AfterFunc(*refreshIn, tm.backgroundRefreshFunc)
			}
		} else {
			tm.timer = nil
		}
	}
}

// helper function used to build the http request used to retrieve initial and refresh tokens
func buildRequest(endPoint string, grantType string, customValues url.Values) (*http.Request, error) {
	data := url.Values{
		"grant_type":    {grantType},
		"response_type": {"cloud_iam"},
	}
	for key, value := range customValues {
		data[key] = value
	}
	req, err := http.NewRequest(http.MethodPost, endPoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s",
		iamClientID, iamClientSecret))))
	req.Header.Set("accept", "application/json")
	req.Header.Set("Cache-control", "no-Cache")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req, nil
}

// helper function used to parse the http response into a Token struct
func processResponse(response *http.Response) (*token.Token, error) {
	bodyContent, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	err = response.Body.Close()
	if err != nil {
		return nil, err
	}
	if isSuccess(response) {
		tokenValue := token.Token{}
		err = json.Unmarshal(bodyContent, &tokenValue)
		if err != nil {
			return nil, err
		}
		return &tokenValue, nil
	} else if response.StatusCode == 400 || response.StatusCode == 401 || response.StatusCode == 403 {
		apiErr := token.Error{}
		err = json.Unmarshal(bodyContent, &apiErr)
		if err != nil {
			return nil, err
		}
		return nil, &apiErr
	} else {
		return nil, fmt.Errorf("Response: Bad Status Code: %s", response.Status)
	}
}

// helper function used to calculate the time before token expires,
// it takes in consideration the mandatory and advisory timeouts
func waitingTime(ttl time.Duration, unixTime int64, advisoryRefreshTimeout,
	mandatoryRefreshTimeout func(time.Duration) time.Duration, timeFunc func() time.Time) *time.Duration {
	if unixTime == 0 {
		return nil
	}
	timeoutAt := time.Unix(unixTime, 0)
	result := timeoutAt.Sub(timeFunc())
	delta := minimumDelta
	if advisoryRefreshTimeout != nil && advisoryRefreshTimeout(ttl) > minimumDelta {
		delta = advisoryRefreshTimeout(ttl)
	}
	if mandatoryRefreshTimeout != nil && mandatoryRefreshTimeout(ttl) > delta {
		delta = mandatoryRefreshTimeout(ttl)
	}
	result -= delta
	return &result
}

func getTTL(unixTime int64, timeFunc func() time.Time) time.Duration {
	if unixTime > 0 {
		timeoutAt := time.Unix(unixTime, 0)
		return timeoutAt.Sub(timeFunc())
	}
	return 0
}
