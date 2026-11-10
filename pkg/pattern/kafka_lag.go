package pattern

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// consumerLagDesc is a gauge which tracks the consumer lag of each
	// partition.
	consumerLagDesc = prometheus.NewDesc(
		"consumer_lag_seconds",
		"The consumer lag of each assigned partition, measured as the age of the last record consumed from Kafka.",
		[]string{"partition"},
		nil,
	)
)

// lagObserverEntry keeps track of the current consumer lag and the time the
// lag was last observed.
type lagObserverEntry struct {
	lag        time.Duration
	observedAt time.Time
}

// effectiveLag returns the lag of the partition as of now. If the partition
// has not been observed for more than a minute it has no new records, so it is
// not lagging and the lag is zero.
func (e lagObserverEntry) effectiveLag(now time.Time) time.Duration {
	if e.lag > 0 && now.Sub(e.observedAt) > time.Minute {
		return 0
	}
	return e.lag
}

// lagObserver keeps track of the partitions assigned and the consumer lag of
// each partition. Observations for partitions that are not assigned are
// ignored, so a late record from a revoked partition cannot re-add its series.
// If a partition has not been observed for more than a minute its lag is
// reported as zero.
type lagObserver struct {
	partitions map[int32]lagObserverEntry
	mtx        sync.RWMutex
}

// newLagObserver returns a new lagObserver.
func newLagObserver(reg prometheus.Registerer) (*lagObserver, error) {
	o := lagObserver{partitions: make(map[int32]lagObserverEntry)}
	if err := reg.Register(&o); err != nil {
		return nil, fmt.Errorf("failed to register metrics: %w", err)
	}
	return &o, nil
}

// Assign assigns the partitions. The lag of each partition is initially zero.
func (o *lagObserver) Assign(partitions []int32) {
	o.mtx.Lock()
	defer o.mtx.Unlock()
	for _, partition := range partitions {
		o.partitions[partition] = lagObserverEntry{}
	}
}

// Revoke deletes the partitions.
func (o *lagObserver) Revoke(partitions []int32) {
	o.mtx.Lock()
	defer o.mtx.Unlock()
	for _, partition := range partitions {
		delete(o.partitions, partition)
	}
}

// Observe records the lag of the partition and when it was observed.
// It does nothing if the partition is not assigned.
func (o *lagObserver) Observe(partition int32, ts time.Time) {
	o.mtx.Lock()
	defer o.mtx.Unlock()
	if entry, ok := o.partitions[partition]; ok {
		now := time.Now()
		entry.lag = now.Sub(ts)
		entry.observedAt = now
		o.partitions[partition] = entry
	}
}

// Describe implements [prometheus.Collector].
func (o *lagObserver) Describe(descs chan<- *prometheus.Desc) {
	descs <- consumerLagDesc
}

// Collect implements [prometheus.Collector].
func (o *lagObserver) Collect(metrics chan<- prometheus.Metric) {
	o.mtx.RLock()
	defer o.mtx.RUnlock()
	now := time.Now()
	for partition, entry := range o.partitions {
		metrics <- prometheus.MustNewConstMetric(
			consumerLagDesc,
			prometheus.GaugeValue,
			entry.effectiveLag(now).Seconds(),
			strconv.FormatInt(int64(partition), 10),
		)
	}
}
