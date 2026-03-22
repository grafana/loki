package tokenmanager

import (
	"time"

	"github.com/IBM/ibm-cos-sdk-go/aws"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials/ibmiam/token"
)

// API Token Manager interface
type API interface {

	// Token Management Get Function
	Get() (*token.Token, error)

	// Token Management Retries Function
	Refresh() error

	// Token Management Stop Background Refresh
	StopBackgroundRefresh()

	// Token Management Start Background Refresh
	StartBackgroundRefresh()
}

// default implementations
// wrap implementation in the interface
var (
	// NewTokenManager token manager constructor using a custom initial function to retrieve first token
	NewTokenManager = func(config *aws.Config, initFunc func() (*token.Token, error), authEndPoint string,
		advisoryRefreshTimeout, mandatoryRefreshTimeout func(time.Duration) time.Duration, timeFunc func() time.Time,
		client IBMClientDo) API {
		return newTokenManager(config, initFunc, authEndPoint, advisoryRefreshTimeout, mandatoryRefreshTimeout,
			timeFunc, client)

	}

	// NewTokenManagerFromAPIKey token manager constructor using api key to retrieve first token
	NewTokenManagerFromAPIKey = func(config *aws.Config, apiKey, authEndPoint string, advisoryRefreshTimeout,
		mandatoryRefreshTimeout func(time.Duration) time.Duration, timeFunc func() time.Time,
		client IBMClientDo) API {
		return newTokenManagerFromAPIKey(config, apiKey, authEndPoint, advisoryRefreshTimeout,
			mandatoryRefreshTimeout, timeFunc, client)
	}
)
