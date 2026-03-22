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
//	A new provider with AWS config, Trusted Profile ID, CR token file path or ApiKey, IBM IAM Authentication Server Endpoint and
//	Service Instance ID
func NewEnvProviderTrustedProfile(config *aws.Config) *TrustedProfileProvider {
	trustedProfileID := os.Getenv("TRUSTED_PROFILE_ID")
	trustedProfileName := os.Getenv("TRUSTED_PROFILE_NAME")
	iamAccountID := os.Getenv("IAM_ACCOUNT_ID")
	serviceInstanceID := os.Getenv("IBM_SERVICE_INSTANCE_ID")
	crTokenFilePath := os.Getenv("CR_TOKEN_FILE_PATH")
	authEndPoint := os.Getenv("IBM_AUTH_ENDPOINT")
	serviceIdApiKey := os.Getenv("IBM_SERVICE_ID_API_KEY")

	tpConfig := &TrustedProfileConfig{
		TrustedProfileID:   trustedProfileID,
		ServiceIDApiKey:    serviceIdApiKey,
		TrustedProfileName: trustedProfileName,
		IAMAccountID:       iamAccountID,
		CrTokenFilePath:    crTokenFilePath,
	}
	if crTokenFilePath != "" {
		return NewTrustedProfileProviderWithConfig(TrustedProfileProviderName, config, authEndPoint, tpConfig, serviceInstanceID, ResourceComputeResource)
	} else {
		return NewTrustedProfileProviderWithConfig(TrustedProfileProviderName, config, authEndPoint, tpConfig, serviceInstanceID, ResourceServiceID)

	}

}

// NewEnvCredentials Constructor
func NewEnvCredentialsTrustedProfile(config *aws.Config) *credentials.Credentials {
	return credentials.NewCredentials(NewEnvProviderTrustedProfile(config))
}
