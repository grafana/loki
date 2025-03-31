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

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
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
			err:      awserr.New(s3.ErrCodeNoSuchKey, "NoSuchKey", nil),
			expected: true,
		},
		{
			name:     "NotFound code is recognized as object not found",
			err:      awserr.New("NotFound", "NotFound", nil),
			expected: true,
		},
		{
			name:     "Nil error isnt recognized as object not found",
			err:      nil,
			expected: false,
		},
		{
			name:     "Other error isnt recognized as object not found",
			err:      awserr.New(s3.ErrCodeNoSuchBucket, "NoSuchBucket", nil),
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
			name: "IsStorageThrottledErr - Too Many Requests",
			err: awserr.NewRequestFailure(
				awserr.New("TooManyRequests", "TooManyRequests", nil), 429, "reqId",
			),
			expected: true,
		},
		{
			name: "IsStorageThrottledErr - 500",
			err: awserr.NewRequestFailure(
				awserr.New("500", "500", nil), 500, "reqId",
			),
			expected: true,
		},
		{
			name: "IsStorageThrottledErr - 5xx",
			err: awserr.NewRequestFailure(
				awserr.New("501", "501", nil), 501, "reqId",
			),
			expected: true,
		},
		{
			name: "IsStorageTimeoutErr - Request Timeout",
			err: awserr.NewRequestFailure(
				awserr.New("Request Timeout", "Request Timeout", nil), 408, "reqId",
			),
			expected: true,
		},
		{
			name: "IsStorageTimeoutErr - Gateway Timeout",
			err: awserr.NewRequestFailure(
				awserr.New("Gateway Timeout", "Gateway Timeout", nil), 504, "reqId",
			),
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
			name: "IsStorageTimeoutErr - Timeout Error",
			err: awserr.NewRequestFailure(
				awserr.New("RequestCanceled", "request canceled due to timeout", nil), 408, "request-id",
			),
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
			name: "Not found 404",
			err: awserr.NewRequestFailure(
				awserr.New("404", "404", nil), 404, "reqId",
			),
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
			3,
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
			3,
			20 * time.Nanosecond,
			3,
			func(c *S3ObjectClient) {
				_, _, _ = c.GetObject(context.Background(), "foo")
			},
		},
		{
			"gets are not hedged when not configured",
			1,
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
	s3.S3
	HeadObjectFunc func(*s3.HeadObjectInput) (*s3.HeadObjectOutput, error)
}

func (m *MockS3Client) HeadObject(input *s3.HeadObjectInput) (*s3.HeadObjectOutput, error) {
	return m.HeadObjectFunc(input)
}

func Test_GetAttributes(t *testing.T) {
	mockS3 := &MockS3Client{
		HeadObjectFunc: func(_ *s3.HeadObjectInput) (*s3.HeadObjectOutput, error) {
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
					return awserr.NewRequestFailure(
						awserr.New("NotFound", "Not Found", nil), 404, "abc",
					)
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
				HeadObjectFunc: func(_ *s3.HeadObjectInput) (*s3.HeadObjectOutput, error) {
					callNum := callCount.Inc()
					if !tc.exists {
						rfIn := awserr.NewRequestFailure(
							awserr.New("NotFound", "Not Found", nil), 404, "abc",
						)
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
	s3iface.S3API
}

func (m *testCommonPrefixesS3Client) ListObjectsV2WithContext(aws.Context, *s3.ListObjectsV2Input, ...request.Option) (*s3.ListObjectsV2Output, error) {
	var commonPrefixes []*s3.CommonPrefix
	commonPrefix := "common-prefix-repeated/"
	for i := 0; i < 2; i++ {
		commonPrefixes = append(commonPrefixes, &s3.CommonPrefix{Prefix: aws.String(commonPrefix)})
	}
	return &s3.ListObjectsV2Output{CommonPrefixes: commonPrefixes, IsTruncated: aws.Bool(false)}, nil
}

func TestCommonPrefixes(t *testing.T) {
	s3 := S3ObjectClient{S3: &testCommonPrefixesS3Client{}, bucketNames: []string{"bucket"}}
	_, CommonPrefixes, err := s3.List(context.Background(), "", "/")
	require.Equal(t, nil, err)
	require.Equal(t, 1, len(CommonPrefixes))
}
