package ibmiam

import (
	"os"

	"github.com/IBM/ibm-cos-sdk-go/aws/awserr"
)

// Helper function to check whether both api-key and trusted profile credentials are set
// in environment variables.
//
// Returns:
//
//	Error if both api-key and trusted profile credentials are set as environmental variables.

func CheckForConflictingIamCredentials() error {
	apiKey := os.Getenv("IBM_API_KEY_ID")
	trustedProfileID := os.Getenv("TRUSTED_PROFILE_ID")
	trustedProfileName := os.Getenv("TRUSTED_PROFILE_NAME")
	crTokenFilePath := os.Getenv("CR_TOKEN_FILE_PATH")
	serviceIdApiKey := os.Getenv("IBM_SERVICE_ID_API_KEY")

	if apiKey != "" && (trustedProfileID != "" || trustedProfileName != "") {
		return awserr.New("InvalidCredentials",
			`only one of ApiKey or TrustedProfile credentials should be set, not both.`,
			nil)
	}
	if trustedProfileName != "" && trustedProfileID != "" {
		return awserr.New("InvalidCredentials",
			`only one of TrustedProfileName or TrustedProfileID should be set, not both.`,
			nil)
	}
	if crTokenFilePath != "" && serviceIdApiKey != "" {
		return awserr.New("InvalidCredentials",
			`only one of ServiceIdApiKey or CrTokenFilePath should be set, not both.`,
			nil)
	}

	return nil
}
