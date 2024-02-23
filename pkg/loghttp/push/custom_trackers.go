package push

import (
	"time"

	"github.com/prometheus/prometheus/model/labels"
)

type CustomStreamsTracker interface {

	// ReceivedBytesAdd records ingested bytes by tenant, retention period and labels.
	// TODO: pass context
	ReceivedBytesAdd(tenant string, retentionPeriod time.Duration, labels labels.Labels, value float64)

	// DiscardedBytesAdd records discarded bytes by tenant and labels.
	// TODO: pass context
	DiscardedBytesAdd(tenant, reason string, labels labels.Labels, value float64)
}
