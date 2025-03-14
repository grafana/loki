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
)

// NewTrustedProfileProviderWithCR constructor of the IBM IAM provider that uses IAM trusted-profile
// details passed
// Returns: New TrustedProfileProvider (AWS type)
func NewTrustedProfileProviderCR(config *aws.Config, authEndPoint string, trustedProfileID string, crTokenFilePath string, serviceInstanceID string) *TrustedProfileProvider {
	// Resource type ResourceComputeResource is passed to identify that this is a CR-token based
	// resource.
	return NewTrustedProfileProvider(TrustedProfileProviderName, config, authEndPoint, trustedProfileID, crTokenFilePath, serviceInstanceID, string(ResourceComputeResource))
}

// NewTrustedProfileCredentials constructor for IBM IAM that uses IAM trusted-profile
// credentials passed
// Returns: credentials.NewCredentials(newTrustedProfileProvider()) (AWS type)
func NewTrustedProfileCredentialsCR(config *aws.Config, authEndPoint string, trustedProfileID string, crTokenFilePath string, serviceInstanceID string) *credentials.Credentials {
	return credentials.NewCredentials(NewTrustedProfileProviderCR(config, authEndPoint, trustedProfileID, crTokenFilePath, serviceInstanceID))
}
