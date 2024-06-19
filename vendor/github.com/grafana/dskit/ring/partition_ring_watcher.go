package ring

import (
	"context"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/services"
)

// PartitionRingWatcher watches the partitions ring for changes in the KV store.
type PartitionRingWatcher struct {
	services.Service

	key    string
	kv     kv.Client
	logger log.Logger

	ringMx sync.Mutex
	ring   *PartitionRing

	// Metrics.
	numPartitionsGaugeVec *prometheus.GaugeVec
}

func NewPartitionRingWatcher(name, key string, kv kv.Client, logger log.Logger, reg prometheus.Registerer) *PartitionRingWatcher {
	r := &PartitionRingWatcher{
		key:    key,
		kv:     kv,
		logger: logger,
		ring:   NewPartitionRing(*NewPartitionRingDesc()),
		numPartitionsGaugeVec: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name:        "partition_ring_partitions",
			Help:        "Number of partitions by state in the partitions ring.",
			ConstLabels: map[string]string{"name": name},
		}, []string{"state"}),
	}

	r.Service = services.NewBasicService(r.starting, r.loop, nil).WithName("partitions-ring-watcher")
	return r
}

func (w *PartitionRingWatcher) starting(ctx context.Context) error {
	// Get the initial ring state so that, as soon as the service will be running, the in-memory
	// ring would be already populated and there's no race condition between when the service is
	// running and the WatchKey() callback is called for the first time.
	value, err := w.kv.Get(ctx, w.key)
	if err != nil {
		return errors.Wrap(err, "unable to initialise ring state")
	}

	if value == nil {
		level.Info(w.logger).Log("msg", "partition ring doesn't exist in KV store yet")
		value = NewPartitionRingDesc()
	}

	w.updatePartitionRing(value.(*PartitionRingDesc))
	return nil
}

func (w *PartitionRingWatcher) loop(ctx context.Context) error {
	w.kv.WatchKey(ctx, w.key, func(value interface{}) bool {
		if value == nil {
			level.Info(w.logger).Log("msg", "partition ring doesn't exist in KV store yet")
			return true
		}

		w.updatePartitionRing(value.(*PartitionRingDesc))
		return true
	})
	return nil
}

func (w *PartitionRingWatcher) updatePartitionRing(desc *PartitionRingDesc) {
	newRing := NewPartitionRing(*desc)

	w.ringMx.Lock()
	w.ring = newRing
	w.ringMx.Unlock()

	// Update metrics.
	for state, count := range desc.countPartitionsByState() {
		w.numPartitionsGaugeVec.WithLabelValues(state.CleanName()).Set(float64(count))
	}
}

// PartitionRing returns the most updated snapshot of the PartitionRing. The returned instance
// is immutable and will not be updated if new changes are done to the ring.
func (w *PartitionRingWatcher) PartitionRing() *PartitionRing {
	w.ringMx.Lock()
	defer w.ringMx.Unlock()

	return w.ring
}
