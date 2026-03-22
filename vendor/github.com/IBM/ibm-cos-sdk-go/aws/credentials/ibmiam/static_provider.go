package ibmiam

import (
	"github.com/IBM/ibm-cos-sdk-go/aws"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials"
)

// StaticProviderName name of the IBM IAM provider that uses IAM details passed directly
const StaticProviderName = "StaticProviderIBM"

// NewStaticProvider constructor of the IBM IAM provider that uses IAM details passed directly
// Returns: New Provider (AWS type)
func NewStaticProvider(config *aws.Config, authEndPoint, apiKey, serviceInstanceID string) *Provider {
	return NewProvider(StaticProviderName, config, apiKey, authEndPoint, serviceInstanceID, nil)
}

// NewStaticCredentials constructor for IBM IAM that uses IAM credentials passed in
// Returns: credentials.NewCredentials(newStaticProvider()) (AWS type)
func NewStaticCredentials(config *aws.Config, authEndPoint, apiKey, serviceInstanceID string) *credentials.Credentials {
	return credentials.NewCredentials(NewStaticProvider(config, authEndPoint, apiKey, serviceInstanceID))
}
