package aws

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	bucket_s3 "github.com/grafana/loki/v3/pkg/storage/bucket/s3"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

type RoundTripperFunc func(*http.Request) (*http.Response, error)

func (f RoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestIsObjectNotFoundErr(t *testing.T) {
	tests := []struct {
		err      error
		expected bool
		name     string
	}{
		{
			name:     "no such key error is recognized as object not found",
			err:      &types.NoSuchKey{},
			expected: true,
		},
		{
			name:     "NotFound code is recognized as object not found",
			err:      &types.NotFound{},
			expected: true,
		},
		{
			name:     "Nil error isnt recognized as object not found",
			err:      nil,
			expected: false,
		},
		{
			name:     "Other error isnt recognized as object not found",
			err:      &types.NoSuchBucket{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewS3ObjectClient(S3Config{BucketNames: "mybucket"}, hedging.Config{})
			require.NoError(t, err)

			require.Equal(t, tt.expected, client.IsObjectNotFoundErr(tt.err))
		})
	}
}

func TestIsRetryableErr(t *testing.T) {
	tests := []struct {
		err      error
		expected bool
		name     string
	}{
		{
			name:     "IsStorageThrottledErr - Too Many Requests",
			err:      &smithy.GenericAPIError{Code: "TooManyRequestsException"},
			expected: true,
		},
		{
			name:     "IsStorageThrottledErr - 503",
			err:      &smithy.GenericAPIError{Code: "SlowDown"},
			expected: true,
		},
		{
			name:     "IsStorageThrottledErr - 5xx",
			err:      &smithy.GenericAPIError{Code: "NotImplemented"},
			expected: true,
		},
		{
			name:     "IsStorageTimeoutErr - Request Timeout",
			err:      &smithy.GenericAPIError{Code: "RequestTimeout"},
			expected: true,
		},
		{
			name:     "IsStorageTimeoutErr - EOF",
			err:      io.EOF,
			expected: true,
		},
		{
			name:     "IsStorageTimeoutErr - Connection Reset",
			err:      syscall.ECONNRESET,
			expected: true,
		},
		{
			name:     "IsStorageTimeoutErr - Closed",
			err:      net.ErrClosed,
			expected: false,
		},
		{
			name:     "IsStorageTimeoutErr - Connection Refused",
			err:      syscall.ECONNREFUSED,
			expected: false,
		},
		{
			name:     "IsStorageTimeoutErr - Context Deadline Exceeded",
			err:      context.DeadlineExceeded,
			expected: false,
		},
		{
			name:     "IsStorageTimeoutErr - Context Canceled",
			err:      context.Canceled,
			expected: false,
		},
		{
			name:     "Not a retryable error",
			err:      syscall.EINVAL,
			expected: false,
		},
		{
			name:     "Not found 404",
			err:      &smithy.GenericAPIError{Code: "NotFound"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewS3ObjectClient(S3Config{BucketNames: "mybucket"}, hedging.Config{})
			require.NoError(t, err)

			require.Equal(t, tt.expected, client.IsRetryableErr(tt.err))
		})
	}
}

func TestRequestMiddleware(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, r.Header.Get("echo-me"))
	}))
	defer ts.Close()

	cfg := S3Config{
		Endpoint:         ts.URL,
		BucketNames:      "buck-o",
		S3ForcePathStyle: true,
		Insecure:         true,
		AccessKeyID:      "key",
		SecretAccessKey:  flagext.SecretWithValue("secret"),
	}

	tests := []struct {
		name     string
		fn       InjectRequestMiddleware
		expected string
	}{
		{
			name:     "Test Nil",
			fn:       nil,
			expected: "",
		},
		{
			name: "Test Header Injection",
			fn: func(next http.RoundTripper) http.RoundTripper {
				return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
					req.Header["echo-me"] = []string{"blerg"}
					return next.RoundTrip(req)
				})
			},
			expected: "blerg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg.Inject = tt.fn
			client, err := NewS3ObjectClient(cfg, hedging.Config{})
			require.NoError(t, err)

			readCloser, _, err := client.GetObject(context.Background(), "key")
			require.NoError(t, err)

			buffer := make([]byte, 100)
			_, err = readCloser.Read(buffer)
			if err != io.EOF {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.expected, strings.Trim(string(buffer), "\n\x00"))
		})
	}
}

func TestS3ObjectClient_GetObject_CanceledContext(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, r.Header.Get("echo-me"))
	}))
	defer ts.Close()

	cfg := S3Config{
		Endpoint:         ts.URL,
		BucketNames:      "buck-o",
		S3ForcePathStyle: true,
		Insecure:         true,
		AccessKeyID:      "key",
		SecretAccessKey:  flagext.SecretWithValue("secret"),
	}

	client, err := NewS3ObjectClient(cfg, hedging.Config{})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err = client.GetObject(ctx, "key")
	require.Error(t, err, "GetObject should fail when given a canceled context")
}

func Test_Hedging(t *testing.T) {
	for _, tc := range []struct {
		name          string
		expectedCalls int32
		hedgeAt       time.Duration
		upTo          int
		do            func(c *S3ObjectClient)
	}{
		{
			"delete/put/list are not hedged",
			9,
			20 * time.Nanosecond,
			10,
			func(c *S3ObjectClient) {
				_ = c.DeleteObject(context.Background(), "foo")
				_, _, _ = c.List(context.Background(), "foo", "/")
				_ = c.PutObject(context.Background(), "foo", bytes.NewReader([]byte("bar")))
			},
		},
		{
			"gets are hedged",
			9,
			20 * time.Nanosecond,
			3,
			func(c *S3ObjectClient) {
				_, _, _ = c.GetObject(context.Background(), "foo")
			},
		},
		{
			"gets are not hedged when not configured",
			3,
			0,
			0,
			func(c *S3ObjectClient) {
				_, _, _ = c.GetObject(context.Background(), "foo")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count := atomic.NewInt32(0)

			c, err := NewS3ObjectClient(S3Config{
				AccessKeyID:     "foo",
				SecretAccessKey: flagext.SecretWithValue("bar"),
				BackoffConfig:   backoff.Config{MaxRetries: 1},
				BucketNames:     "foo",
				Inject: func(_ http.RoundTripper) http.RoundTripper {
					return RoundTripperFunc(func(_ *http.Request) (*http.Response, error) {
						count.Inc()
						time.Sleep(200 * time.Millisecond)
						return nil, errors.New("foo")
					})
				},
			}, hedging.Config{
				At:           tc.hedgeAt,
				UpTo:         tc.upTo,
				MaxPerSecond: 1000,
			})
			require.NoError(t, err)
			tc.do(c)
			require.Equal(t, tc.expectedCalls, count.Load())
		})
	}
}

type MockS3Client struct {
	s3.Client
	HeadObjectFunc func(context.Context, *s3.HeadObjectInput) (*s3.HeadObjectOutput, error)
	GetObjectFunc  func(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObjectFunc  func(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

func (m *MockS3Client) HeadObject(ctx context.Context, input *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	return m.HeadObjectFunc(ctx, input)
}

func (m *MockS3Client) GetObject(ctx context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return m.GetObjectFunc(ctx, input)
}

func (m *MockS3Client) PutObject(ctx context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return m.PutObjectFunc(ctx, input)
}

func Test_GetAttributes(t *testing.T) {
	mockS3 := &MockS3Client{
		HeadObjectFunc: func(_ context.Context, _ *s3.HeadObjectInput) (*s3.HeadObjectOutput, error) {
			var size int64 = 128
			return &s3.HeadObjectOutput{ContentLength: &size}, nil
		},
	}

	c, err := NewS3ObjectClient(S3Config{
		AccessKeyID:     "foo",
		SecretAccessKey: flagext.SecretWithValue("bar"),
		BackoffConfig:   backoff.Config{MaxRetries: 3},
		BucketNames:     "foo",
		Inject: func(_ http.RoundTripper) http.RoundTripper {
			return RoundTripperFunc(func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader([]byte("object content"))),
				}, nil
			})
		},
	}, hedging.Config{})
	require.NoError(t, err)
	c.S3 = mockS3

	attrs, err := c.GetAttributes(context.Background(), "abc")
	require.NoError(t, err)
	require.EqualValues(t, 128, attrs.Size)
}

func Test_RetryLogic(t *testing.T) {
	for _, tc := range []struct {
		name       string
		maxRetries int
		exists     bool
		do         func(c *S3ObjectClient) error
	}{
		{
			"get object with retries",
			3,
			true,
			func(c *S3ObjectClient) error {
				_, _, err := c.GetObject(context.Background(), "foo")
				return err
			},
		},
		{
			"object exists with retries",
			3,
			true,
			func(c *S3ObjectClient) error {
				_, err := c.ObjectExists(context.Background(), "foo")
				return err
			},
		},
		{
			"object exists with size with retries",
			3,
			true,
			func(c *S3ObjectClient) error {
				_, err := c.ObjectExists(context.Background(), "foo")
				return err
			},
		},
		{
			"object doesn't exist with retries",
			3,
			false,
			func(c *S3ObjectClient) error {
				exists, err := c.ObjectExists(context.Background(), "foo")
				if err == nil && !exists {
					return &types.NotFound{}
				}
				return err
			},
		},
		{
			"object doesn't exist (with size) with retries",
			3,
			false,
			func(c *S3ObjectClient) error {
				_, err := c.GetAttributes(context.Background(), "foo")
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			callCount := atomic.NewInt32(0)

			mockS3 := &MockS3Client{
				HeadObjectFunc: func(_ context.Context, _ *s3.HeadObjectInput) (*s3.HeadObjectOutput, error) {
					callNum := callCount.Inc()
					if !tc.exists {
						rfIn := &types.NotFound{}
						return nil, rfIn
					}

					// Fail the first set of calls
					if int(callNum) <= tc.maxRetries-1 {
						time.Sleep(200 * time.Millisecond) // Simulate latency
						return nil, errors.New("simulated error on mock call")
					}

					// Succeed on the last call
					return &s3.HeadObjectOutput{}, nil
				},
			}

			c, err := NewS3ObjectClient(S3Config{
				AccessKeyID:     "foo",
				SecretAccessKey: flagext.SecretWithValue("bar"),
				BackoffConfig:   backoff.Config{MaxRetries: tc.maxRetries},
				BucketNames:     "foo",
				Inject: func(_ http.RoundTripper) http.RoundTripper {
					return RoundTripperFunc(func(_ *http.Request) (*http.Response, error) {
						// Increment the call counter
						callNum := callCount.Inc()

						// Fail the first set of calls
						if int(callNum) <= tc.maxRetries-1 {
							time.Sleep(200 * time.Millisecond) // Simulate latency
							return nil, errors.New("simulated error on call")
						}

						// Succeed on the last call
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(bytes.NewReader([]byte("object content"))),
						}, nil
					})
				},
			}, hedging.Config{})
			require.NoError(t, err)
			c.S3 = mockS3
			err = tc.do(c)
			if tc.exists {
				require.NoError(t, err)
				require.Equal(t, tc.maxRetries, int(callCount.Load()))
			} else {
				require.True(t, c.IsObjectNotFoundErr(err))
				require.Equal(t, 1, int(callCount.Load()))
			}
		})
	}
}

func Test_ConfigRedactsCredentials(t *testing.T) {
	underTest := S3Config{
		AccessKeyID:     "access key id",
		SecretAccessKey: flagext.SecretWithValue("secret access key"),
	}

	output, err := yaml.Marshal(underTest)
	require.NoError(t, err)

	require.True(t, bytes.Contains(output, []byte("access key id")))
	require.False(t, bytes.Contains(output, []byte("secret access id")))
}

func Test_ConfigParsesCredentialsInline(t *testing.T) {
	var underTest = S3Config{}
	yamlCfg := `
access_key_id: access key id
secret_access_key: secret access key
`
	err := yaml.Unmarshal([]byte(yamlCfg), &underTest)
	require.NoError(t, err)

	require.Equal(t, underTest.AccessKeyID, "access key id")
	require.Equal(t, underTest.SecretAccessKey.String(), "secret access key")
	require.Equal(t, underTest.SessionToken.String(), "")

}

func Test_ConfigParsesCredentialsInlineWithSessionToken(t *testing.T) {
	var underTest = S3Config{}
	yamlCfg := `
access_key_id: access key id
secret_access_key: secret access key
session_token: session token
`
	err := yaml.Unmarshal([]byte(yamlCfg), &underTest)
	require.NoError(t, err)

	require.Equal(t, underTest.AccessKeyID, "access key id")
	require.Equal(t, underTest.SecretAccessKey.String(), "secret access key")
	require.Equal(t, underTest.SessionToken.String(), "session token")

}

type testCommonPrefixesS3Client struct {
	*s3.Client
}

func (m testCommonPrefixesS3Client) ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	var commonPrefixes []types.CommonPrefix
	commonPrefix := "common-prefix-repeated/"
	for i := 0; i < 2; i++ {
		commonPrefixes = append(commonPrefixes, types.CommonPrefix{Prefix: aws.String(commonPrefix)})
	}
	return &s3.ListObjectsV2Output{CommonPrefixes: commonPrefixes, IsTruncated: aws.Bool(false)}, nil
}

func TestCommonPrefixes(t *testing.T) {
	s3 := S3ObjectClient{S3: testCommonPrefixesS3Client{}, bucketNames: []string{"bucket"}}
	_, CommonPrefixes, err := s3.List(context.Background(), "", "/")
	require.Equal(t, nil, err)
	require.Equal(t, 1, len(CommonPrefixes))
}

func TestS3SSEConfig_GetObject(t *testing.T) {
	// GetObject injects only headers for SSE of type SSE-C

	tests := []struct {
		name            string
		wantSSECHeaders bool
		sseConfig       bucket_s3.SSEConfig
	}{
		{
			name:            "SSE-C configured",
			wantSSECHeaders: true,
			sseConfig: bucket_s3.SSEConfig{
				Type:                  bucket_s3.SSEC,
				CustomerEncryptionKey: strings.Repeat("a", 32),
			},
		},
		{
			name:            "SSE-S3 configured",
			wantSSECHeaders: false,
			sseConfig: bucket_s3.SSEConfig{
				Type: bucket_s3.SSES3,
			},
		},
		{
			name:            "SSE-KMS configured",
			wantSSECHeaders: false,
			sseConfig: bucket_s3.SSEConfig{
				Type:                 bucket_s3.SSEKMS,
				KMSKeyID:             "test",
				KMSEncryptionContext: `{"a": "bc", "b": "cd"}`,
			},
		},
		{
			name:            "No SSE configured",
			wantSSECHeaders: false,
			sseConfig:       bucket_s3.SSEConfig{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockS3 := &MockS3Client{
				GetObjectFunc: func(_ context.Context, params *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
					keyHash := md5.Sum([]byte(tt.sseConfig.CustomerEncryptionKey)[:])
					b64CustomerKey := base64.StdEncoding.EncodeToString([]byte(tt.sseConfig.CustomerEncryptionKey))
					b64CustomerKeyMD5 := base64.StdEncoding.EncodeToString(keyHash[:])

					// check that SSE-C headers are only set when expected
					// SSE-S3/SSE-KMS related headers (like x-amz-server-side-encryption-aws-kms-key-id) are required only when uploading/creating objects, not for retrieving.
					if tt.wantSSECHeaders {
						assert.Equal(t, string(types.ServerSideEncryptionAes256), *params.SSECustomerAlgorithm)
						assert.Equal(t, b64CustomerKey, *params.SSECustomerKey)
						assert.Equal(t, b64CustomerKeyMD5, *params.SSECustomerKeyMD5)
					} else {
						// no headers should be set when not expected
						assert.Empty(t, params.SSECustomerAlgorithm)
						assert.Empty(t, params.SSECustomerKey)
						assert.Empty(t, params.SSECustomerKeyMD5)
					}

					return &s3.GetObjectOutput{
						Body:          io.NopCloser(bytes.NewReader([]byte("object content"))),
						ContentLength: aws.Int64(128),
					}, nil
				},
			}

			cfg := S3Config{
				AccessKeyID:     "foo",
				SecretAccessKey: flagext.SecretWithValue("bar"),
				BackoffConfig:   backoff.Config{MaxRetries: 1},
				BucketNames:     "foo",
				SSEConfig:       tt.sseConfig,
			}

			c, err := NewS3ObjectClient(cfg, hedging.Config{})
			require.NoError(t, err)
			c.S3 = mockS3
			c.hedgedS3 = c.S3

			_, _, err = c.GetObject(context.Background(), "foo")
			assert.NoError(t, err, "GetObject should succeed")

			_, err = c.GetObjectRange(context.Background(), "foo", 0, 10)
			assert.NoError(t, err, "GetObjectRange should succeed")
		})
	}
}

func TestS3SSEConfig_PutObject(t *testing.T) {
	tests := []struct {
		name      string
		sseConfig bucket_s3.SSEConfig
	}{
		{
			name: "SSE-C configured",
			sseConfig: bucket_s3.SSEConfig{
				Type:                  bucket_s3.SSEC,
				CustomerEncryptionKey: strings.Repeat("a", 32),
			},
		},
		{
			name: "SSE-S3 configured",
			sseConfig: bucket_s3.SSEConfig{
				Type: bucket_s3.SSES3,
			},
		},
		{
			name: "SSE-KMS configured",
			sseConfig: bucket_s3.SSEConfig{
				Type:                 bucket_s3.SSEKMS,
				KMSKeyID:             "test",
				KMSEncryptionContext: `{"a": "bc", "b": "cd"}`,
			},
		},
		{
			name:      "No SSE configured",
			sseConfig: bucket_s3.SSEConfig{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockS3 := &MockS3Client{
				PutObjectFunc: func(_ context.Context, params *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
					if tt.sseConfig != (bucket_s3.SSEConfig{}) {
						switch tt.sseConfig.Type {
						case bucket_s3.SSEC:
							keyHash := md5.Sum([]byte(tt.sseConfig.CustomerEncryptionKey)[:])
							b64CustomerKey := base64.StdEncoding.EncodeToString([]byte(tt.sseConfig.CustomerEncryptionKey))
							b64CustomerKeyMD5 := base64.StdEncoding.EncodeToString(keyHash[:])

							// sse-c algo is always a pointer to a string which value is AES256
							assert.Equal(t, string(types.ServerSideEncryptionAes256), *params.SSECustomerAlgorithm)
							assert.Equal(t, b64CustomerKey, *params.SSECustomerKey)
							assert.Equal(t, b64CustomerKeyMD5, *params.SSECustomerKeyMD5)

							assert.Empty(t, params.ServerSideEncryption)
							assert.Empty(t, params.SSEKMSKeyId)
							assert.Empty(t, params.SSEKMSEncryptionContext)
						case bucket_s3.SSES3:
							assert.Equal(t, types.ServerSideEncryptionAes256, params.ServerSideEncryption)

							assert.Empty(t, params.SSECustomerAlgorithm)
							assert.Empty(t, params.SSECustomerKey)
							assert.Empty(t, params.SSECustomerKeyMD5)
							assert.Empty(t, params.SSEKMSKeyId)
							assert.Empty(t, params.SSEKMSEncryptionContext)
						case bucket_s3.SSEKMS:
							encContext, err := parseKMSEncryptionContext(tt.sseConfig.KMSEncryptionContext)
							assert.NoError(t, err)
							assert.Equal(t, types.ServerSideEncryptionAwsKms, params.ServerSideEncryption)
							assert.Equal(t, tt.sseConfig.KMSKeyID, *params.SSEKMSKeyId)
							assert.Equal(t, *encContext, *params.SSEKMSEncryptionContext)

							assert.Empty(t, params.SSECustomerAlgorithm)
							assert.Empty(t, params.SSECustomerKey)
							assert.Empty(t, params.SSECustomerKeyMD5)
						}
					} else {
						// no SSE configured, so no SSE headers should be set
						assert.Empty(t, params.SSECustomerAlgorithm)
						assert.Empty(t, params.SSECustomerKey)
						assert.Empty(t, params.SSECustomerKeyMD5)
						assert.Empty(t, params.ServerSideEncryption)
						assert.Empty(t, params.SSEKMSKeyId)
						assert.Empty(t, params.SSEKMSEncryptionContext)
					}

					return &s3.PutObjectOutput{}, nil
				},
			}

			cfg := S3Config{
				AccessKeyID:     "foo",
				SecretAccessKey: flagext.SecretWithValue("bar"),
				BackoffConfig:   backoff.Config{MaxRetries: 1},
				BucketNames:     "foo",
				SSEConfig:       tt.sseConfig,
			}

			c, err := NewS3ObjectClient(cfg, hedging.Config{})
			require.NoError(t, err)
			c.S3 = mockS3
			c.hedgedS3 = c.S3

			err = c.PutObject(context.Background(), "foo", bytes.NewReader([]byte("bar")))
			assert.NoError(t, err, "PutObject should succeed")
		})
	}
}

func TestS3SSEConfig_GetAttributes(t *testing.T) {
	// GetObject injects only headers for SSE of type SSE-C

	tests := []struct {
		name            string
		wantSSECHeaders bool
		sseConfig       bucket_s3.SSEConfig
	}{
		{
			name:            "SSE-C configured",
			wantSSECHeaders: true,
			sseConfig: bucket_s3.SSEConfig{
				Type:                  bucket_s3.SSEC,
				CustomerEncryptionKey: strings.Repeat("a", 32),
			},
		},
		{
			name:            "SSE-S3 configured",
			wantSSECHeaders: false,
			sseConfig: bucket_s3.SSEConfig{
				Type: bucket_s3.SSES3,
			},
		},
		{
			name:            "SSE-KMS configured",
			wantSSECHeaders: false,
			sseConfig: bucket_s3.SSEConfig{
				Type:                 bucket_s3.SSEKMS,
				KMSKeyID:             "test",
				KMSEncryptionContext: `{"a": "bc", "b": "cd"}`,
			},
		},
		{
			name:            "No SSE configured",
			wantSSECHeaders: false,
			sseConfig:       bucket_s3.SSEConfig{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockS3 := &MockS3Client{
				HeadObjectFunc: func(_ context.Context, params *s3.HeadObjectInput) (*s3.HeadObjectOutput, error) {
					keyHash := md5.Sum([]byte(tt.sseConfig.CustomerEncryptionKey)[:])
					b64CustomerKey := base64.StdEncoding.EncodeToString([]byte(tt.sseConfig.CustomerEncryptionKey))
					b64CustomerKeyMD5 := base64.StdEncoding.EncodeToString(keyHash[:])

					// check that SSE-C headers are only set when expected
					// SSE-S3/SSE-KMS related headers (like x-amz-server-side-encryption-aws-kms-key-id) are required only when uploading/creating objects, not for retrieving.
					if tt.wantSSECHeaders {
						assert.Equal(t, string(types.ServerSideEncryptionAes256), *params.SSECustomerAlgorithm)
						assert.Equal(t, b64CustomerKey, *params.SSECustomerKey)
						assert.Equal(t, b64CustomerKeyMD5, *params.SSECustomerKeyMD5)
					} else {
						// no headers should be set when SSE-C is not expected
						assert.Empty(t, params.SSECustomerAlgorithm)
						assert.Empty(t, params.SSECustomerKey)
						assert.Empty(t, params.SSECustomerKeyMD5)
					}

					return &s3.HeadObjectOutput{
						Metadata: map[string]string{
							"Content-Length": "128",
						},
					}, nil
				},
			}

			cfg := S3Config{
				AccessKeyID:     "foo",
				SecretAccessKey: flagext.SecretWithValue("bar"),
				BackoffConfig:   backoff.Config{MaxRetries: 1},
				BucketNames:     "foo",
				SSEConfig:       tt.sseConfig,
			}

			c, err := NewS3ObjectClient(cfg, hedging.Config{})
			require.NoError(t, err)
			c.S3 = mockS3
			c.hedgedS3 = c.S3

			_, err = c.GetAttributes(context.Background(), "foo")
			assert.NoError(t, err, "GetAttributes should succeed")
		})
	}
}
