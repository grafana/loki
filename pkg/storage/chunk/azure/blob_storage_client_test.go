package azure

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/pkg/storage/chunk/hedging"
)

type RoundTripperFunc func(*http.Request) (*http.Response, error)

func (fn RoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func Test_Hedging(t *testing.T) {
	for _, tc := range []struct {
		name          string
		expectedCalls int32
		hedgeAt       time.Duration
		upTo          int
		do            func(c *BlobStorage)
	}{
		{
			"delete/put/list are not hedged",
			3,
			20 * time.Nanosecond,
			10,
			func(c *BlobStorage) {
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
			func(c *BlobStorage) {
				_, _, _ = c.GetObject(context.Background(), "foo")
			},
		},
		{
			"gets are not hedged when not configured",
			1,
			0,
			0,
			func(c *BlobStorage) {
				_, _, _ = c.GetObject(context.Background(), "foo")
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			count := atomic.NewInt32(0)
			// hijack the client to count the number of calls
			defaultClientFactory = func() *http.Client {
				return &http.Client{
					Transport: RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
						count.Inc()
						time.Sleep(200 * time.Millisecond)
						return nil, errors.New("fo")
					}),
				}
			}
			c, err := NewBlobStorage(&BlobStorageConfig{
				ContainerName: "foo",
				Environment:   azureGlobal,
				MaxRetries:    1,
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
