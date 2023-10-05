package ibmiam

import (
	"os"

	"github.com/IBM/ibm-cos-sdk-go/aws"
	"github.com/IBM/ibm-cos-sdk-go/aws/awserr"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials"
	"github.com/IBM/ibm-cos-sdk-go/internal/shareddefaults"
)

const (
	// SharedConfProviderName of the IBM IAM provider that loads IAM credentials
	// from shared config
	SharedConfProviderName = "SharedConfigProviderIBM"

	// Profile prefix
	profilePrefix = "profile "
)

// NewSharedConfigProvider constructor of the IBM IAM provider that loads IAM Credentials
// from shared config
// Parameters:
//		AWS Config
// 		Profile filename
//		Profile name
// Returns:
// 		Common Ini Provider with values
func NewSharedConfigProvider(config *aws.Config, filename, profilename string) *Provider {

	// Sets the file name from possible locations
	//	- AWS_CONFIG_FILE environment variable
	// Error if the filename is missing
	if filename == "" {
		filename = os.Getenv("AWS_CONFIG_FILE")
		if filename == "" {
			// BUG?
			home := shareddefaults.UserHomeDir()
			if home == "" {
				e := awserr.New("SharedCredentialsHomeNotFound", "Shared Credentials Home folder not found", nil)
				logFromConfigHelper(config, "<DEBUG>", "<IBM IAM PROVIDER BUILD>", SharedConfProviderName, e)
				return &Provider{
					providerName: SharedConfProviderName,
					ErrorStatus:  e,
				}
			}
			filename = shareddefaults.SharedConfigFilename()
		}
	}

	// Sets the profile name
	// Otherwise sets the prefix with profile name passed in
	if profilename == "" {
		profilename = os.Getenv("AWS_PROFILE")
		if profilename == "" {
			profilename = defaultProfile
		} else {
			profilename = profilePrefix + profilename
		}
	} else {
		profilename = profilePrefix + profilename
	}

	return commonIniProvider(SharedConfProviderName, config, filename, profilename)
}

// NewConfigCredentials Constructor
func NewConfigCredentials(config *aws.Config, filename, profilename string) *credentials.Credentials {
	return credentials.NewCredentials(NewSharedConfigProvider(config, filename, profilename))
}
