package ibmiam

import (
	"os"

	"github.com/IBM/ibm-cos-sdk-go/aws"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials"
)

// EnvProviderName name of the IBM IAM provider that loads IAM credentials from environment
// variables
const EnvProviderName = "EnvProviderIBM"

// NewEnvProvider constructor of the IBM IAM provider that loads IAM credentials from environment
// variables
// Parameter:
//
//	AWS Config
//
// Returns:
//
//	A new provider with AWS config, API Key, IBM IAM Authentication Server Endpoint and
//	Service Instance ID
func NewEnvProvider(config *aws.Config) *Provider {
	apiKey := os.Getenv("IBM_API_KEY_ID")
	serviceInstanceID := os.Getenv("IBM_SERVICE_INSTANCE_ID")
	authEndPoint := os.Getenv("IBM_AUTH_ENDPOINT")

	return NewProvider(EnvProviderName, config, apiKey, authEndPoint, serviceInstanceID, nil)
}

// NewEnvCredentials Constructor
func NewEnvCredentials(config *aws.Config) *credentials.Credentials {
	return credentials.NewCredentials(NewEnvProvider(config))
}
