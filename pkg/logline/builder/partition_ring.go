package builder

import (
	"context"
	"fmt"
	"strconv"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// partitionRingName labels the builder's view of the producer partition ring.
//
// It is deliberately distinct from Loki's own watcher name
// ("ingester-partitions") so the two sets of series stay separable, and it is
// exported under the logline_ prefix.
const partitionRingName = "logline-builder-partitions"

// PartitionRingWatcher gives the builder a read-only view of the producer
// partition ring, which it reads to discover active partitions.
//
// TODO: reuse Loki's partition ring watcher instead of running a second one.
//
// initPartitionRing already watches the same ring, on the same KV key, and
// builder.New takes a ring.PartitionRingReader, so it could be handed Loki's
// watcher directly and this type would go away.
//
// It is kept for now so the transition changes no metrics. Loki's watcher is
// named "ingester-partitions" under the loki_ prefix, this one is
// "logline-builder-partitions" under logline_, and this one attaches a delegate
// exporting logline_partition_ring_active_partitions, a per-active-partition
// gauge with no equivalent in Loki. Switching would rename two series and drop
// a third.
//
// Note the delegate cannot simply be attached to Loki's watcher: that watcher
// is shared with the ingester and querier paths.
type PartitionRingWatcher struct {
	services.Service

	kvCfg   kv.Config
	ringKey string
	logger  log.Logger
	reg     prometheus.Registerer

	watcher *ring.PartitionRingWatcher
}

// NewPartitionRingWatcher returns a watcher over the partition ring stored at
// ringKey in the given KV store.
//
// kvCfg is Loki's own partition-ring KV config, so the builder joins the
// cluster's existing memberlist rather than starting a second one. Loki's
// memberlist KV already registers every codec gossiped in the cluster
// (ring, partition ring and analytics), so nothing extra is needed here.
func NewPartitionRingWatcher(kvCfg kv.Config, ringKey string, logger log.Logger, reg prometheus.Registerer) *PartitionRingWatcher {
	svc := &PartitionRingWatcher{
		kvCfg:   kvCfg,
		ringKey: ringKey,
		logger:  logger,
		reg:     reg,
	}
	svc.Service = services.NewBasicService(svc.starting, svc.running, svc.stopping).WithName("builder-partition-ring")
	return svc
}

// starting builds the KV client and the partition-ring watcher. The watcher
// must reach Running before this service does, so the balancer never sees a
// missing watcher.
func (s *PartitionRingWatcher) starting(ctx context.Context) error {
	reg := prometheus.WrapRegistererWithPrefix("logline_", s.reg)

	client, err := kv.NewClient(
		s.kvCfg,
		ring.GetPartitionRingCodec(),
		kv.RegistererWithKVName(reg, partitionRingName+"-watcher"),
		log.With(s.logger, "component", "partition-ring-kv"),
	)
	if err != nil {
		return fmt.Errorf("create partition ring kv client: %w", err)
	}

	s.watcher = ring.NewPartitionRingWatcher(partitionRingName, s.ringKey, client, log.With(s.logger, "component", "partition-ring"), reg)

	s.watcher.WithDelegate(newPartitionRingMetrics(reg, partitionRingName))
	if err := s.watcher.StartAsync(ctx); err != nil {
		return fmt.Errorf("start partition ring watcher: %w", err)
	}
	if err := s.watcher.AwaitRunning(ctx); err != nil {
		return fmt.Errorf("await partition ring watcher running: %w", err)
	}
	return nil
}

func (s *PartitionRingWatcher) running(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

func (s *PartitionRingWatcher) stopping(_ error) error {
	if s.watcher != nil {
		if err := services.StopAndAwaitTerminated(context.Background(), s.watcher); err != nil {
			return err
		}
	}
	return nil
}

func (s *PartitionRingWatcher) PartitionRing() *ring.PartitionRing {
	if s == nil || s.watcher == nil {
		empty, _ := ring.NewPartitionRing(*ring.NewPartitionRingDesc())
		return empty
	}
	return s.watcher.PartitionRing()
}

type partitionRingMetrics struct {
	activePartitions *prometheus.GaugeVec
}

func newPartitionRingMetrics(reg prometheus.Registerer, ringName string) *partitionRingMetrics {
	return &partitionRingMetrics{
		activePartitions: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name:        "partition_ring_active_partitions",
			Help:        "Set to 1 for every partition currently in the Active state. One series per active partition_id; non-active partitions do not appear.",
			ConstLabels: map[string]string{"name": ringName},
		}, []string{"partition"}),
	}
}

// OnPartitionRingChanged updates related metrics when we observe partition ring changes.
func (m *partitionRingMetrics) OnPartitionRingChanged(oldRing, newRing *ring.PartitionRingDesc) {
	// Drop series for partitions that no longer exist in the ring at all.
	for pid := range oldRing.Partitions {
		if _, ok := newRing.Partitions[pid]; !ok {
			m.activePartitions.DeleteLabelValues(strconv.Itoa(int(pid)))
		}
	}
	// Set or clear each current partition based on its state.
	for pid, pDesc := range newRing.Partitions {
		label := strconv.Itoa(int(pid))
		if pDesc.State == ring.PartitionActive {
			m.activePartitions.WithLabelValues(label).Set(1)
		} else {
			m.activePartitions.DeleteLabelValues(label)
		}
	}
}
