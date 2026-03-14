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

	// Resource type - CR, SID, etc
	resourceType ResourceType

	// Error
	ErrorStatus error

	// Logger attributes
	logger   aws.Logger
	logLevel *aws.LogLevelType
}

// TrustedProfileConfig has all the authentication parameters for trusted profile.
type TrustedProfileConfig struct {
	TrustedProfileName string
	CrTokenFilePath    string
	TrustedProfileID   string
	IAMAccountID       string
	ServiceIDApiKey    string
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
//
// Deprecated: Use NewTrustedProfileProviderWithConfig instead.
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
			provider.logger.Log(debugLog, "<IBM TRUSTED PROFILE PROVIDER>", provider.ErrorStatus)
		}
		return
	}

	if trustedProfileID == "" {
		provider.ErrorStatus = awserr.New("trustedProfileIDNotFound", "Trusted profile id not found", nil)
		if provider.logLevel.Matches(aws.LogDebug) {
			provider.logger.Log(debugLog, "<IBM TRUSTED PROFILE PROVIDER>", provider.ErrorStatus)
		}
		return
	}

	provider.serviceInstanceID = serviceInstanceID

	if authEndPoint == "" {
		authEndPoint = defaultAuthEndPoint
		if provider.logLevel.Matches(aws.LogDebug) {
			provider.logger.Log(debugLog, "<IBM TRUSTED PROFILE PROVIDER>", "using default auth endpoint", authEndPoint)
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
			provider.logger.Log(debugLog, "<IBM TRUSTED PROFILE PROVIDER>", provider.ErrorStatus)
		}
		return
	}
	provider.authenticator = authenticator

	return provider
}

// NewTrustedProfileProviderWithConfig allows the creation of a custom IBM IAM Trusted Profile Provider with trusted profile config
// Parameters:
//
//		Provider Name
//		AWS Config
//		IBM IAM Authentication Server Endpoint
//		Trusted Profile Config
//		Service Instance ID
//		Resource type
//
// Returns:
//
//		TrustedProfileProvider

func NewTrustedProfileProviderWithConfig(providerName string, config *aws.Config, authEndPoint string, trustedProfileConfig *TrustedProfileConfig,
	serviceInstanceID string, resourceType ResourceType) (provider *TrustedProfileProvider) {
	provider = new(TrustedProfileProvider)

	provider.providerName = providerName
	provider.providerType = "oauth"
	provider.resourceType = resourceType

	logLevel := aws.LogLevel(aws.LogOff)
	if config != nil && config.LogLevel != nil && config.Logger != nil {
		logLevel = config.LogLevel
		provider.logger = config.Logger
	}
	provider.logLevel = logLevel
	if trustedProfileConfig == nil {
		provider.ErrorStatus = awserr.New("trustedProfileConfigNil", "TrustedProfileConfig cannot be nil", nil)
		return
	}

	if trustedProfileConfig.CrTokenFilePath == "" && trustedProfileConfig.ServiceIDApiKey == "" {
		provider.ErrorStatus = awserr.New("CredentialsNotFound", "CR Token file path not found or Service Id's api key not found", nil)
		if provider.logLevel.Matches(aws.LogDebug) {
			provider.logger.Log(debugLog, "<IBM TRUSTED PROFILE PROVIDER>", provider.ErrorStatus)
		}
		return
	}

	provider.serviceInstanceID = serviceInstanceID

	if authEndPoint == "" {
		authEndPoint = defaultAuthEndPoint
		if provider.logLevel.Matches(aws.LogDebug) {
			provider.logger.Log(debugLog, "<IBM TRUSTED PROFILE PROVIDER>", "using default auth endpoint", authEndPoint)
		}
	}
	var authenticatorErr error
	// This authenticator is dynamically initialized based on the resourceType parameter.
	if resourceType == ResourceComputeResource {
		// For computed resource we need to make sure that the trusted profile ID is not null
		if trustedProfileConfig.TrustedProfileID == "" {
			provider.ErrorStatus = awserr.New("trustedProfileIDNotFound", "Trusted profile id not found", nil)
			if provider.logLevel.Matches(aws.LogDebug) {
				provider.logger.Log(debugLog, "<IBM TRUSTED PROFILE PROVIDER>", provider.ErrorStatus)
			}
			return
		}
		// Here cr-token based resources is specified, hence initialized accordingly.
		authenticator, err := core.NewContainerAuthenticatorBuilder().
			SetCRTokenFilename(trustedProfileConfig.CrTokenFilePath).
			SetIAMProfileID(trustedProfileConfig.TrustedProfileID).
			SetURL(authEndPoint).
			SetDisableSSLVerification(true).
			Build()
		authenticatorErr = err
		provider.authenticator = authenticator
	} else if resourceType == ResourceServiceID {
		if trustedProfileConfig.TrustedProfileID == "" && trustedProfileConfig.TrustedProfileName == "" {
			provider.ErrorStatus = awserr.New("trustedProfileDetailsNotFound", "Trusted profile id or TrustedProfileName not found", nil)
			if provider.logLevel.Matches(aws.LogDebug) {
				provider.logger.Log(debugLog, "<IBM TRUSTED PROFILE PROVIDER>", provider.ErrorStatus)
			}
			return
		}
		//For service ID either trusted profile name of trusted profile id is required
		if trustedProfileConfig.TrustedProfileID != "" && trustedProfileConfig.TrustedProfileName != "" {
			provider.ErrorStatus = awserr.New("trustedProfileInputConflict", "Either provide TrustedProfileId or TrustedProfileName not both", nil)
			if provider.logLevel.Matches(aws.LogDebug) {
				provider.logger.Log(debugLog, "<IBM TRUSTED PROFILE PROVIDER>", provider.ErrorStatus)
			}
			return
		}
		// If  TrustedProfileName is provided.
		if trustedProfileConfig.TrustedProfileName != "" {
			//   If using trustedProfile name IamAccountID is also required to identify the right account , because trusted profile name is only unique within an account.
			if trustedProfileConfig.IAMAccountID == "" {
				provider.ErrorStatus = awserr.New("trustedProfileMissingRequiredField", "IamAccountId is required when using TrustedProfileName", nil)
				if provider.logLevel.Matches(aws.LogDebug) {
					provider.logger.Log(debugLog, "<IBM TRUSTED PROFILE PROVIDER>", provider.ErrorStatus)
				}
				return
			} else {
				// Here service-Id based resources with trustedProfileName is specified, hence initialized accordingly.
				authenticator, err := core.NewIamAssumeAuthenticatorBuilder().
					SetApiKey(trustedProfileConfig.ServiceIDApiKey).
					SetIAMProfileName(trustedProfileConfig.TrustedProfileName).
					SetIAMAccountID(trustedProfileConfig.IAMAccountID).
					Build()
				authenticatorErr = err
				provider.authenticator = authenticator
			}
		}
		if trustedProfileConfig.TrustedProfileID != "" {
			// Here service-Id based resources with trustedProfileId is specified, hence initialized accordingly.
			authenticator, err := core.NewIamAssumeAuthenticatorBuilder().
				SetApiKey(trustedProfileConfig.ServiceIDApiKey).
				SetIAMProfileID(trustedProfileConfig.TrustedProfileID).
				Build()
			authenticatorErr = err
			provider.authenticator = authenticator
		}
		if authenticatorErr != nil {
			provider.ErrorStatus = awserr.New("errCreatingAuthenticatorClient", "cannot setup new Authenticator client", authenticatorErr)
			if provider.logLevel.Matches(aws.LogDebug) {
				provider.logger.Log(debugLog, "<IBM TRUSTED PROFILE PROVIDER>", provider.ErrorStatus)
			}
			return
		}
	}
	// When other resources are supported, the authenticator should be initialized accordingly.
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
	var tokenValue string
	var err error
	if p.resourceType == ResourceComputeResource {
		tokenValue, err = p.authenticator.(*core.ContainerAuthenticator).GetToken() // Cr-token based resources, hence it is assigned to ContainerAuthenticator.
	} else if p.resourceType == ResourceServiceID {
		tokenValue, err = p.authenticator.(*core.IamAssumeAuthenticator).GetToken() // Service-Id based resources, hence it is assigned to IamAssumeAuthenticator
	}
	if err != nil {
		var returnErr error
		if p.logLevel.Matches(aws.LogDebug) {
			p.logger.Log(debugLog, ibmiamProviderLog, p.providerName, "ERROR ON GET", err)
		}
		returnErr = awserr.New("TokenManagerRetrieveError", "error retrieving the token", err)
		return credentials.Value{}, returnErr
	}
	// When other resource types are supported, the respective class should be used accordingly.
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
