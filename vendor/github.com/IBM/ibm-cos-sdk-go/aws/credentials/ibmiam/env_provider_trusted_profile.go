package ibmiam

import (
	"os"

	"github.com/IBM/ibm-cos-sdk-go/aws"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials"
)

// EnvProviderName name of the IBM IAM provider that loads IAM trusted profile
// credentials from environment variables
const EnvProviderTrustedProfileName = "EnvProviderTrustedProfileIBM"

// NewEnvProvider constructor of the IBM IAM provider that loads IAM trusted profile
// credentials from environment variables
// Parameter:
//
//	AWS Config
//
// Returns:
//
//	A new provider with AWS config, Trusted Profile ID, CR token file path, IBM IAM Authentication Server Endpoint and
//	Service Instance ID
func NewEnvProviderTrustedProfile(config *aws.Config) *TrustedProfileProvider {
	trustedProfileID := os.Getenv("TRUSTED_PROFILE_ID")
	serviceInstanceID := os.Getenv("IBM_SERVICE_INSTANCE_ID")
	crTokenFilePath := os.Getenv("CR_TOKEN_FILE_PATH")
	authEndPoint := os.Getenv("IBM_AUTH_ENDPOINT")

	return NewTrustedProfileProvider(EnvProviderTrustedProfileName, config, authEndPoint, trustedProfileID, crTokenFilePath, serviceInstanceID, "CR")
}

// NewEnvCredentials Constructor
func NewEnvCredentialsTrustedProfile(config *aws.Config) *credentials.Credentials {
	return credentials.NewCredentials(NewEnvProviderTrustedProfile(config))
}
