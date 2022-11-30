package deletion

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestGetCacheGenNumberForUser(t *testing.T) {
	deleteClientMetrics := NewDeleteRequestClientMetrics(prometheus.DefaultRegisterer)

	t.Run("it requests results from the compactor client", func(t *testing.T) {
		compactorClient := mockCompactorClient{
			delRequests: []DeleteRequest{
				{
					RequestID: "test-request",
				},
			},
		}

		client, err := NewDeleteRequestsClient(&compactorClient, deleteClientMetrics, "test_client")
		require.Nil(t, err)

		deleteRequests, err := client.GetAllDeleteRequestsForUser(context.Background(), "userID")
		require.Nil(t, err)

		require.Len(t, deleteRequests, 1)
		require.Equal(t, "test-request", deleteRequests[0].RequestID)
	})

	t.Run("it caches the results", func(t *testing.T) {
		compactorClient := mockCompactorClient{
			delRequests: []DeleteRequest{
				{
					RequestID: "test-request",
				},
			},
		}
		client, err := NewDeleteRequestsClient(&compactorClient, deleteClientMetrics, "test_client", WithRequestClientCacheDuration(100*time.Millisecond))
		require.Nil(t, err)

		deleteRequests, err := client.GetAllDeleteRequestsForUser(context.Background(), "userID")
		require.Nil(t, err)
		require.Equal(t, "test-request", deleteRequests[0].RequestID)

		compactorClient.delRequests = []DeleteRequest{
			{
				RequestID: "different",
			},
		}

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

type mockCompactorClient struct {
	delRequests []DeleteRequest
	cacheGenNum string
}

func (m *mockCompactorClient) GetAllDeleteRequestsForUser(ctx context.Context, userID string) ([]DeleteRequest, error) {
	return m.delRequests, nil
}

func (m *mockCompactorClient) GetCacheGenerationNumber(ctx context.Context, userID string) (string, error) {
	return m.cacheGenNum, nil
}

func (m *mockCompactorClient) Name() string {
	return ""
}

func (m *mockCompactorClient) Stop() {}
