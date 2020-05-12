package ingester

import (
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util/validation"
)

// userMetricsMetadata allows metric metadata of a tenant to be held by the ingester.
// Metadata is kept as a set as it can come from multiple targets that Prometheus scrapes
// with the same metric name.
type userMetricsMetadata struct {
	limiter *Limiter
	metrics *ingesterMetrics
	userID  string

	mtx              sync.RWMutex
	metricToMetadata map[string]metricMetadataSet
}

func newMetadataMap(l *Limiter, m *ingesterMetrics, userID string) *userMetricsMetadata {
	return &userMetricsMetadata{
		metricToMetadata: map[string]metricMetadataSet{},
		limiter:          l,
		metrics:          m,
		userID:           userID,
	}
}

func (mm *userMetricsMetadata) add(metric string, metadata *client.MetricMetadata) error {
	mm.mtx.Lock()
	defer mm.mtx.Unlock()

	// As we get the set, we also validate two things:
	// 1. The user is allowed to create new metrics to add metadata to.
	// 2. If the metadata set is already present, it hasn't reached the limit of metadata we can append.
	set, ok := mm.metricToMetadata[metric]
	if !ok {
		// Verify that the user can create more metric metadata given we don't have a set for that metric name.
		if err := mm.limiter.AssertMaxMetricsWithMetadataPerUser(mm.userID, len(mm.metricToMetadata)); err != nil {
			validation.DiscardedMetadata.WithLabelValues(mm.userID, perUserMetadataLimit).Inc()
			return makeLimitError(perUserMetadataLimit, err)
		}
		set = metricMetadataSet{}
		mm.metricToMetadata[metric] = set
	}

	if err := mm.limiter.AssertMaxMetadataPerMetric(mm.userID, len(set)); err != nil {
		validation.DiscardedMetadata.WithLabelValues(mm.userID, perMetricMetadataLimit).Inc()
		return makeLimitError(perMetricMetadataLimit, err)
	}

	// if we have seen this metadata before, it is a no-op and we don't need to change our metrics.
	_, ok = set[*metadata]
	if !ok {
		mm.metrics.memMetadata.Inc()
		mm.metrics.memMetadataCreatedTotal.WithLabelValues(mm.userID).Inc()
	}

	mm.metricToMetadata[metric][*metadata] = time.Now()
	return nil
}

func (mm *userMetricsMetadata) purge(deadline time.Time) {
	mm.mtx.Lock()
	defer mm.mtx.Unlock()
	var deleted int
	for m, s := range mm.metricToMetadata {
		deleted += s.purge(deadline)

		if len(s) <= 0 {
			delete(mm.metricToMetadata, m)
		}
	}

	mm.metrics.memMetadata.Sub(float64(deleted))
	mm.metrics.memMetadataRemovedTotal.WithLabelValues(mm.userID).Add(float64(deleted))
}

func (mm *userMetricsMetadata) toClientMetadata() []*client.MetricMetadata {
	mm.mtx.RLock()
	defer mm.mtx.RUnlock()
	r := make([]*client.MetricMetadata, 0, len(mm.metricToMetadata))
	for _, set := range mm.metricToMetadata {
		for m := range set {
			r = append(r, &m)
		}
	}
	return r
}

type metricMetadataSet map[client.MetricMetadata]time.Time

func (mms metricMetadataSet) purge(deadline time.Time) int {
	var deleted int
	for metadata, t := range mms {
		if deadline.After(t) {
			delete(mms, metadata)
			deleted++
		}
	}

	return deleted
}
