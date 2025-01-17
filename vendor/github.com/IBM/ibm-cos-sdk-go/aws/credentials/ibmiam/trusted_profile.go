package ibmiam

import (
	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/ibm-cos-sdk-go/aws"
	"github.com/IBM/ibm-cos-sdk-go/aws/awserr"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials/ibmiam/token"
)

// Provider Struct
type TrustedProfileProvider struct {
	// Name of Provider
	providerName string

	// Type of Provider - SharedCred, SharedConfig, etc.
	providerType string

	// Authenticator instance, will be assigned dynamically
	authenticator core.Authenticator

	// Service Instance ID passes in a provider
	serviceInstanceID string

	// Error
	ErrorStatus error

	// Logger attributes
	logger   aws.Logger
	logLevel *aws.LogLevelType
}

// NewTrustedProfileProvider allows the creation of a custom IBM IAM Trusted Profile Provider
// Parameters:
//
//	Provider Name
//	AWS Config
//	Trusted Profile ID
//	CR token file path
//	IBM IAM Authentication Server Endpoint
//	Service Instance ID
//	Resource type
//
// Returns:
//
//	TrustedProfileProvider
func NewTrustedProfileProvider(providerName string, config *aws.Config, authEndPoint string, trustedProfileID string, crTokenFilePath string,
	serviceInstanceID string, resourceType string) (provider *TrustedProfileProvider) {
	provider = new(TrustedProfileProvider)

	provider.providerName = providerName
	provider.providerType = "oauth"

	logLevel := aws.LogLevel(aws.LogOff)
	if config != nil && config.LogLevel != nil && config.Logger != nil {
		logLevel = config.LogLevel
		provider.logger = config.Logger
	}
	provider.logLevel = logLevel

	if crTokenFilePath == "" {
		provider.ErrorStatus = awserr.New("crTokenFilePathNotFound", "CR Token file path not found", nil)
		if provider.logLevel.Matches(aws.LogDebug) {
			provider.logger.Log(debugLog, "<IBM IAM PROVIDER BUILD>", provider.ErrorStatus)
		}
		return
	}

	if trustedProfileID == "" {
		provider.ErrorStatus = awserr.New("trustedProfileIDNotFound", "Trusted profile id not found", nil)
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

	// This authenticator is dynamically initialized based on the resourceType parameter.
	// Since only cr-token based resources is supported now, it is initialized directly.
	// when other resources are supported, the authenticator should be initialized accordingly.
	authenticator, err := core.NewContainerAuthenticatorBuilder().
		SetCRTokenFilename(crTokenFilePath).
		SetIAMProfileID(trustedProfileID).
		SetURL(authEndPoint).
		SetDisableSSLVerification(true).
		Build()
	if err != nil {
		provider.ErrorStatus = awserr.New("errCreatingAuthenticatorClient", "cannot setup new Authenticator client", err)
		if provider.logLevel.Matches(aws.LogDebug) {
			provider.logger.Log(debugLog, "<IBM IAM PROVIDER BUILD>", provider.ErrorStatus)
		}
		return
	}
	provider.authenticator = authenticator

	return provider
}

// IsValid ...
// Returns:
//
//	TrustedProfileProvider validation - boolean
func (p *TrustedProfileProvider) IsValid() bool {
	return nil == p.ErrorStatus
}

// Retrieve ...
// Returns:
//
//	Credential values
//	Error
func (p *TrustedProfileProvider) Retrieve() (credentials.Value, error) {
	if p.ErrorStatus != nil {
		if p.logLevel.Matches(aws.LogDebug) {
			p.logger.Log(debugLog, ibmiamProviderLog, p.providerName, p.ErrorStatus)
		}
		return credentials.Value{ProviderName: p.providerName}, p.ErrorStatus
	}

	// The respective resourceTypes's class should be called based on the resourceType parameter.
	// Since only cr-token based resources is supported now, it is assigned to ContainerAuthenticator
	// directly. when other resource types are supported, the respective class should be used accordingly.
	tokenValue, err := p.authenticator.(*core.ContainerAuthenticator).GetToken()

	if err != nil {
		var returnErr error
		if p.logLevel.Matches(aws.LogDebug) {
			p.logger.Log(debugLog, ibmiamProviderLog, p.providerName, "ERROR ON GET", err)
		}
		returnErr = awserr.New("TokenManagerRetrieveError", "error retrieving the token", err)
		return credentials.Value{}, returnErr
	}

	return credentials.Value{
		Token: token.Token{
			AccessToken: tokenValue,
			TokenType:   "Bearer",
		},
		ProviderName:      p.providerName,
		ProviderType:      p.providerType,
		ServiceInstanceID: p.serviceInstanceID,
	}, nil
}

// IsExpired ...
//
//	TrustedProfileProvider expired or not - boolean
func (p *TrustedProfileProvider) IsExpired() bool {
	return true
}
