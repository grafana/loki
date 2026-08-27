package gcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
)

func Test_Hedging(t *testing.T) {
	for _, tc := range []struct {
		name          string
		expectedCalls int32
		hedgeAt       time.Duration
		upTo          int
		do            func(c *GCSObjectClient)
	}{
		{
			"delete/put/list are not hedged",
			3,
			20 * time.Nanosecond,
			10,
			func(c *GCSObjectClient) {
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
			func(c *GCSObjectClient) {
				_, _, _ = c.GetObject(context.Background(), "foo")
			},
		},
		{
			"gets are not hedged when not configured",
			1,
			0,
			0,
			func(c *GCSObjectClient) {
				_, _, _ = c.GetObject(context.Background(), "foo")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count := new(atomic.Int32)
			server := fakeServer(t, 200*time.Millisecond, count)
			ctx := context.Background()
			c, err := newGCSObjectClient(ctx, GCSConfig{
				BucketName: "test-bucket",
				Insecure:   true,
			}, hedging.Config{
				At:           tc.hedgeAt,
				UpTo:         tc.upTo,
				MaxPerSecond: 1000,
			}, func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error) {
				opts = append(opts, option.WithEndpoint(server.URL))
				opts = append(opts, option.WithoutAuthentication())
				return storage.NewClient(ctx, opts...)
			})
			require.NoError(t, err)
			tc.do(c)
			require.Equal(t, tc.expectedCalls, count.Load())
		})
	}
}

func fakeServer(t *testing.T, returnIn time.Duration, counter *atomic.Int32) *httptest.Server {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		counter.Add(1)
		time.Sleep(returnIn)
		_, _ = w.Write([]byte(`{}`))
	}))
	server.StartTLS()
	t.Cleanup(server.Close)

	return server
}

func TestUpstreamRetryableErrs(t *testing.T) {

	tests := []struct {
		name             string
		httpResponseCode int
		isThrottledErr   bool
		isTimeoutErr     bool
	}{
		{
			"bad request",
			http.StatusBadRequest,
			false,
			false,
		},
		{
			"too many requests",
			http.StatusTooManyRequests,
			true,
			false,
		},
		{
			"request timeout",
			http.StatusRequestTimeout,
			false,
			true,
		},
		{
			"internal server error",
			http.StatusInternalServerError,
			true,
			false,
		},
		{
			"service unavailable",
			http.StatusServiceUnavailable,
			true,
			false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := fakeHTTPRespondingServer(t, tc.httpResponseCode)
			ctx := context.Background()
			cli, err := newGCSObjectClient(ctx, GCSConfig{
				BucketName:    "test-bucket",
				Insecure:      true,
				EnableRetries: false,
			}, hedging.Config{}, func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error) {
				opts = append(opts, option.WithEndpoint(server.URL))
				opts = append(opts, option.WithoutAuthentication())
				return storage.NewClient(ctx, opts...)
			})

			require.NoError(t, err)

			_, _, err = cli.GetObject(ctx, "foo")
			require.Equal(t, tc.isThrottledErr, isStorageThrottledErr(err))
			require.Equal(t, tc.isTimeoutErr, isStorageTimeoutErr(err))
		})
	}
}

func TestIsStorageTimeoutErr(t *testing.T) {
	clientTimeout := &url.Error{
		Op:  "Get",
		URL: "http://example",
		Err: fmt.Errorf("%w (Client.Timeout exceeded while awaiting headers)", context.DeadlineExceeded),
	}
	syscallErr := func(err syscall.Errno) error {
		return &net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", err)}
	}

	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{"http client timeout is server-side", clientTimeout, true},
		{"caller context deadline", &url.Error{Op: "Get", URL: "http://example", Err: context.DeadlineExceeded}, false},
		{"caller context canceled", context.Canceled, false},

		{"use of closed connection", net.ErrClosed, false},
		{"connection refused", syscallErr(syscall.ECONNREFUSED), false},

		{"i/o timeout", &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}, true},
		{"eof (closed before established)", io.EOF, true},
		{"connection reset (closed after established)", syscallErr(syscall.ECONNRESET), true},

		{"gcs request timeout", &googleapi.Error{Code: http.StatusRequestTimeout}, true},
		{"gcs gateway timeout", &googleapi.Error{Code: http.StatusGatewayTimeout}, true},
		{"gcs internal error is not a timeout", &googleapi.Error{Code: http.StatusInternalServerError}, false},

		{"unclassified error", errors.New("boom"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.retryable, isStorageTimeoutErr(tc.err))
		})
	}
}

func TestTCPErrs(t *testing.T) {
	const ctxTimeout = 100 * time.Millisecond

	tests := []struct {
		name string
		// server behaviour (exactly one is set per case)
		hangResponse  bool // accepts the request but never replies
		hangConnect   bool // accepts the connection but never reads the request
		closeOnActive bool // drops the connection after reading the request

		ctxTimeout time.Duration
		retryable  bool
	}{
		{
			// the caller's context deadline fires while awaiting the response: client-side, not retryable
			name:         "context deadline while awaiting response, not retryable",
			hangResponse: true,
			ctxTimeout:   ctxTimeout,
			retryable:    false,
		},
		{
			// the caller's context deadline fires while the server is silent on connect: client-side, not retryable
			name:        "context deadline while connecting, not retryable",
			hangConnect: true,
			ctxTimeout:  ctxTimeout,
			retryable:   false,
		},
		{
			// the server drops the connection: a reset is a server-side issue, retryable
			name:          "connection reset server-side, retryable",
			closeOnActive: true,
			retryable:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := fakeSleepingServer(t, tc.hangResponse, tc.hangConnect, tc.closeOnActive)

			ctx := context.Background()
			if tc.ctxTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.ctxTimeout)
				t.Cleanup(cancel)
			}

			// A plain-HTTP transport is required so the client speaks to the
			// plain-HTTP fake server; the default GCS transport would attempt TLS.
			client := &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()}

			cli, err := newGCSObjectClient(ctx, GCSConfig{
				BucketName:    "test-bucket",
				Insecure:      true,
				EnableRetries: false,
			}, hedging.Config{}, func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error) {
				opts = append(opts, option.WithEndpoint(server.URL))
				opts = append(opts, option.WithoutAuthentication())
				opts = append(opts, option.WithHTTPClient(client))
				return storage.NewClient(ctx, opts...)
			})

			require.NoError(t, err)

			_, _, err = cli.GetObject(ctx, "foo")
			require.Error(t, err)
			require.Equal(t, tc.retryable, isStorageTimeoutErr(err))
		})
	}
}

func fakeHTTPRespondingServer(t *testing.T, code int) *httptest.Server {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
	}))
	server.StartTLS()
	t.Cleanup(server.Close)

	return server
}

func fakeSleepingServer(t *testing.T, hangResponse, hangConnect, closeOnActive bool) *httptest.Server {
	release := make(chan struct{})

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		if hangResponse {
			<-release
		}
	}))
	server.Config.ConnState = func(conn net.Conn, state http.ConnState) {
		if state == http.StateNew && hangConnect {
			<-release
		}

		if state == http.StateActive && closeOnActive {
			_ = conn.Close()
		}
	}
	t.Cleanup(func() {
		close(release)
		server.Close()
	})
	server.Start()
	return server
}
