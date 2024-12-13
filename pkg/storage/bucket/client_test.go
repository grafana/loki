package bucket

import (
	"context"
	"testing"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v2"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	configWithS3Backend = `
s3:
  endpoint:          localhost
  bucket_name:       test
  access_key_id:     xxx
  secret_access_key: yyy
  insecure:          true
`

	configWithGCSBackend = `
gcs:
  bucket_name:     test
  service_account: |-
    {
      "type": "service_account",
      "project_id": "id",
      "private_key_id": "id",
      "private_key": "-----BEGIN PRIVATE KEY-----\nSOMETHING\n-----END PRIVATE KEY-----\n",
      "client_email": "test@test.com",
      "client_id": "12345",
      "auth_uri": "https://accounts.google.com/o/oauth2/auth",
      "token_uri": "https://oauth2.googleapis.com/token",
      "auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
      "client_x509_cert_url": "https://www.googleapis.com/robot/v1/metadata/x509/test%40test.com"
    }
`
)

func TestNewClient(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		backend     string
		config      string
		expectedErr error
	}{
		"should create an S3 bucket": {
			backend:     "s3",
			config:      configWithS3Backend,
			expectedErr: nil,
		},
		"should create a GCS bucket": {
			backend:     "gcs",
			config:      configWithGCSBackend,
			expectedErr: nil,
		},
		"should return error on unknown backend": {
			backend:     "unknown",
			config:      "",
			expectedErr: ErrUnsupportedStorageBackend,
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			// Load config
			cfg := Config{}
			flagext.DefaultValues(&cfg)

			err := yaml.Unmarshal([]byte(testData.config), &cfg)
			require.NoError(t, err)

			// Instance a new bucket client from the config
			bucketClient, err := NewClient(context.Background(), testData.backend, cfg, "test", util_log.Logger)
			require.Equal(t, testData.expectedErr, err)

			if testData.expectedErr == nil {
				require.NotNil(t, bucketClient)
				bucketClient.Close()
			} else {
				assert.Equal(t, nil, bucketClient)
			}
		})
	}
}
