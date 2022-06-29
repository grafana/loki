package groupcache_exporter

import (
	"github.com/prometheus/client_golang/prometheus"
)

type Exporter struct {
	groups []GroupStatistics

	groupGets           *prometheus.Desc
	groupCacheHits      *prometheus.Desc
	groupPeerLoads      *prometheus.Desc
	groupPeerErrors     *prometheus.Desc
	groupLoads          *prometheus.Desc
	groupLoadsDeduped   *prometheus.Desc
	groupLocalLoads     *prometheus.Desc
	groupLocalLoadErrs  *prometheus.Desc
	groupServerRequests *prometheus.Desc
	cacheBytes          *prometheus.Desc
	cacheItems          *prometheus.Desc
	cacheGets           *prometheus.Desc
	cacheHits           *prometheus.Desc
	cacheEvictions      *prometheus.Desc
}

type GroupStatistics interface {
	// Name returns the group's name
	Name() string

	// Gets represents any Get request, including from peers
	Gets() int64
	// CacheHits represents either cache was good
	CacheHits() int64
	// GetFromPeersLatencyLower represents slowest duration to request value from peers
	GetFromPeersLatencyLower() int64
	// PeerLoads represents either remote load or remote cache hit (not an error)
	PeerLoads() int64
	// PeerErrors represents a count of errors from peers
	PeerErrors() int64
	// Loads represents (gets - cacheHits)
	Loads() int64
	// LoadsDeduped represents after singleflight
	LoadsDeduped() int64
	// LocalLoads represents total good local loads
	LocalLoads() int64
	// LocalLoadErrs represents total bad local loads
	LocalLoadErrs() int64
	// ServerRequests represents gets that came over the network from peers
	ServerRequests() int64

	MainCacheItems() int64
	MainCacheBytes() int64
	MainCacheGets() int64
	MainCacheHits() int64
	MainCacheEvictions() int64

	HotCacheItems() int64
	HotCacheBytes() int64
	HotCacheGets() int64
	HotCacheHits() int64
	HotCacheEvictions() int64
}

func NewExporter(labels map[string]string, groups ...GroupStatistics) *Exporter {
	return &Exporter{
		groups: groups,
		groupGets: prometheus.NewDesc(
			"gets_total",
			"todo",
			[]string{"group"},
			labels,
		),
		groupCacheHits: prometheus.NewDesc(
			"hits_total",
			"todo",
			[]string{"group"},
			labels,
		),
		groupPeerLoads: prometheus.NewDesc(
			"peer_loads_total",
			"todo",
			[]string{"group"},
			labels,
		),
		groupPeerErrors: prometheus.NewDesc(
			"peer_errors_total",
			"todo",
			[]string{"group"},
			labels,
		),
		groupLoads: prometheus.NewDesc(
			"loads_total",
			"todo",
			[]string{"group"},
			labels,
		),
		groupLoadsDeduped: prometheus.NewDesc(
			"loads_deduped_total",
			"todo",
			[]string{"group"},
			labels,
		),
		groupLocalLoads: prometheus.NewDesc(
			"local_load_total",
			"todo",
			[]string{"group"},
			labels,
		),
		groupLocalLoadErrs: prometheus.NewDesc(
			"local_load_errs_total",
			"todo",
			[]string{"group"},
			labels,
		),
		groupServerRequests: prometheus.NewDesc(
			"server_requests_total",
			"todo",
			[]string{"group"},
			labels,
		),
		cacheBytes: prometheus.NewDesc(
			"cache_bytes",
			"todo",
			[]string{"group", "type"},
			labels,
		),
		cacheItems: prometheus.NewDesc(
			"cache_items",
			"todo",
			[]string{"group", "type"},
			labels,
		),
		cacheGets: prometheus.NewDesc(
			"cache_gets_total",
			"todo",
			[]string{"group", "type"},
			labels,
		),
		cacheHits: prometheus.NewDesc(
			"cache_hits_total",
			"todo",
			[]string{"group", "type"},
			labels,
		),
		cacheEvictions: prometheus.NewDesc(
			"cache_evictions_total",
			"todo",
			[]string{"group", "type"},
			labels,
		),
	}
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.groupGets
	ch <- e.groupCacheHits
	ch <- e.groupPeerLoads
	ch <- e.groupPeerErrors
	ch <- e.groupLoads
	ch <- e.groupLoadsDeduped
	ch <- e.groupLocalLoads
	ch <- e.groupLocalLoadErrs
	ch <- e.groupServerRequests
	ch <- e.cacheBytes
	ch <- e.cacheItems
	ch <- e.cacheGets
	ch <- e.cacheHits
	ch <- e.cacheEvictions
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	for _, group := range e.groups {
		e.collectFromGroup(ch, group)
	}
}

func (e *Exporter) collectFromGroup(ch chan<- prometheus.Metric, stats GroupStatistics) {
	e.collectStats(ch, stats)
	e.collectCacheStats(ch, stats)
}

func (e *Exporter) collectStats(ch chan<- prometheus.Metric, stats GroupStatistics) {
	ch <- prometheus.MustNewConstMetric(e.groupGets, prometheus.CounterValue, float64(stats.Gets()), stats.Name())
	ch <- prometheus.MustNewConstMetric(e.groupCacheHits, prometheus.CounterValue, float64(stats.CacheHits()), stats.Name())
	ch <- prometheus.MustNewConstMetric(e.groupPeerLoads, prometheus.CounterValue, float64(stats.PeerLoads()), stats.Name())
	ch <- prometheus.MustNewConstMetric(e.groupPeerErrors, prometheus.CounterValue, float64(stats.PeerErrors()), stats.Name())
	ch <- prometheus.MustNewConstMetric(e.groupLoads, prometheus.CounterValue, float64(stats.Loads()), stats.Name())
	ch <- prometheus.MustNewConstMetric(e.groupLoadsDeduped, prometheus.CounterValue, float64(stats.LoadsDeduped()), stats.Name())
	ch <- prometheus.MustNewConstMetric(e.groupLocalLoads, prometheus.CounterValue, float64(stats.LocalLoads()), stats.Name())
	ch <- prometheus.MustNewConstMetric(e.groupLocalLoadErrs, prometheus.CounterValue, float64(stats.LocalLoadErrs()), stats.Name())
	ch <- prometheus.MustNewConstMetric(e.groupServerRequests, prometheus.CounterValue, float64(stats.ServerRequests()), stats.Name())
}

func (e *Exporter) collectCacheStats(ch chan<- prometheus.Metric, stats GroupStatistics) {
	ch <- prometheus.MustNewConstMetric(e.cacheItems, prometheus.GaugeValue, float64(stats.MainCacheItems()), stats.Name(), "main")
	ch <- prometheus.MustNewConstMetric(e.cacheBytes, prometheus.GaugeValue, float64(stats.MainCacheBytes()), stats.Name(), "main")
	ch <- prometheus.MustNewConstMetric(e.cacheGets, prometheus.CounterValue, float64(stats.MainCacheGets()), stats.Name(), "main")
	ch <- prometheus.MustNewConstMetric(e.cacheHits, prometheus.CounterValue, float64(stats.MainCacheHits()), stats.Name(), "main")
	ch <- prometheus.MustNewConstMetric(e.cacheEvictions, prometheus.CounterValue, float64(stats.MainCacheEvictions()), stats.Name(), "main")

	ch <- prometheus.MustNewConstMetric(e.cacheItems, prometheus.GaugeValue, float64(stats.HotCacheItems()), stats.Name(), "hot")
	ch <- prometheus.MustNewConstMetric(e.cacheBytes, prometheus.GaugeValue, float64(stats.HotCacheBytes()), stats.Name(), "hot")
	ch <- prometheus.MustNewConstMetric(e.cacheGets, prometheus.CounterValue, float64(stats.HotCacheGets()), stats.Name(), "hot")
	ch <- prometheus.MustNewConstMetric(e.cacheHits, prometheus.CounterValue, float64(stats.HotCacheHits()), stats.Name(), "hot")
	ch <- prometheus.MustNewConstMetric(e.cacheEvictions, prometheus.CounterValue, float64(stats.HotCacheEvictions()), stats.Name(), "hot")
}
