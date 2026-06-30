package pattern

import (
	"context"
	"fmt"
	"strconv"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/dns"
	"github.com/grafana/dskit/kv/codec"
	"github.com/grafana/dskit/kv/memberlist"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/analytics"
)

type PartitionRingWatcher struct {
	services.Service

	cfg    RingConfig
	logger log.Logger
	reg    prometheus.Registerer

	kvInit  *memberlist.KVInitService
	watcher *ring.PartitionRingWatcher
}

func NewPartitionRingWatcher(cfg RingConfig, logger log.Logger, reg prometheus.Registerer) *PartitionRingWatcher {
	svc := &PartitionRingWatcher{
		cfg:    cfg,
		logger: logger,
		reg:    reg,
	}
	svc.Service = services.NewBasicService(svc.starting, svc.running, svc.stopping).WithName("builder-partition-ring")
	return svc
}

// starting initialises the memberlist KV and partition-ring watcher. Both
// must reach the Running state before this service is considered Running,
// so the balancer never sees a missing watcher.
func (s *PartitionRingWatcher) starting(ctx context.Context) error {
	memberlistCfg := s.cfg.Memberlist
	memberlistCfg.WatchPrefixBufferSize = s.cfg.WatcherBufferSize
	// Register every codec used by other gossip participants in the
	// shared Loki memberlist cluster, even ones we don't read:
	//
	//   - partitionRingDesc — the partition ring we actually consume.
	//   - ringDesc          — Loki's standard rings (parquet/ring,
	//                         rulers/ring, collectors/querier, ...).
	//   - usagestats.jsonCodec — Loki analytics' usage stats seed,
	//                         gossiped on keys like
	//                         "partition-ingesters/usagestats_token".
	//
	// Without each codec, dskit logs
	//   "failed to parse remote state: unknown codec for key"
	// at ERROR on every gossip cycle and silently drops the value.
	// Registering them makes the builder a well-behaved gossip member:
	// it still doesn't *use* the foreign payloads, but memberlist can
	// decode and re-broadcast them and the noise disappears. This is
	// the same codec set log-template-service registers.
	memberlistCfg.Codecs = []codec.Codec{
		ring.GetPartitionRingCodec(),
		ring.GetCodec(),
		analytics.JSONCodec,
	}

	resolver := dns.NewProvider(dns.GolangResolverType, 0, log.With(s.logger, "component", "memberlist-dns"), s.reg)
	s.kvInit = memberlist.NewKVInitService(&memberlistCfg, log.With(s.logger, "component", "memberlist-kv"), resolver, s.reg)
	if err := s.kvInit.StartAsync(ctx); err != nil {
		return fmt.Errorf("start memberlist kv init service: %w", err)
	}
	if err := s.kvInit.AwaitRunning(ctx); err != nil {
		return fmt.Errorf("await memberlist kv init service running: %w", err)
	}

	kvStore, err := s.kvInit.GetMemberlistKV()
	if err != nil {
		return fmt.Errorf("get memberlist kv: %w", err)
	}
	client, err := memberlist.NewClient(kvStore, ring.GetPartitionRingCodec())
	if err != nil {
		return fmt.Errorf("create memberlist kv client: %w", err)
	}

	const partitionRingName = "pattern-ingester-partitions"
	s.watcher = ring.NewPartitionRingWatcher(partitionRingName, s.cfg.Key, client, log.With(s.logger, "component", "partition-ring"), s.reg)

	s.watcher.WithDelegate(newPartitionRingMetrics(s.reg))
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
	if s.kvInit != nil {
		if err := services.StopAndAwaitTerminated(context.Background(), s.kvInit); err != nil {
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

func newPartitionRingMetrics(reg prometheus.Registerer) *partitionRingMetrics {
	return &partitionRingMetrics{
		activePartitions: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "partition_ring_active_partitions",
			Help: "Set to 1 for every partition currently in the Active state. One series per active partition_id; non-active partitions do not appear.",
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
