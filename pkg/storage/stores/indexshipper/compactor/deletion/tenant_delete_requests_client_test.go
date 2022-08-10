package deletion

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTenantDeleteRequestsClient(t *testing.T) {
	fakeClient := &fakeRequestsClient{
		reqs: []DeleteRequest{{
			RequestID: "test-request",
		}},
	}
	perTenantClient := NewPerTenantDeleteRequestsClient(fakeClient, limits)

	t.Run("tenant enabled", func(t *testing.T) {
		reqs, err := perTenantClient.GetAllDeleteRequestsForUser(context.Background(), "1")
		require.Nil(t, err)
		require.Equal(t, []DeleteRequest{{RequestID: "test-request"}}, reqs)
	})

	t.Run("tenant disabled", func(t *testing.T) {
		reqs, err := perTenantClient.GetAllDeleteRequestsForUser(context.Background(), "2")
		require.Nil(t, err)
		require.Empty(t, reqs)
	})

	t.Run("tenant disabled but default enabled", func(t *testing.T) {
		limits.defaultLimit.compactorDeletionEnabled = "filter-only"
		reqs, err := perTenantClient.GetAllDeleteRequestsForUser(context.Background(), "2")
		require.Nil(t, err)
		require.Empty(t, reqs)
	})

	t.Run("default is enabled", func(t *testing.T) {
		limits.defaultLimit.compactorDeletionEnabled = "filter-and-delete"
		reqs, err := perTenantClient.GetAllDeleteRequestsForUser(context.Background(), "3")
		require.Nil(t, err)
		require.Equal(t, []DeleteRequest{{RequestID: "test-request"}}, reqs)
	})

	t.Run("default is disabled", func(t *testing.T) {
		limits.defaultLimit.compactorDeletionEnabled = "disabled"
		reqs, err := perTenantClient.GetAllDeleteRequestsForUser(context.Background(), "3")
		require.Nil(t, err)
		require.Empty(t, reqs)
	})
}

type fakeRequestsClient struct {
	DeleteRequestsClient

	reqs []DeleteRequest
}

func (c *fakeRequestsClient) GetAllDeleteRequestsForUser(_ context.Context, userID string) ([]DeleteRequest, error) {
	return c.reqs, nil
}

var (
	limits = &fakeLimits{
		perTenant: map[string]retentionLimit{
			"1": {compactorDeletionEnabled: "filter-only"},
			"2": {compactorDeletionEnabled: "disabled"},
		},
	}
)
