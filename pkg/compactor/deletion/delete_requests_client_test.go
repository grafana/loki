package deletion

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGetCacheGenNumberForUser(t *testing.T) {
	deleteClientMetrics := NewDeleteRequestClientMetrics(nil)

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

		compactorClient.SetDeleteRequests([]DeleteRequest{
			{
				RequestID: "different",
			},
		})

		deleteRequests, err = client.GetAllDeleteRequestsForUser(context.Background(), "userID")
		require.Nil(t, err)
		require.Equal(t, "test-request", deleteRequests[0].RequestID)

		time.Sleep(200 * time.Millisecond)

		deleteRequests, err = client.GetAllDeleteRequestsForUser(context.Background(), "userID")
		require.Nil(t, err)
		require.Equal(t, "different", deleteRequests[0].RequestID)

		// failure in compactor calls should not wipe the cache
		compactorClient.SetErr(fmt.Errorf("fail compactor calls"))
		time.Sleep(200 * time.Millisecond)

		deleteRequests, err = client.GetAllDeleteRequestsForUser(context.Background(), "userID")
		require.Nil(t, err)
		require.Equal(t, "different", deleteRequests[0].RequestID)

		client.Stop()
	})
}

type mockCompactorClient struct {
	mx          sync.Mutex
	delRequests []DeleteRequest
	cacheGenNum string
	err         error
}

func (m *mockCompactorClient) SetDeleteRequests(d []DeleteRequest) {
	m.mx.Lock()
	m.delRequests = d
	m.mx.Unlock()
}

func (m *mockCompactorClient) GetAllDeleteRequestsForUser(_ context.Context, _ string) ([]DeleteRequest, error) {
	m.mx.Lock()
	defer m.mx.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	return m.delRequests, nil
}

func (m *mockCompactorClient) GetCacheGenerationNumber(_ context.Context, _ string) (string, error) {
	if m.err != nil {
		return "", m.err
	}

	return m.cacheGenNum, nil
}

func (m *mockCompactorClient) Name() string {
	return ""
}

func (m *mockCompactorClient) Stop() {}

func (m *mockCompactorClient) SetErr(err error) {
	m.mx.Lock()
	defer m.mx.Unlock()
	m.err = err
}
