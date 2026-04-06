package ibmiam

import (
	"github.com/IBM/ibm-cos-sdk-go/aws"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials"
)

type ResourceType string

// TrustedProfileProviderName name of the IBM IAM provider that uses IAM trusted-profile
// details passed directly
const (
	TrustedProfileProviderName              = "TrustedProfileProviderIBM"
	ResourceComputeResource    ResourceType = "CR"
	ResourceServiceID          ResourceType = "SID"
)

// NewTrustedProfileProviderWithCR constructor of the IBM IAM provider that uses IAM trusted-profile
// details passed
// Returns: New TrustedProfileProvider (AWS type)
func NewTrustedProfileProviderCR(config *aws.Config, authEndPoint string, trustedProfileID string, crTokenFilePath string, serviceInstanceID string) *TrustedProfileProvider {

	tpConfig := &TrustedProfileConfig{
		TrustedProfileID: trustedProfileID,
		CrTokenFilePath:  crTokenFilePath,
	}
	// Resource type ResourceComputeResource is passed to identify that this is a CR-token based
	// resource.
	return NewTrustedProfileProviderWithConfig(TrustedProfileProviderName, config, authEndPoint, tpConfig, serviceInstanceID, ResourceComputeResource)
}

// NewTrustedProfileCredentials constructor for IBM IAM that uses IAM trusted-profile-id, crTokenFilePath
// credentials passed
// Returns: credentials.NewCredentials(newTrustedProfileProvider()) (AWS type)
func NewTrustedProfileCredentialsCR(config *aws.Config, authEndPoint string, trustedProfileID string, crTokenFilePath string, serviceInstanceID string) *credentials.Credentials {
	return credentials.NewCredentials(NewTrustedProfileProviderCR(config, authEndPoint, trustedProfileID, crTokenFilePath, serviceInstanceID))
}

// NewTrustedProfileCredentials constructor for IBM IAM that uses IAM trusted-profile-id , serviceId-api-key
// credentials passed
// Returns: credentials.NewCredentials(newTrustedProfileProvider()) (AWS type)
func NewTrustedProfileProviderServiceIdWithTrustedProfileId(config *aws.Config, authEndPoint string, trustedProfileID string, serviceIdApiKey string, serviceInstanceID string) *TrustedProfileProvider {

	tpConfig := &TrustedProfileConfig{
		TrustedProfileID: trustedProfileID,
		ServiceIDApiKey:  serviceIdApiKey,
	}
	// Resource type ResourceServiceID is passed to identify that this is a Service Id based
	// resource.
	return NewTrustedProfileProviderWithConfig(TrustedProfileProviderName, config, authEndPoint, tpConfig, serviceInstanceID, ResourceServiceID)
}

// NewTrustedProfileCredentials constructor for IBM IAM that uses IAM trusted-profile-id, serviceId-api-key
// credentials passed
// Returns: credentials.NewCredentials(newTrustedProfileProvider()) (AWS type)
func NewTrustedProfileCredentialsServiceIDWithTrustedProfileId(config *aws.Config, authEndPoint string, trustedProfileID string, serviceIdApiKey string, serviceInstanceID string) *credentials.Credentials {
	return credentials.NewCredentials(NewTrustedProfileProviderServiceIdWithTrustedProfileId(config, authEndPoint, trustedProfileID, serviceIdApiKey, serviceInstanceID))
}

// NewTrustedProfileCredentials constructor for IBM IAM that uses IAM trusted-profile-name, iam-account-id, serviceId-api-key
// credentials passed
// Returns: credentials.NewCredentials(newTrustedProfileProvider()) (AWS type)
func NewTrustedProfileProviderServiceIdWithTrustedProfileName(config *aws.Config, authEndPoint string, trustedProfileName string, iamAccountId string, serviceIdApiKey string, serviceInstanceID string) *TrustedProfileProvider {

	tpConfig := &TrustedProfileConfig{
		TrustedProfileName: trustedProfileName,
		IAMAccountID:       iamAccountId,
		ServiceIDApiKey:    serviceIdApiKey,
	}
	// Resource type ResourceServiceID is passed to identify that this is a Service Id based
	// resource.
	return NewTrustedProfileProviderWithConfig(TrustedProfileProviderName, config, authEndPoint, tpConfig, serviceInstanceID, ResourceServiceID)
}

//	NewTrustedProfileCredentials constructor for IBM IAM that uses IAM trusted-profile-id, serviceId-api-key
//
// credentials passed
// Returns: credentials.NewCredentials(newTrustedProfileProvider()) (AWS type)
func NewTrustedProfileCredentialsServiceIDWithTrustedProfileName(config *aws.Config, authEndPoint string, trustedProfileName string, iamAccountId string, serviceIdApiKey string, serviceInstanceID string) *credentials.Credentials {
	return credentials.NewCredentials(NewTrustedProfileProviderServiceIdWithTrustedProfileName(config, authEndPoint, trustedProfileName, iamAccountId, serviceIdApiKey, serviceInstanceID))
}
