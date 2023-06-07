package gcp

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"google.golang.org/api/option"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/pkg/storage/chunk/client/hedging"
)

func TestGCSConfig_UnmarshalYAML(t *testing.T) {
	in := []byte(`bucket_name: foobar
request_timeout: 30s`)

	dst := &GCSConfig{}
	require.NoError(t, yaml.UnmarshalStrict(in, dst))
	require.Equal(t, "foobar", dst.BucketName)

	// set defaults
	require.Equal(t, true, dst.EnableHTTP2)

	// override defaults
	require.Equal(t, 30*time.Second, dst.RequestTimeout)
}

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
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.Inc()
		time.Sleep(returnIn)
		_, _ = w.Write([]byte(`{}`))
	}))
	server.StartTLS()
	t.Cleanup(server.Close)

	return server
}
