package aws

import (
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	objstores3 "github.com/thanos-io/objstore/providers/s3"

	buckets3 "github.com/grafana/loki/v4/pkg/storage/bucket/s3"
)

func TestNewSSEParsedConfig(t *testing.T) {
	kmsKeyID := "test"
	kmsEncryptionContext := `{"a": "bc", "b": "cd"}`
	// compact form of kmsEncryptionContext
	parsedKMSEncryptionContext := "eyJhIjoiYmMiLCJiIjoiY2QifQ=="

	tests := []struct {
		name        string
		params      buckets3.SSEConfig
		expected    *SSEParsedConfig
		expectedErr error
		errContains string
	}{
		{
			name: "Test SSE encryption with SSES3 type",
			params: buckets3.SSEConfig{
				Type: objstores3.SSES3,
			},
			expected: &SSEParsedConfig{
				ServerSideEncryption: sseS3Type,
			},
		},
		{
			name: "Test SSE encryption with SSEKMS type without context",
			params: buckets3.SSEConfig{
				Type:     objstores3.SSEKMS,
				KMSKeyID: kmsKeyID,
			},
			expected: &SSEParsedConfig{
				ServerSideEncryption: sseKMSType,
				KMSKeyID:             &kmsKeyID,
			},
		},
		{
			name: "Test SSE encryption with SSEKMS type with context",
			params: buckets3.SSEConfig{
				Type:                 objstores3.SSEKMS,
				KMSKeyID:             kmsKeyID,
				KMSEncryptionContext: kmsEncryptionContext,
			},
			expected: &SSEParsedConfig{
				ServerSideEncryption: sseKMSType,
				KMSKeyID:             &kmsKeyID,
				KMSEncryptionContext: &parsedKMSEncryptionContext,
			},
		},
		{
			name: "Test invalid SSE type",
			params: buckets3.SSEConfig{
				Type: "invalid",
			},
			expectedErr: errors.New("SSE type is empty or invalid"),
		},
		{
			name: "Test SSE encryption with SSEKMS type without KMS Key ID",
			params: buckets3.SSEConfig{
				Type:     objstores3.SSEKMS,
				KMSKeyID: "",
			},
			expectedErr: errors.New("KMS key id must be passed when SSE-KMS encryption is selected"),
		},
		{
			name: "Test SSE with invalid KMS encryption context JSON",
			params: buckets3.SSEConfig{
				Type:                 objstores3.SSEKMS,
				KMSKeyID:             kmsKeyID,
				KMSEncryptionContext: `INVALID_JSON`,
			},
			// Go 1.26 and 1.27 disagree on the encoding/json type name in this error.
			expectedErr: errors.New("failed to parse KMS encryption context: failed to marshal KMS encryption context"),
			errContains: "invalid character 'I'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := NewSSEParsedConfig(tt.params)
			if tt.expectedErr != nil {
				require.ErrorContains(t, err, tt.expectedErr.Error())
			}
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
			}
			require.Equal(t, tt.expected, result)
		})
	}
}
