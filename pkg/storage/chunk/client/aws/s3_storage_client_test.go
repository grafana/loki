package aws

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/pkg/storage/chunk/client/hedging"
)

type RoundTripperFunc func(*http.Request) (*http.Response, error)

func (f RoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
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
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			count := atomic.NewInt32(0)

			c, err := NewS3ObjectClient(S3Config{
				AccessKeyID:     "foo",
				SecretAccessKey: flagext.SecretWithValue("bar"),
				BackoffConfig:   backoff.Config{MaxRetries: 1},
				BucketNames:     "foo",
				Inject: func(next http.RoundTripper) http.RoundTripper {
					return RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
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
