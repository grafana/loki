package deletion

import (
	"context"

	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/retention"
)

type perTenantDeleteRequestsClient struct {
	client DeleteRequestsClient
	limits retention.Limits
}

func NewPerTenantDeleteRequestsClient(c DeleteRequestsClient, l retention.Limits) DeleteRequestsClient {
	return &perTenantDeleteRequestsClient{
		client: c,
		limits: l,
	}
}

func (c *perTenantDeleteRequestsClient) GetAllDeleteRequestsForUser(ctx context.Context, userID string) ([]DeleteRequest, error) {
	allLimits := c.limits.AllByUserID()
	userLimits, ok := allLimits[userID]
	if ok && userLimits.CompactorDeletionEnabled || c.limits.DefaultLimits().CompactorDeletionEnabled {
		return c.client.GetAllDeleteRequestsForUser(ctx, userID)
	}

	return nil, nil
}

func (c *perTenantDeleteRequestsClient) Stop() {
	c.client.Stop()
}
