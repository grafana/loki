package azure

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/pkg/storage/chunk/client/hedging"
)

var metrics = NewBlobStorageMetrics()

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
						// blocklist is a call that can be fired by the SDK after PUT but is not guaranteed.
						if !strings.Contains(req.URL.String(), "blocklist") {
							count.Inc()
							time.Sleep(50 * time.Millisecond)
						}
						return nil, http.ErrNotSupported
					}),
				}
			}
			c, err := NewBlobStorage(&BlobStorageConfig{
				ContainerName: "foo",
				Environment:   azureGlobal,
				MaxRetries:    1,
			}, metrics,
				hedging.Config{
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

func Test_DefaultContainerURL(t *testing.T) {
	c, err := NewBlobStorage(&BlobStorageConfig{
		ContainerName:      "foo",
		StorageAccountName: "bar",
		Environment:        azureGlobal,
	}, metrics, hedging.Config{})
	require.NoError(t, err)
	expect, _ := url.Parse("https://bar.blob.core.windows.net/foo")
	require.Equal(t, *expect, c.containerURL.URL())
}

func Test_EndpointSuffixWithContainer(t *testing.T) {
	c, err := NewBlobStorage(&BlobStorageConfig{
		ContainerName:      "foo",
		StorageAccountName: "bar",
		Environment:        azureGlobal,
		Endpoint:           "test.com",
	}, metrics, hedging.Config{})
	require.NoError(t, err)
	expect, _ := url.Parse("https://bar.test.com/foo")
	require.Equal(t, *expect, c.containerURL.URL())
}

func Test_DefaultBlobURL(t *testing.T) {
	c, err := NewBlobStorage(&BlobStorageConfig{
		ContainerName:      "foo",
		StorageAccountName: "bar",
		Environment:        azureGlobal,
	}, metrics, hedging.Config{})
	require.NoError(t, err)
	expect, _ := url.Parse("https://bar.blob.core.windows.net/foo/blob")
	bloburl, err := c.getBlobURL("blob", false)
	require.NoError(t, err)
	require.Equal(t, *expect, bloburl.URL())
}

func Test_EndpointSuffixWithBlob(t *testing.T) {
	c, err := NewBlobStorage(&BlobStorageConfig{
		ContainerName:      "foo",
		StorageAccountName: "bar",
		Environment:        azureGlobal,
		Endpoint:           "test.com",
	}, metrics, hedging.Config{})
	require.NoError(t, err)
	expect, _ := url.Parse("https://bar.test.com/foo/blob")
	bloburl, err := c.getBlobURL("blob", false)
	require.NoError(t, err)
	require.Equal(t, *expect, bloburl.URL())
}
