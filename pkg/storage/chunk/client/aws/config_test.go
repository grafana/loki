package aws

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCredentialsFromURL(t *testing.T) {
	tests := []struct {
		name            string
		urlStr          string
		expectedKey     string
		expectedSecret  string
		expectedSession string
		expectError     bool
	}{
		{
			name:            "URL with username and password",
			urlStr:          "s3://mykey:mysecret@us-east-1",
			expectedKey:     "mykey",
			expectedSecret:  "mysecret",
			expectedSession: "",
			expectError:     false,
		},
		{
			name:            "URL with only username",
			urlStr:          "s3://mykey@us-east-1",
			expectedKey:     "mykey",
			expectedSecret:  "",
			expectedSession: "",
			expectError:     false,
		},
		{
			name:            "URL without credentials (should not error)",
			urlStr:          "s3://us-east-1",
			expectedKey:     "",
			expectedSecret:  "",
			expectedSession: "",
			expectError:     false,
		},
		{
			name:            "URL with endpoint without credentials",
			urlStr:          "s3://s3.amazonaws.com",
			expectedKey:     "",
			expectedSecret:  "",
			expectedSession: "",
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.urlStr)
			require.NoError(t, err)

			key, secret, session, err := CredentialsFromURL(u)
			
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedKey, key)
				require.Equal(t, tt.expectedSecret, secret)
				require.Equal(t, tt.expectedSession, session)
			}
		})
	}
}

