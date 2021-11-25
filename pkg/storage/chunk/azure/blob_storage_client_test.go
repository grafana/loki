package azure

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
			"deletes are not hedged",
			1,
			5,
			10,
			func(c *BlobStorage) {
				_ = c.DeleteObject(context.Background(), "foo")
			},
		},
		{
			"list are not hedged",
			1,
			5,
			10,
			func(c *BlobStorage) {
				_, _, _ = c.List(context.Background(), "foo", "/")
			},
		},
		{
			"gets are hedged",
			3,
			5,
			3,
			func(c *BlobStorage) {
				_, _ = c.GetObject(context.Background(), "foo")
			},
		},
		{
			"gets are not hedged when not configured",
			1,
			0,
			0,
			func(c *BlobStorage) {
				_, _ = c.GetObject(context.Background(), "foo")
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			count := int32(0)
			// hijack the client to count the number of calls
			defaultClient = &http.Client{
				Transport: RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
					time.Sleep(1 * time.Second)
					atomic.AddInt32(&count, 1)
					return nil, errors.New("fo")
				}),
			}
			c, err := NewBlobStorage(&BlobStorageConfig{
				ContainerName: "foo",
				Environment:   azureGlobal,
				Hedging: hedging.Config{
					At:   tc.hedgeAt,
					UpTo: tc.upTo,
				},
				MaxRetries: 1,
			})
			require.NoError(t, err)
			tc.do(c)
			require.Equal(t, tc.expectedCalls, count)
		})
	}
}
