package aws

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"go.yaml.in/yaml/v4"

	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

func TestMain(m *testing.M) {
	// Prevent tests from loading the host's AWS config, which may have profiles
	// that fail validation (e.g. SSO profiles missing required fields).
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "AWS_") {
			os.Unsetenv(strings.SplitN(env, "=", 2)[0])
		}
	}
	os.Setenv("AWS_CONFIG_FILE", "/dev/null")
	os.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")
	os.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	os.Exit(m.Run())
}

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
}

func (m *MockS3Client) HeadObject(ctx context.Context, input *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	return m.HeadObjectFunc(ctx, input)
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

// TestPutObject_NoAWSChunkedEncoding verifies that PutObject sends the SHA-256
// checksum as a plain request header (x-amz-checksum-sha256) and does NOT use
// aws-chunked transfer encoding (Content-Encoding: aws-chunked).
func TestPutObject_NoAWSChunkedEncoding(t *testing.T) {
	var capturedReq *http.Request

	// Use an HTTPS test server because the SDK only activates trailing
	// checksums (and therefore aws-chunked) on HTTPS connections.
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.WriteHeader(http.StatusOK)
		// Return minimal valid S3 PutObject XML response
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><PutObjectResponse/>`))
	}))
	defer ts.Close()

	cfg := S3Config{
		Endpoint:         ts.URL,
		BucketNames:      "test-bucket",
		S3ForcePathStyle: true,
		AccessKeyID:      "test-key",
		SecretAccessKey:  flagext.SecretWithValue("test-secret"),
		// Use the TLS client from the test server (accepts self-signed cert)
		Inject: func(_ http.RoundTripper) http.RoundTripper {
			return ts.Client().Transport
		},
	}

	client, err := NewS3ObjectClient(cfg, hedging.Config{})
	require.NoError(t, err)

	body := []byte("hello from loki chunk")
	err = client.PutObject(context.Background(), "test/key", bytes.NewReader(body))
	require.NoError(t, err)
	require.NotNil(t, capturedReq, "server should have received a request")

	// Verify: no aws-chunked encoding header
	contentEncoding := capturedReq.Header.Get("Content-Encoding")
	require.NotContains(t, contentEncoding, "aws-chunked",
		"PutObject must NOT use aws-chunked encoding — breaks S3-compatible backends like OpenStack Swift (issue #21791)")

	// Verify: SHA-256 checksum is present as a plain header
	checksumHeader := capturedReq.Header.Get("x-amz-checksum-sha256")
	require.NotEmpty(t, checksumHeader,
		"PutObject must send x-amz-checksum-sha256 header for Object Lock compatibility (issue #20088)")

	// Verify: x-amz-content-sha256 is NOT the streaming sentinel value
	payloadHash := capturedReq.Header.Get("x-amz-content-sha256")
	require.NotEqual(t, "STREAMING-UNSIGNED-PAYLOAD-TRAILER", payloadHash,
		"payload hash must not be the streaming sentinel — that indicates aws-chunked mode is active")
}

// TestPutObject_SwiftRejects_AWSChunked reproduces the exact failure from
// https://github.com/grafana/loki/issues/21791.
//
// It spins up a mock server that behaves like OpenStack Swift's S3-compatible
// API: it returns 501 NotImplemented when it receives a request with
// Content-Encoding: aws-chunked, and 200 OK for normal requests.
//
// With the fix in place, PutObject sends a plain request (no aws-chunked)
// and the mock Swift server accepts it successfully.
func TestPutObject_SwiftRejects_AWSChunked(t *testing.T) {
	// mockSwiftHandler simulates OpenStack Swift S3-compatible API behaviour:
	// rejects aws-chunked with 501, accepts plain requests with 200.
	mockSwiftHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentEncoding := r.Header.Get("Content-Encoding")

		if strings.Contains(contentEncoding, "aws-chunked") {
			// This is exactly what OpenStack Swift returns
			w.WriteHeader(http.StatusNotImplemented)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Error>
  <Code>NotImplemented</Code>
  <Message>Transfering payloads in multiple chunks using aws-chunked is not supported</Message>
</Error>`))
			return
		}

		// Normal request — Swift accepts it
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><PutObjectResponse/>`))
	})

	ts := httptest.NewTLSServer(mockSwiftHandler)
	defer ts.Close()

	cfg := S3Config{
		Endpoint:         ts.URL,
		BucketNames:      "swift-bucket",
		S3ForcePathStyle: true,
		AccessKeyID:      "swift-key",
		SecretAccessKey:  flagext.SecretWithValue("swift-secret"),
		Inject: func(_ http.RoundTripper) http.RoundTripper {
			return ts.Client().Transport
		},
	}

	client, err := NewS3ObjectClient(cfg, hedging.Config{})
	require.NoError(t, err)

	body := []byte("loki log chunk data")

	// With the fix: PutObject should succeed because no aws-chunked is sent
	err = client.PutObject(context.Background(), "logs/chunk-001", bytes.NewReader(body))
	require.NoError(t, err,
		"PutObject must succeed against OpenStack Swift (no aws-chunked should be sent)")
}
