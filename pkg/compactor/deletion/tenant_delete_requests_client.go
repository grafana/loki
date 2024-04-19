package deletion

import (
	"context"
	"time"

	"github.com/grafana/loki/v3/pkg/validation"
)

const deletionNotAvailableMsg = "deletion is not available for this tenant"

type Limits interface {
	DeletionMode(userID string) string
	RetentionPeriod(userID string) time.Duration
	StreamRetention(userID string) []validation.StreamRetention
}

type perTenantDeleteRequestsClient struct {
	client DeleteRequestsClient
	limits Limits
}

func NewPerTenantDeleteRequestsClient(c DeleteRequestsClient, l Limits) DeleteRequestsClient {
	return &perTenantDeleteRequestsClient{
		client: c,
		limits: l,
	}
}

func (c *perTenantDeleteRequestsClient) GetAllDeleteRequestsForUser(ctx context.Context, userID string) ([]DeleteRequest, error) {
	hasDelete, err := validDeletionLimit(c.limits, userID)
	if err != nil {
		return nil, err
	}

	if hasDelete {
		return c.client.GetAllDeleteRequestsForUser(ctx, userID)
	}
	return nil, nil
}

func (c *perTenantDeleteRequestsClient) Stop() {
	c.client.Stop()
}
