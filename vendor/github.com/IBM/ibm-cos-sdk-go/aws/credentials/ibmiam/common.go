package ibmiam

import (
	"runtime"

	"github.com/IBM/ibm-cos-sdk-go/aws"
	"github.com/IBM/ibm-cos-sdk-go/aws/awserr"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials/ibmiam/tokenmanager"
)

const (
	// Constants
	// Default IBM IAM Authentication Server Endpoint
	defaultAuthEndPoint = `https://iam.cloud.ibm.com/identity/token`

	// Logger constants
	// Debug Log constant
	debugLog = "<DEBUG>"
	// IBM IAM Provider Log constant
	ibmiamProviderLog = "IBM IAM PROVIDER"
)

// Provider Struct
type Provider struct {
	// Name of Provider
	providerName string

	// Type of Provider - SharedCred, SharedConfig, etc.
	providerType string

	// Token Manager Provider uses
	tokenManager tokenmanager.API

	// Service Instance ID passes in a provider
	serviceInstanceID string

	// Error
	ErrorStatus error

	// Logger attributes
	logger   aws.Logger
	logLevel *aws.LogLevelType
}

// NewProvider allows the creation of a custom IBM IAM Provider
// Parameters:
//
//	Provider Name
//	AWS Config
//	API Key
//	IBM IAM Authentication Server Endpoint
//	Service Instance ID
//	Token Manager client
//
// Returns:
//
//	Provider
func NewProvider(providerName string, config *aws.Config, apiKey, authEndPoint, serviceInstanceID string,
	client tokenmanager.IBMClientDo) (provider *Provider) { //linter complain about (provider *Provider) {
	provider = new(Provider)

	provider.providerName = providerName
	provider.providerType = "oauth"

	logLevel := aws.LogLevel(aws.LogOff)
	if config != nil && config.LogLevel != nil && config.Logger != nil {
		logLevel = config.LogLevel
		provider.logger = config.Logger
	}
	provider.logLevel = logLevel

	if apiKey == "" {
		provider.ErrorStatus = awserr.New("IbmApiKeyIdNotFound", "IBM API Key Id not found", nil)
		if provider.logLevel.Matches(aws.LogDebug) {
			provider.logger.Log(debugLog, "<IBM IAM PROVIDER BUILD>", provider.ErrorStatus)
		}
		return
	}

	provider.serviceInstanceID = serviceInstanceID

	if authEndPoint == "" {
		authEndPoint = defaultAuthEndPoint
		if provider.logLevel.Matches(aws.LogDebug) {
			provider.logger.Log(debugLog, "<IBM IAM PROVIDER BUILD>", "using default auth endpoint", authEndPoint)
		}
	}

	if client == nil {
		client = tokenmanager.DefaultIBMClient(config)
	}

	provider.tokenManager = tokenmanager.NewTokenManagerFromAPIKey(config, apiKey, authEndPoint, nil, nil, nil, client)

	runtime.SetFinalizer(provider, func(p *Provider) {
		p.tokenManager.StopBackgroundRefresh()
	})

	return
}

// IsValid ...
// Returns:
//
//	Provider validation - boolean
func (p *Provider) IsValid() bool {
	return nil == p.ErrorStatus
}

// Retrieve ...
// Returns:
//
//	Credential values
//	Error
func (p *Provider) Retrieve() (credentials.Value, error) {
	if p.ErrorStatus != nil {
		if p.logLevel.Matches(aws.LogDebug) {
			p.logger.Log(debugLog, ibmiamProviderLog, p.providerName, p.ErrorStatus)
		}
		return credentials.Value{ProviderName: p.providerName}, p.ErrorStatus
	}
	tokenValue, err := p.tokenManager.Get()
	if err != nil {
		var returnErr error
		if p.logLevel.Matches(aws.LogDebug) {
			p.logger.Log(debugLog, ibmiamProviderLog, p.providerName, "ERROR ON GET", err)
		}
		returnErr = awserr.New("TokenManagerRetrieveError", "error retrieving the token", err)
		return credentials.Value{}, returnErr
	}

	return credentials.Value{Token: *tokenValue, ProviderName: p.providerName, ProviderType: p.providerType,
		ServiceInstanceID: p.serviceInstanceID}, nil
}

// IsExpired ...
//
//	Provider expired or not - boolean
func (p *Provider) IsExpired() bool {
	return true
}
