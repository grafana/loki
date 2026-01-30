package aws

import (
	"crypto/md5"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"

	s3 "github.com/grafana/loki/v3/pkg/storage/bucket/s3"
)

func TestNewSSEParsedConfig(t *testing.T) {
	kmsKeyID := "test"
	kmsEncryptionContext := `{"a": "bc", "b": "cd"}`
	// compact form of kmsEncryptionContext
	parsedKMSEncryptionContext := "eyJhIjoiYmMiLCJiIjoiY2QifQ=="

	sseCKey := strings.Repeat("a", 32)
	sseCKeyMD5 := md5.Sum([]byte(sseCKey))
	sseCKeyMD5B64 := base64.StdEncoding.EncodeToString(sseCKeyMD5[:])
	sseCKeyHashB64 := base64.StdEncoding.EncodeToString([]byte(sseCKey))

	tests := []struct {
		name        string
		params      s3.SSEConfig
		expected    *SSEParsedConfig
		expectedErr error
	}{
		{
			name: "Test SSE with SSE S3 type",
			params: s3.SSEConfig{
				Type: s3.SSES3,
			},
			expected: &SSEParsedConfig{
				SSEType:              s3.SSES3,
				ServerSideEncryption: sseS3EncryptionType,
			},
		},
		{
			name: "Test SSE with SSE KMS type without context",
			params: s3.SSEConfig{
				Type:     s3.SSEKMS,
				KMSKeyID: kmsKeyID,
			},
			expected: &SSEParsedConfig{
				SSEType:              s3.SSEKMS,
				ServerSideEncryption: sseKMSEncryptionType,
				KMSKeyID:             &kmsKeyID,
			},
		},
		{
			name: "Test SSE with SSE KMS type with context",
			params: s3.SSEConfig{
				Type:                 s3.SSEKMS,
				KMSKeyID:             kmsKeyID,
				KMSEncryptionContext: kmsEncryptionContext,
			},
			expected: &SSEParsedConfig{
				SSEType:              s3.SSEKMS,
				ServerSideEncryption: sseKMSEncryptionType,
				KMSKeyID:             &kmsKeyID,
				KMSEncryptionContext: &parsedKMSEncryptionContext,
			},
		},
		{
			name: "Test invalid SSE type",
			params: s3.SSEConfig{
				Type: "invalid",
			},
			expectedErr: errors.New("SSE type is empty or invalid"),
		},
		{
			name: "Test SSE with SSE KMS type without KMS Key ID",
			params: s3.SSEConfig{
				Type:     s3.SSEKMS,
				KMSKeyID: "",
			},
			expectedErr: errors.New("KMS key id must be passed when SSE-KMS encryption is selected"),
		},
		{
			name: "Test SSE with SSE KMS type with invalid KMS encryption context JSON",
			params: s3.SSEConfig{
				Type:                 s3.SSEKMS,
				KMSKeyID:             kmsKeyID,
				KMSEncryptionContext: `INVALID_JSON`,
			},
			expectedErr: errors.New("failed to parse KMS encryption context: failed to marshal KMS encryption context: json: error calling MarshalJSON for type json.RawMessage: invalid character 'I' looking for beginning of value"),
		},
		{
			name: "Test SSE with SSE C type",
			params: s3.SSEConfig{
				Type:                  s3.SSEC,
				CustomerEncryptionKey: sseCKey,
			},
			expected: &SSEParsedConfig{
				SSEType:                     s3.SSEC,
				ServerSideEncryption:        sseCEncryptionType,
				CustomerEncryptionKeyB64:    &sseCKeyHashB64,
				CustomerEncryptionKeyMD5B64: &sseCKeyMD5B64,
			},
		},
		{
			name: "Test SSE with SSE C type with invalid key length",
			params: s3.SSEConfig{
				Type:                  s3.SSEC,
				CustomerEncryptionKey: "short_key",
			},
			expectedErr: errors.New("customer encryption key must be 32 bytes long"),
		},
		{
			name: "Test SSE with SSE C type with missing key",
			params: s3.SSEConfig{
				Type: s3.SSEC,
			},
			expectedErr: errors.New("customer encryption key must be passed when SSE-C encryption is selected"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := NewSSEParsedConfig(tt.params)
			if tt.expectedErr != nil {
				assert.Equal(t, tt.expectedErr.Error(), err.Error())
			}
			assert.Equal(t, tt.expected, result)
		})
	}
}
