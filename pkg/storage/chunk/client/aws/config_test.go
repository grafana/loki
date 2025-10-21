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
	}{
		{
			name:            "URL with username and password",
			urlStr:          "s3://mykey:mysecret@us-east-1",
			expectedKey:     "mykey",
			expectedSecret:  "mysecret",
			expectedSession: "",
		},
		{
			name:            "URL with only username",
			urlStr:          "s3://mykey@us-east-1",
			expectedKey:     "mykey",
			expectedSecret:  "",
			expectedSession: "",
		},
		{
			name:            "URL without credentials (should not error)",
			urlStr:          "s3://us-east-1",
			expectedKey:     "",
			expectedSecret:  "",
			expectedSession: "",
		},
		{
			name:            "URL with endpoint without credentials",
			urlStr:          "s3://s3.amazonaws.com",
			expectedKey:     "",
			expectedSecret:  "",
			expectedSession: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.urlStr)
			require.NoError(t, err)

			key, secret, session := CredentialsFromURL(u)
			require.Equal(t, tt.expectedKey, key)
			require.Equal(t, tt.expectedSecret, secret)
			require.Equal(t, tt.expectedSession, session)
		})
	}
}
