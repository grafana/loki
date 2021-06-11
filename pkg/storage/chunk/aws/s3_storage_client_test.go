package aws

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		SecretAccessKey:  "secret",
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
			client, err := NewS3ObjectClient(cfg)
			require.NoError(t, err)

			readCloser, err := client.GetObject(context.Background(), "key")
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
