package compactor

import (
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/util"
)

// Copied from Thanos, pkg/compact/compact.go.
// Here we aggregate metrics from all finished syncers.
type syncerMetrics struct {
	metaSync                  prometheus.Counter
	metaSyncFailures          prometheus.Counter
	metaSyncDuration          *util.HistogramDataCollector // was prometheus.Histogram before
	metaSyncConsistencyDelay  prometheus.Gauge
	garbageCollectedBlocks    prometheus.Counter
	garbageCollections        prometheus.Counter
	garbageCollectionFailures prometheus.Counter
	garbageCollectionDuration *util.HistogramDataCollector // was prometheus.Histogram before
	compactions               prometheus.Counter
	compactionRunsStarted     prometheus.Counter
	compactionRunsCompleted   prometheus.Counter
	compactionFailures        prometheus.Counter
	verticalCompactions       prometheus.Counter
}

// Copied (and modified with Cortex prefix) from Thanos, pkg/compact/compact.go
// We also ignore "group" label, since we only use a single group.
func newSyncerMetrics(reg prometheus.Registerer) *syncerMetrics {
	var m syncerMetrics

	m.metaSync = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cortex_compactor_meta_syncs_total",
		Help: "TSDB Syncer: Total blocks metadata synchronization attempts.",
	})
	m.metaSyncFailures = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cortex_compactor_meta_sync_failures_total",
		Help: "TSDB Syncer: Total blocks metadata synchronization failures.",
	})
	m.metaSyncDuration = util.NewHistogramDataCollector(prometheus.NewDesc(
		"cortex_compactor_meta_sync_duration_seconds",
		"TSDB Syncer: Duration of the blocks metadata synchronization in seconds.",
		nil, nil))
	m.metaSyncConsistencyDelay = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cortex_compactor_meta_sync_consistency_delay_seconds",
		Help: "TSDB Syncer: Configured consistency delay in seconds.",
	})

	m.garbageCollectedBlocks = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cortex_compactor_garbage_collected_blocks_total",
		Help: "TSDB Syncer: Total number of deleted blocks by compactor.",
	})
	m.garbageCollections = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cortex_compactor_garbage_collection_total",
		Help: "TSDB Syncer: Total number of garbage collection operations.",
	})
	m.garbageCollectionFailures = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cortex_compactor_garbage_collection_failures_total",
		Help: "TSDB Syncer: Total number of failed garbage collection operations.",
	})
	m.garbageCollectionDuration = util.NewHistogramDataCollector(prometheus.NewDesc(
		"cortex_compactor_garbage_collection_duration_seconds",
		"TSDB Syncer: Time it took to perform garbage collection iteration.",
		nil, nil))

	m.compactions = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cortex_compactor_group_compactions_total",
		Help: "TSDB Syncer: Total number of group compaction attempts that resulted in a new block.",
	})
	m.compactionRunsStarted = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cortex_compactor_group_compaction_runs_started_total",
		Help: "TSDB Syncer: Total number of group compaction attempts.",
	})
	m.compactionRunsCompleted = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cortex_compactor_group_compaction_runs_completed_total",
		Help: "TSDB Syncer: Total number of group completed compaction runs. This also includes compactor group runs that resulted with no compaction.",
	})
	m.compactionFailures = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cortex_compactor_group_compactions_failures_total",
		Help: "TSDB Syncer: Total number of failed group compactions.",
	})
	m.verticalCompactions = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cortex_compactor_group_vertical_compactions_total",
		Help: "TSDB Syncer: Total number of group compaction attempts that resulted in a new block based on overlapping blocks.",
	})

	if reg != nil {
		reg.MustRegister(
			m.metaSync,
			m.metaSyncFailures,
			m.metaSyncDuration,
			m.metaSyncConsistencyDelay,
			m.garbageCollectedBlocks,
			m.garbageCollections,
			m.garbageCollectionFailures,
			m.garbageCollectionDuration,
			m.compactions,
			m.compactionRunsStarted,
			m.compactionRunsCompleted,
			m.compactionFailures,
			m.verticalCompactions,
		)
	}
	return &m
}

func (m *syncerMetrics) gatherThanosSyncerMetrics(reg *prometheus.Registry) {
	if m == nil {
		return
	}

	mf, err := reg.Gather()
	if err != nil {
		level.Warn(util.Logger).Log("msg", "failed to gather metrics from syncer registry after compaction", "err", err)
		return
	}

	mfm, err := util.NewMetricFamilyMap(mf)
	if err != nil {
		level.Warn(util.Logger).Log("msg", "failed to gather metrics from syncer registry after compaction", "err", err)
		return
	}

	m.metaSync.Add(mfm.SumCounters("blocks_meta_syncs_total"))
	m.metaSyncFailures.Add(mfm.SumCounters("blocks_meta_sync_failures_total"))
	m.metaSyncDuration.Add(mfm.SumHistograms("blocks_meta_sync_duration_seconds"))
	m.metaSyncConsistencyDelay.Set(mfm.MaxGauges("consistency_delay_seconds"))

	m.garbageCollectedBlocks.Add(mfm.SumCounters("thanos_compact_garbage_collected_blocks_total"))
	m.garbageCollections.Add(mfm.SumCounters("thanos_compact_garbage_collection_total"))
	m.garbageCollectionFailures.Add(mfm.SumCounters("thanos_compact_garbage_collection_failures_total"))
	m.garbageCollectionDuration.Add(mfm.SumHistograms("thanos_compact_garbage_collection_duration_seconds"))

	// These metrics have "group" label, but we sum them all together.
	m.compactions.Add(mfm.SumCounters("thanos_compact_group_compactions_total"))
	m.compactionRunsStarted.Add(mfm.SumCounters("thanos_compact_group_compaction_runs_started_total"))
	m.compactionRunsCompleted.Add(mfm.SumCounters("thanos_compact_group_compaction_runs_completed_total"))
	m.compactionFailures.Add(mfm.SumCounters("thanos_compact_group_compactions_failures_total"))
	m.verticalCompactions.Add(mfm.SumCounters("thanos_compact_group_vertical_compactions_total"))
}
