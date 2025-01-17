package ibmiam

import (
	"os"

	"github.com/IBM/ibm-cos-sdk-go/aws"
	"github.com/IBM/ibm-cos-sdk-go/aws/awserr"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials"
	"github.com/IBM/ibm-cos-sdk-go/internal/shareddefaults"
)

const (
	// SharedCredsProviderName name of the IBM IAM provider that loads IAM credentials
	// from shared credentials file
	SharedCredsProviderName = "SharedCredentialsProviderIBM"
)

// NewSharedCredentialsProvider constructor of the IBM IAM provider that loads
// IAM credentials from shared credentials file
// Parameters:
//
//	AWS Config
//	Profile filename
//	Profile prefix
//
// Returns:
//
//	Common initial provider with config file/profile
func NewSharedCredentialsProvider(config *aws.Config, filename, profilename string) *Provider {

	// Sets the file name from possible locations
	//	- AWS_SHARED_CREDENTIALS_FILE environment variable
	// Error if the filename is missing
	if filename == "" {
		filename = os.Getenv("AWS_SHARED_CREDENTIALS_FILE")
		if filename == "" {
			// BUG where will we use home?
			home := shareddefaults.UserHomeDir()
			if home == "" {
				e := awserr.New("SharedCredentialsHomeNotFound", "Shared Credentials Home folder not found", nil)
				logFromConfigHelper(config, "<DEBUG>", "<IBM IAM PROVIDER BUILD>", SharedCredsProviderName, e)
				return &Provider{
					providerName: SharedCredsProviderName,
					ErrorStatus:  e,
				}
			}
			filename = shareddefaults.SharedCredentialsFilename()
		}
	}

	// Sets the profile name from AWS_PROFILE environment variable
	// Otherwise sets the profile name with defaultProfile passed in
	if profilename == "" {
		profilename = os.Getenv("AWS_PROFILE")
		if profilename == "" {
			profilename = defaultProfile
		}
	}

	return commonIniProvider(SharedCredsProviderName, config, filename, profilename)
}

// NewSharedCredentials constructor for IBM IAM that uses IAM credentials passed in
// Returns:
//
//	credentials.NewCredentials(newSharedCredentialsProvider()) (AWS type)
func NewSharedCredentials(config *aws.Config, filename, profilename string) *credentials.Credentials {
	return credentials.NewCredentials(NewSharedCredentialsProvider(config, filename, profilename))
}
