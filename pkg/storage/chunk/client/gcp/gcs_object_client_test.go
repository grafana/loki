package gcp

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
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
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			count := atomic.NewInt32(0)
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
		counter.Inc()
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
			require.Equal(t, tc.isThrottledErr, cli.IsStorageThrottledErr(err))
			require.Equal(t, tc.isTimeoutErr, cli.IsStorageTimeoutErr(err))
		})
	}
}

func TestTCPErrs(t *testing.T) {

	tests := []struct {
		name           string
		responseSleep  time.Duration
		connectSleep   time.Duration
		clientTimeout  time.Duration
		serverTimeout  time.Duration
		connectTimeout time.Duration
		closeOnNew     bool
		closeOnActive  bool
		retryable      bool
	}{
		{
			name:          "request took longer than client timeout, not retryable",
			responseSleep: time.Millisecond * 20,
			clientTimeout: time.Millisecond * 10,
			retryable:     false,
		},
		{
			name:          "client timeout exceeded on connect, not retryable",
			connectSleep:  time.Millisecond * 20,
			clientTimeout: time.Millisecond * 10,
			retryable:     false,
		},
		{
			// there are retryable because it's a server-side timeout
			name:           "transport connect timeout exceeded, retryable",
			connectSleep:   time.Millisecond * 40,
			connectTimeout: time.Millisecond * 20,
			// even though the client timeout is set, the connect timeout will be hit first
			clientTimeout: time.Millisecond * 100,
			retryable:     true,
		},
		{
			name:          "connection is closed server-side before being established",
			connectSleep:  time.Millisecond * 10,
			clientTimeout: time.Millisecond * 100,
			closeOnNew:    true,
			retryable:     true,
		},
		{
			name:          "connection is closed server-side after being established",
			connectSleep:  time.Millisecond * 10,
			clientTimeout: time.Millisecond * 100,
			closeOnActive: true,
			retryable:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := fakeSleepingServer(t, tc.responseSleep, tc.connectSleep, tc.closeOnNew, tc.closeOnActive)
			ctx, cancelFunc := context.WithTimeout(context.Background(), tc.clientTimeout)
			defer t.Cleanup(cancelFunc)

			client := http.DefaultClient
			transport := http.DefaultTransport.(*http.Transport).Clone()
			client.Transport = transport
			client.Timeout = tc.connectTimeout

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
			require.Equal(t, tc.retryable, cli.IsStorageTimeoutErr(err))
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

func fakeSleepingServer(t *testing.T, responseSleep, connectSleep time.Duration, closeOnNew, closeOnActive bool) *httptest.Server {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		// sleep on response to mimic server overload
		time.Sleep(responseSleep)
	}))
	server.Config.ConnState = func(conn net.Conn, state http.ConnState) {
		// sleep on initial connection attempt to mimic server non-responsiveness
		if state == http.StateNew {
			time.Sleep(connectSleep)
			if closeOnNew {
				require.NoError(t, conn.Close())
			}
		}

		if closeOnActive && state != http.StateClosed {
			require.NoError(t, conn.Close())
		}
	}
	t.Cleanup(server.Close)
	server.Start()
	return server
}
