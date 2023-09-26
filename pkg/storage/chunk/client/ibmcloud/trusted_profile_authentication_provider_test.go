package ibmcloud

import (
	"os"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func Test_TrustedProfileProvider(t *testing.T) {
	tests := []struct {
		name,
		trustedProfileProviderName,
		authEndpoint,
		trustedProfileName,
		trustedProfileID,
		crTokenFilePath string
		token   string
		isValid bool
		wantErr error
	}{
		{
			"valid inputs",
			trustedProfileProviderName,
			"",
			"test-trusted-profile",
			"test-trusted-profile-id",
			"",
			"test-token",
			true,
			nil,
		},
		{
			"empty CR token file path",
			trustedProfileProviderName,
			"",
			"test-trusted-profile",
			"test-trusted-profile-id",
			"",
			"",
			false,
			errors.New("crTokenFilePathEmpty: must supply cr token file path"),
		},
		{
			"empty profileName and profileID",
			trustedProfileProviderName,
			"",
			"",
			"",
			"",
			"",
			false,
			errors.New("trustedProfileNotFound: either Trusted profile name or id must be provided"),
		},
	}

	for _, tt := range tests {

		mockAuthServer := mockAuthServer(tt.token, tokenType)
		defer mockAuthServer.Close()

		if tt.isValid {
			file, err := createTempFile("crtoken", "test cr token")
			require.NoError(t, err)
			defer os.Remove(file.Name())
			tt.crTokenFilePath = file.Name()
		}

		prov := NewTrustedProfileProvider(tt.trustedProfileProviderName, tt.trustedProfileName, tt.trustedProfileID,
			tt.crTokenFilePath, mockAuthServer.URL)

		if !tt.isValid {
			require.Equal(t, tt.crTokenFilePath, "", "cr token filepath did not match")
			require.Equal(t, tt.wantErr.Error(), prov.ErrorStatus.Error())
		} else {
			require.Equal(t, tt.trustedProfileName, prov.authenticator.IAMProfileName, "trusted profile name did not match")
			require.Equal(t, tt.trustedProfileID, prov.authenticator.IAMProfileID, "trusted profile ID did not match")
			require.Equal(t, mockAuthServer.URL, prov.authenticator.URL, "auth endpoint did not match")
			require.Equal(t, tt.crTokenFilePath, prov.authenticator.CRTokenFilename, "cr token filepath did not match")
			require.Equal(t, tt.trustedProfileProviderName, prov.providerName, "provider name did not match")
			require.Equal(t, "oauth", prov.providerType)
		}

		isValid := prov.IsValid()
		require.Equal(t, tt.isValid, isValid)

		isExpired := prov.IsExpired()
		require.Equal(t, true, isExpired)

		cred, err := prov.Retrieve()
		if tt.wantErr != nil {
			require.Equal(t, tt.wantErr.Error(), err.Error())

			continue
		}

		require.Equal(t, tt.token, cred.AccessToken)
		require.Equal(t, tokenType, cred.TokenType)
	}
}

func createTempFile(_, _ string) (*os.File, error) {
	file, err := os.CreateTemp(os.TempDir(), "crtoken")
	if err != nil {
		return nil, err
	}

	defer file.Close()
	_, err = file.Write([]byte("test cr token"))

	return file, err
}
