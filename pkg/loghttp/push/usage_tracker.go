package push

import (
	"context"
	"time"

	"github.com/prometheus/prometheus/model/labels"
)

type UsageTracker interface {

	// ReceivedBytesAdd records ingested bytes by tenant, retention period and labels.
	ReceivedBytesAdd(ctx context.Context, tenant string, retentionPeriod time.Duration, labels labels.Labels, value float64)

	// DiscardedBytesAdd records discarded bytes by tenant and labels.
	DiscardedBytesAdd(ctx context.Context, tenant, reason string, labels labels.Labels, value float64)
}
