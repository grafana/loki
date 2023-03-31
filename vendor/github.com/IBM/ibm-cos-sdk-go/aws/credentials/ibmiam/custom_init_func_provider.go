package ibmiam

import (
	"github.com/IBM/ibm-cos-sdk-go/aws"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials/ibmiam/token"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials/ibmiam/tokenmanager"
)

// CustomInitFuncProviderName the Name of the IBM IAM provider with a custom init function
const CustomInitFuncProviderName = "CustomInitFuncProviderIBM"

// NewCustomInitFuncProvider constructor of IBM IAM Provider with a custom init Function
// Parameters:
// 			aws.config: AWS Config to provide service configuration for service clients. By default,
//				all clients will use the defaults.DefaultConfig structure.
//			initFunc token: Contents of the token
//			authEndPoint: IAM Authentication Server end point
//			serviceInstanceID: service instance ID of the IBM account
//			client: Token Management's client
// Returns:
// 			A complete Provider with Token Manager initialized
func NewCustomInitFuncProvider(config *aws.Config, initFunc func() (*token.Token, error), authEndPoint,
	serviceInstanceID string, client tokenmanager.IBMClientDo) *Provider {

	// New provider with oauth request type
	provider := new(Provider)
	provider.providerName = CustomInitFuncProviderName
	provider.providerType = "oauth"

	// Initialize LOGGER and inserts into the provider
	logLevel := aws.LogLevel(aws.LogOff)
	if config != nil && config.LogLevel != nil && config.Logger != nil {
		logLevel = config.LogLevel
		provider.logger = config.Logger
	}
	provider.logLevel = logLevel

	provider.serviceInstanceID = serviceInstanceID

	// Checks local IAM Authentication Server Endpoint; if none, sets the default auth end point
	if authEndPoint == "" {
		authEndPoint = defaultAuthEndPoint
		if provider.logLevel.Matches(aws.LogDebug) {
			provider.logger.Log("<DEBUG>", "<IBM IAM PROVIDER BUILD>", "using default auth endpoint", authEndPoint)
		}
	}

	// Checks if the client has been passed in; otherwise, create one with token manager's default IBM client
	if client == nil {
		client = tokenmanager.DefaultIBMClient(config)
	}

	provider.tokenManager = tokenmanager.NewTokenManager(config, initFunc, authEndPoint, nil, nil, nil, client)
	return provider

}

// NewCustomInitFuncCredentials costructor
func NewCustomInitFuncCredentials(config *aws.Config, initFunc func() (*token.Token, error), authEndPoint,
	serviceInstanceID string) *credentials.Credentials {
	return credentials.NewCredentials(NewCustomInitFuncProvider(config, initFunc, authEndPoint,
		serviceInstanceID, nil))
}
