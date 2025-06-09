package bucket

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/storage/bucket/s3"
)

func TestSSEBucketClient_Upload_ShouldInjectCustomSSEConfig(t *testing.T) {
	tests := map[string]struct {
		withExpectedErrs bool
	}{
		"default client": {
			withExpectedErrs: false,
		},
		"client with expected errors": {
			withExpectedErrs: true,
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			const (
				kmsKeyID             = "ABC"
				kmsEncryptionContext = "{\"department\":\"10103.0\"}"
			)

			var req *http.Request

			// Start a fake HTTP server which simulate S3.
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Keep track of the received request.
				req = r

				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			s3Cfg := s3.Config{
				Endpoint:        srv.Listener.Addr().String(),
				Region:          "test",
				BucketName:      "test-bucket",
				SecretAccessKey: flagext.SecretWithValue("test"),
				AccessKeyID:     "test",
				Insecure:        true,
			}

			s3Client, err := s3.NewBucketClient(s3Cfg, "test", log.NewNopLogger(), nil)
			require.NoError(t, err)

			// Configure the config provider with NO KMS key ID.
			cfgProvider := &mockTenantConfigProvider{}

			var sseBkt objstore.Bucket
			if testData.withExpectedErrs {
				sseBkt = NewSSEBucketClient("user-1", s3Client, cfgProvider).WithExpectedErrs(s3Client.IsObjNotFoundErr)
			} else {
				sseBkt = NewSSEBucketClient("user-1", s3Client, cfgProvider)
			}

			err = sseBkt.Upload(context.Background(), "test", strings.NewReader("test"))
			require.NoError(t, err)

			// Ensure NO KMS header has been injected.
			assert.Equal(t, "", req.Header.Get("x-amz-server-side-encryption"))
			assert.Equal(t, "", req.Header.Get("x-amz-server-side-encryption-aws-kms-key-id"))
			assert.Equal(t, "", req.Header.Get("x-amz-server-side-encryption-context"))

			// Configure the config provider with a KMS key ID and without encryption context.
			cfgProvider.s3SseType = s3.SSEKMS
			cfgProvider.s3KmsKeyID = kmsKeyID

			err = sseBkt.Upload(context.Background(), "test", strings.NewReader("test"))
			require.NoError(t, err)

			// Ensure the KMS header has been injected.
			assert.Equal(t, "aws:kms", req.Header.Get("x-amz-server-side-encryption"))
			assert.Equal(t, kmsKeyID, req.Header.Get("x-amz-server-side-encryption-aws-kms-key-id"))
			assert.Equal(t, "", req.Header.Get("x-amz-server-side-encryption-context"))

			// Configure the config provider with a KMS key ID and encryption context.
			cfgProvider.s3SseType = s3.SSEKMS
			cfgProvider.s3KmsKeyID = kmsKeyID
			cfgProvider.s3KmsEncryptionContext = kmsEncryptionContext

			err = sseBkt.Upload(context.Background(), "test", strings.NewReader("test"))
			require.NoError(t, err)

			// Ensure the KMS header has been injected.
			assert.Equal(t, "aws:kms", req.Header.Get("x-amz-server-side-encryption"))
			assert.Equal(t, kmsKeyID, req.Header.Get("x-amz-server-side-encryption-aws-kms-key-id"))
			assert.Equal(t, base64.StdEncoding.EncodeToString([]byte(kmsEncryptionContext)), req.Header.Get("x-amz-server-side-encryption-context"))
		})
	}
}

type mockTenantConfigProvider struct {
	s3SseType              string
	s3KmsKeyID             string
	s3KmsEncryptionContext string
}

func (m *mockTenantConfigProvider) S3SSEType(_ string) string {
	return m.s3SseType
}

func (m *mockTenantConfigProvider) S3SSEKMSKeyID(_ string) string {
	return m.s3KmsKeyID
}

func (m *mockTenantConfigProvider) S3SSEKMSEncryptionContext(_ string) string {
	return m.s3KmsEncryptionContext
}
