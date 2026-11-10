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

// lagObserver keeps track of the partitions assigned and the consumer lag of
// each partition. Observations for partitions that are not assigned are
// ignored, so a late record from a revoked partition cannot re-add its series.
type lagObserver struct {
	partitions map[int32]time.Duration
	mtx        sync.RWMutex
}

// newLagObserver returns a new lagObserver.
func newLagObserver(reg prometheus.Registerer) (*lagObserver, error) {
	o := lagObserver{partitions: make(map[int32]time.Duration)}
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
		o.partitions[partition] = 0
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

// Observe records the lag of the partition, given the timestamp of the record
// just consumed. It does nothing if the partition is not assigned.
func (o *lagObserver) Observe(partition int32, ts time.Time) {
	o.mtx.Lock()
	defer o.mtx.Unlock()
	if _, ok := o.partitions[partition]; ok {
		o.partitions[partition] = time.Since(ts)
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
	for partition, lag := range o.partitions {
		metrics <- prometheus.MustNewConstMetric(
			consumerLagDesc,
			prometheus.GaugeValue,
			lag.Seconds(),
			strconv.FormatInt(int64(partition), 10),
		)
	}
}
