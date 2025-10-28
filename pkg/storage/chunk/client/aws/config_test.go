package aws

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCredentialsFromURL(t *testing.T) {
	tests := []struct {
		name           string
		urlStr         string
		expectedKey    string
		expectedSecret string
	}{
		{
			name:           "URL with username and password",
			urlStr:         "s3://mykey:mysecret@us-east-1",
			expectedKey:    "mykey",
			expectedSecret: "mysecret",
		},
		{
			name:           "URL with only username",
			urlStr:         "s3://mykey@us-east-1",
			expectedKey:    "mykey",
			expectedSecret: "",
		},
		{
			name:           "URL without credentials (should not error)",
			urlStr:         "s3://us-east-1",
			expectedKey:    "",
			expectedSecret: "",
		},
		{
			name:           "URL with endpoint without credentials",
			urlStr:         "s3://s3.amazonaws.com",
			expectedKey:    "",
			expectedSecret: "",
		},
		{
			name:           "URL with credentials and bucket",
			urlStr:         "s3://key:secret@us-east-1/bucket",
			expectedKey:    "key",
			expectedSecret: "secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.urlStr)
			require.NoError(t, err)

			key, secret := CredentialsFromURL(u)
			require.Equal(t, tt.expectedKey, key)
			require.Equal(t, tt.expectedSecret, secret)
		})
	}
}
