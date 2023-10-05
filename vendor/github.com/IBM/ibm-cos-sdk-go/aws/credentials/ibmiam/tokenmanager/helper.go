package tokenmanager

import "net/http"

// Constant values for the token manager package and testcases
const (
	// Not Match Constants
	httpClientNotMatch         = "Http Client did not match"
	maxRetriesNotMatch         = "Max Retries did not match"
	logLevelNotMatch           = "Log Level did not match"
	loggerNotMatch             = "logger did not match"
	backoffProgressionNotMatch = "BackOff Progression did not match"
	numberOfLogEntriesNotMatch = "Number of log entries do not match"
	tokensNotMatch             = "Tokens do not Match"

	// Backoff constants
	initialBackoffUnset     = "Initial BackOff unset"
	backoffProgressionUnset = "BackOff Progression unset"

	// Error constants
	errorBuildingRequest = "Error Building Request"
	badNumberOfRetries   = "Bad Number of retries"
	errorGettingToken    = "Error getting token"

	// Global LOGGER constant
	debugLog = "<DEBUG>"

	// LOGGER constant for IBM Client Implementation
	defaultIBMCImpLog = "defaultIBMCImplementation"

	// LOGGER constant for IBM Token Management Implementation
	defaultTMImpLog      = "defaultTMImplementation"
	getOpsLog            = "GET OPERATION"
	backgroundRefreshLog = "BACKGROUND REFRESH"

	// Global constants
	endPoint = "EndPoint"
)

// Returns a success response code (200 <= code < 300)
func isSuccess(response *http.Response) bool {
	return response.StatusCode >= 200 && response.StatusCode < 300
}
