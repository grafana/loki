package ibmiam

import (
	"os"

	"github.com/IBM/ibm-cos-sdk-go/aws/awserr"
)

// Helper function to check whether both api-key and trusted-profile-id are set
// in environment variables.
//
// Returns:
//
//	Error if both apiKey and trustedProfileID are set, nil if only either of them is set.
func CheckForConflictingIamCredentials() error {
	apiKey := os.Getenv("IBM_API_KEY_ID")
	trustedProfileID := os.Getenv("TRUSTED_PROFILE_ID")

	if apiKey != "" && trustedProfileID != "" {
		return awserr.New("InvalidCredentials",
			`only one of ApiKey or TrustedProfileID should be set, not both`,
			nil)
	}
	return nil
}
