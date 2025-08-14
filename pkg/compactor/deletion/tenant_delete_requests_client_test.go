package deletion

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compactor/deletion/deletionproto"
)

func TestTenantDeleteRequestsClient(t *testing.T) {
	fakeClient := &fakeRequestsClient{
		reqs: []deletionproto.DeleteRequest{{
			RequestID: "test-request",
		}},
	}
	perTenantClient := NewPerTenantDeleteRequestsClient(fakeClient, defaultLimits)

	t.Run("tenant enabled", func(t *testing.T) {
		reqs, err := perTenantClient.GetAllDeleteRequestsForUser(context.Background(), "1")
		require.Nil(t, err)
		require.Equal(t, []deletionproto.DeleteRequest{{RequestID: "test-request"}}, reqs)
	})

	t.Run("tenant disabled", func(t *testing.T) {
		reqs, err := perTenantClient.GetAllDeleteRequestsForUser(context.Background(), "2")
		require.Nil(t, err)
		require.Empty(t, reqs)
	})
}

type fakeRequestsClient struct {
	DeleteRequestsClient

	reqs []deletionproto.DeleteRequest
}

func (c *fakeRequestsClient) GetAllDeleteRequestsForUser(_ context.Context, _ string) ([]deletionproto.DeleteRequest, error) {
	return c.reqs, nil
}

var (
	defaultLimits = &fakeLimits{
		tenantLimits: map[string]limit{
			"1": {deletionMode: "filter-only"},
			"2": {deletionMode: "disabled"},
			"3": {retentionPeriod: time.Hour},
		},
	}
)
