// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/cortexproject/cortex/blob/master/pkg/storage/bucket/s3/config_test.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Cortex Authors.

package s3

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"testing"

	s3_service "github.com/aws/aws-sdk-go/service/s3"
	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestSSEConfig_Validate(t *testing.T) {
	tests := map[string]struct {
		setup    func() *SSEConfig
		expected error
	}{
		"should pass with default config": {
			setup: func() *SSEConfig {
				cfg := &SSEConfig{}
				flagext.DefaultValues(cfg)

				return cfg
			},
		},
		"should fail on invalid SSE type": {
			setup: func() *SSEConfig {
				return &SSEConfig{
					Type: "unknown",
				}
			},
			expected: errUnsupportedSSEType,
		},
		"should fail on invalid SSE KMS encryption context": {
			setup: func() *SSEConfig {
				return &SSEConfig{
					Type:                 SSEKMS,
					KMSEncryptionContext: "!{}!",
				}
			},
			expected: errInvalidSSEContext,
		},
		"should pass on valid SSE KMS encryption context": {
			setup: func() *SSEConfig {
				return &SSEConfig{
					Type:                 SSEKMS,
					KMSEncryptionContext: `{"department": "10103.0"}`,
				}
			},
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			assert.Equal(t, testData.expected, testData.setup().Validate())
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := map[string]struct {
		setup    func() *Config
		expected error
	}{
		"should pass with default config": {
			setup: func() *Config {
				sseCfg := &SSEConfig{}
				flagext.DefaultValues(sseCfg)
				cfg := &Config{
					Endpoint:     "s3.eu-central-1.amazonaws.com",
					BucketName:   "mimir-block",
					SSE:          *sseCfg,
					StorageClass: s3_service.StorageClassStandard,
				}
				return cfg
			},
		},
		"should fail if invalid storage class is set": {
			setup: func() *Config {
				return &Config{
					StorageClass: "foo",
				}
			},
			expected: errUnsupportedStorageClass,
		},
		"should fail on invalid endpoint prefix": {
			setup: func() *Config {
				return &Config{
					Endpoint:     "mimir-blocks.s3.eu-central-1.amazonaws.com",
					BucketName:   "mimir-blocks",
					StorageClass: s3_service.StorageClassStandard,
				}
			},
			expected: errInvalidEndpointPrefix,
		},
		"should pass if native_aws_auth_enabled is set": {
			setup: func() *Config {
				return &Config{
					NativeAWSAuthEnabled: true,
				}
			},
		},
		"should pass with using sts endpoint": {
			setup: func() *Config {
				sseCfg := &SSEConfig{}
				flagext.DefaultValues(sseCfg)
				cfg := &Config{
					BucketName:   "mimir-block",
					SSE:          *sseCfg,
					StorageClass: s3_service.StorageClassStandard,
					STSEndpoint:  "https://sts.eu-central-1.amazonaws.com",
				}
				return cfg
			},
		},
		"should not pass with using sts endpoint as its using an invalid url": {
			setup: func() *Config {
				sseCfg := &SSEConfig{}
				flagext.DefaultValues(sseCfg)
				cfg := &Config{
					BucketName:   "mimir-block",
					SSE:          *sseCfg,
					StorageClass: s3_service.StorageClassStandard,
					STSEndpoint:  "sts.eu-central-1.amazonaws.com",
				}
				return cfg
			},
			expected: errInvalidSTSEndpoint,
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			assert.Equal(t, testData.expected, testData.setup().Validate())
		})
	}
}

func TestSSEConfig_BuildMinioConfig(t *testing.T) {
	tests := map[string]struct {
		cfg             *SSEConfig
		expectedType    string
		expectedKeyID   string
		expectedContext string
	}{
		"SSE KMS without encryption context": {
			cfg: &SSEConfig{
				Type:     SSEKMS,
				KMSKeyID: "test-key",
			},
			expectedType:    "aws:kms",
			expectedKeyID:   "test-key",
			expectedContext: "",
		},
		"SSE KMS with encryption context": {
			cfg: &SSEConfig{
				Type:                 SSEKMS,
				KMSKeyID:             "test-key",
				KMSEncryptionContext: "{\"department\":\"10103.0\"}",
			},
			expectedType:    "aws:kms",
			expectedKeyID:   "test-key",
			expectedContext: "{\"department\":\"10103.0\"}",
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			sse, err := testData.cfg.BuildMinioConfig()
			require.NoError(t, err)

			headers := http.Header{}
			sse.Marshal(headers)

			assert.Equal(t, testData.expectedType, headers.Get("x-amz-server-side-encryption"))
			assert.Equal(t, testData.expectedKeyID, headers.Get("x-amz-server-side-encryption-aws-kms-key-id"))
			assert.Equal(t, base64.StdEncoding.EncodeToString([]byte(testData.expectedContext)), headers.Get("x-amz-server-side-encryption-context"))
		})
	}
}

func TestParseKMSEncryptionContext(t *testing.T) {
	actual, err := parseKMSEncryptionContext("")
	assert.NoError(t, err)
	assert.Equal(t, map[string]string(nil), actual)

	expected := map[string]string{
		"department": "10103.0",
	}
	actual, err = parseKMSEncryptionContext(`{"department": "10103.0"}`)
	assert.NoError(t, err)
	assert.Equal(t, expected, actual)
}

func TestConfigParsesCredentialsInlineWithSessionToken(t *testing.T) {
	var cfg = Config{}
	yamlCfg := `
access_key_id: access key id
secret_access_key: secret access key
session_token: session token
`
	err := yaml.Unmarshal([]byte(yamlCfg), &cfg)
	require.NoError(t, err)

	require.Equal(t, cfg.AccessKeyID, "access key id")
	require.Equal(t, cfg.SecretAccessKey.String(), "secret access key")
	require.Equal(t, cfg.SessionToken.String(), "session token")
}

func TestConfigRedactsCredentials(t *testing.T) {
	cfg := Config{
		AccessKeyID:     "access key id",
		SecretAccessKey: flagext.SecretWithValue("secret access key"),
		SessionToken:    flagext.SecretWithValue("session token"),
	}

	output, err := yaml.Marshal(cfg)
	require.NoError(t, err)

	require.True(t, bytes.Contains(output, []byte("access key id")))
	require.False(t, bytes.Contains(output, []byte("secret access id")))
	require.False(t, bytes.Contains(output, []byte("session token")))
}
