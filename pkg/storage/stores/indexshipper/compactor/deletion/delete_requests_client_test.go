package deletion

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestGetCacheGenNumberForUser(t *testing.T) {
	deleteClientMetrics := NewDeleteRequestClientMetrics(prometheus.DefaultRegisterer)

	t.Run("it requests results from the api", func(t *testing.T) {
		httpClient := &mockHTTPClient{ret: `[{"request_id":"test-request"}]`}
		client, err := NewDeleteRequestsClient("http://test-server", httpClient, deleteClientMetrics, "test_client")
		require.Nil(t, err)

		deleteRequests, err := client.GetAllDeleteRequestsForUser(context.Background(), "userID")
		require.Nil(t, err)

		require.Len(t, deleteRequests, 1)
		require.Equal(t, "test-request", deleteRequests[0].RequestID)

		require.Equal(t, "http://test-server/loki/api/v1/delete", httpClient.req.URL.String())
		require.Equal(t, http.MethodGet, httpClient.req.Method)
		require.Equal(t, "userID", httpClient.req.Header.Get("X-Scope-OrgID"))
	})

	t.Run("it caches the results", func(t *testing.T) {
		httpClient := &mockHTTPClient{ret: `[{"request_id":"test-request"}]`}
		client, err := NewDeleteRequestsClient("http://test-server", httpClient, deleteClientMetrics, "test_client", WithRequestClientCacheDuration(100*time.Millisecond))
		require.Nil(t, err)

		deleteRequests, err := client.GetAllDeleteRequestsForUser(context.Background(), "userID")
		require.Nil(t, err)
		require.Equal(t, "test-request", deleteRequests[0].RequestID)

		httpClient.ret = `[{"request_id":"different"}]`

		deleteRequests, err = client.GetAllDeleteRequestsForUser(context.Background(), "userID")
		require.Nil(t, err)
		require.Equal(t, "test-request", deleteRequests[0].RequestID)

		time.Sleep(200 * time.Millisecond)

		deleteRequests, err = client.GetAllDeleteRequestsForUser(context.Background(), "userID")
		require.Nil(t, err)
		require.Equal(t, "different", deleteRequests[0].RequestID)

		client.Stop()
	})
}

type mockHTTPClient struct {
	ret string
	req *http.Request
}

func (c *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	c.req = req

	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(c.ret)),
	}, nil
}
