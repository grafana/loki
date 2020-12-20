package storegateway

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/util"
)

// This struct aggregates metrics exported by Thanos MetaFetcher
// and re-exports those aggregates as Cortex metrics.
type MetadataFetcherMetrics struct {
	// Maps userID -> registry
	regsMu sync.Mutex
	regs   map[string]*prometheus.Registry

	// Exported metrics, gathered from Thanos MetaFetcher
	syncs                *prometheus.Desc
	syncFailures         *prometheus.Desc
	syncDuration         *prometheus.Desc
	syncConsistencyDelay *prometheus.Desc
	synced               *prometheus.Desc

	// Ignored:
	// blocks_meta_modified
	// blocks_meta_base_syncs_total
}

func NewMetadataFetcherMetrics() *MetadataFetcherMetrics {
	return &MetadataFetcherMetrics{
		regs: map[string]*prometheus.Registry{},

		syncs: prometheus.NewDesc(
			"cortex_blocks_meta_syncs_total",
			"Total blocks metadata synchronization attempts",
			nil, nil),
		syncFailures: prometheus.NewDesc(
			"cortex_blocks_meta_sync_failures_total",
			"Total blocks metadata synchronization failures",
			nil, nil),
		syncDuration: prometheus.NewDesc(
			"cortex_blocks_meta_sync_duration_seconds",
			"Duration of the blocks metadata synchronization in seconds",
			nil, nil),
		syncConsistencyDelay: prometheus.NewDesc(
			"cortex_blocks_meta_sync_consistency_delay_seconds",
			"Configured consistency delay in seconds.",
			nil, nil),
		synced: prometheus.NewDesc(
			"cortex_blocks_meta_synced",
			"Reflects current state of synced blocks (over all tenants).",
			[]string{"state"}, nil),
	}
}

func (m *MetadataFetcherMetrics) AddUserRegistry(user string, reg *prometheus.Registry) {
	m.regsMu.Lock()
	m.regs[user] = reg
	m.regsMu.Unlock()
}

func (m *MetadataFetcherMetrics) registries() map[string]*prometheus.Registry {
	regs := map[string]*prometheus.Registry{}

	m.regsMu.Lock()
	defer m.regsMu.Unlock()
	for uid, r := range m.regs {
		regs[uid] = r
	}

	return regs
}

func (m *MetadataFetcherMetrics) Describe(out chan<- *prometheus.Desc) {

	out <- m.syncs
	out <- m.syncFailures
	out <- m.syncDuration
	out <- m.syncConsistencyDelay
	out <- m.synced
}

func (m *MetadataFetcherMetrics) Collect(out chan<- prometheus.Metric) {
	data := util.BuildMetricFamiliesPerUserFromUserRegistries(m.registries())

	data.SendSumOfCounters(out, m.syncs, "blocks_meta_syncs_total")
	data.SendSumOfCounters(out, m.syncFailures, "blocks_meta_sync_failures_total")
	data.SendSumOfHistograms(out, m.syncDuration, "blocks_meta_sync_duration_seconds")
	data.SendMaxOfGauges(out, m.syncConsistencyDelay, "consistency_delay_seconds")
	data.SendSumOfGaugesWithLabels(out, m.synced, "blocks_meta_synced", "state")
}
