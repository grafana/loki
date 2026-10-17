package dataobjread

import (
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/readbudget"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// The component label values of the object-store metrics. Each names the phase of the read path that
// issued the requests. The values are part of the metrics API.
const (
	componentMetastore     = "metastore"
	componentStreamsReader = "streams-reader"
	componentLogsReader    = "logs-reader"
	componentOther         = "other"
)

// regionStreamsReader names the xcap region of the planner's object opens and streams-section reads.
// It is also the component label of those reads. Without it they run outside any region, and a
// request outside a region is not counted.
const regionStreamsReader = componentStreamsReader

var components = []string{componentMetastore, componentStreamsReader, componentLogsReader, componentOther}

// objectStoreOperation ties an operation label to the statistics that count the requests and the
// failures of that operation.
type objectStoreOperation struct {
	label    string
	requests xcap.Statistic
	failures xcap.Statistic
}

var objectStoreOperations = []objectStoreOperation{
	{"attributes", dataobj.StatObjectRequestsAttributes, dataobj.StatObjectRequestFailuresAttributes},
	{"get", dataobj.StatObjectRequestsGet, dataobj.StatObjectRequestFailuresGet},
	{"get_range", dataobj.StatObjectRequestsGetRange, dataobj.StatObjectRequestFailuresGetRange},
}

// Metrics are the read path's metrics. One set is shared by every query a store serves.
type Metrics struct {
	taskWaitSeconds prometheus.Counter
	taskScanSeconds prometheus.Counter

	fetchedCompressedBytes    *prometheus.CounterVec
	objectStoreRequests       *prometheus.CounterVec
	objectStoreRequestsFailed *prometheus.CounterVec

	readBudgetWaitSeconds prometheus.Counter
	sectionReservedBytes  prometheus.Histogram
}

// NewMetrics registers the read path's metrics on reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		taskWaitSeconds: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_querier_dataobj_read_task_wait_seconds_total",
			Help: "Total time data-object section readers spent waiting for the read planner to produce the next task. Meaningful as a share of loki_querier_dataobj_read_task_scan_seconds_total, not on its own: a rising share means section resolution cannot keep up with scanning.",
		}),
		taskScanSeconds: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_querier_dataobj_read_task_scan_seconds_total",
			Help: "Total time data-object section readers spent scanning logs sections, including the time they waited for the read budget.",
		}),
		fetchedCompressedBytes: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_querier_dataobj_fetched_compressed_bytes_total",
			Help: "Compressed bytes read from object storage for data-object queries, by read-path component. Includes the table of contents, the head prefetch of each object, section metadata and page reads.",
		}, []string{"component"}),
		objectStoreRequests: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_querier_dataobj_object_store_requests_total",
			Help: "Object-store requests issued for data-object queries, by read-path component and operation (attributes, get, get_range). Counts every request when it is issued, whatever its outcome.",
		}, []string{"component", "operation"}),
		objectStoreRequestsFailed: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_querier_dataobj_object_store_requests_failed_total",
			Help: "Object-store requests that failed for data-object queries, by read-path component and operation (attributes, get, get_range). A request for an object that does not exist (not found) or a canceled request is not a failure. Every failed request is also counted in loki_querier_dataobj_object_store_requests_total.",
		}, []string{"component", "operation"}),
		readBudgetWaitSeconds: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_querier_dataobj_read_budget_wait_seconds_total",
			Help: "Total time data-object section reads waited for room in the read budget of their query, including the wait of reads that gave up when their query ended. It stays at zero while the budget is off.",
		}),
		sectionReservedBytes: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "loki_querier_dataobj_section_reserved_bytes",
			Help:    "Compressed bytes that a data-object section read reserved from the read budget of its query before it downloaded them. It has no observations while the budget is off.",
			Buckets: prometheus.ExponentialBuckets(1<<16, 4, 7),
		}),
	}

	// Create every series at 0, so rate and increase see the first increment of a series.
	for _, component := range components {
		m.fetchedCompressedBytes.WithLabelValues(component)
		for _, op := range objectStoreOperations {
			m.objectStoreRequests.WithLabelValues(component, op.label)
			m.objectStoreRequestsFailed.WithLabelValues(component, op.label)
		}
	}

	return m
}

var _ readbudget.Observer = (*Metrics)(nil)

// Admitted records one reservation that a read budget admitted, with its bytes and the time it
// waited. It is safe for concurrent use. A nil Metrics records nothing.
func (m *Metrics) Admitted(bytes int64, waited time.Duration) {
	if m == nil {
		return
	}

	m.sectionReservedBytes.Observe(float64(bytes))
	m.readBudgetWaitSeconds.Add(waited.Seconds())
}

// Withdrawn records the time that a reservation waited before its context ended. The wait counts
// toward the same total as an admitted wait, so a query that times out in the budget shows it.
func (m *Metrics) Withdrawn(waited time.Duration) {
	if m == nil {
		return
	}

	m.readBudgetWaitSeconds.Add(waited.Seconds())
}

// Record adds the object-store requests and downloaded bytes of a finished query to the
// per-component counters. The request counter includes the failed requests, and the failed counter
// holds those alone.
//
// It attributes each region to the component of its root region, not of its own name. The metastore
// reads index objects through nested postings and streams regions, and the root keeps their requests
// under the metastore component.
func (m *Metrics) Record(capture *xcap.Capture) {
	if m == nil || capture == nil {
		return
	}

	type totals struct {
		fetchedCompressedBytes int64
		requestsByOperation    []int64
		failuresByOperation    []int64
	}
	byComponent := map[string]*totals{}

	for root, tree := range capture.RootRegions() {
		component := componentForRootRegion(root.Name())
		t, ok := byComponent[component]
		if !ok {
			t = &totals{
				requestsByOperation: make([]int64, len(objectStoreOperations)),
				failuresByOperation: make([]int64, len(objectStoreOperations)),
			}
			byComponent[component] = t
		}

		for _, r := range tree {
			t.fetchedCompressedBytes += regionInt64(r, dataobj.StatObjectBytesDownloaded)
			for i, op := range objectStoreOperations {
				t.requestsByOperation[i] += regionInt64(r, op.requests)
				t.failuresByOperation[i] += regionInt64(r, op.failures)
			}
		}
	}

	for component, t := range byComponent {
		if t.fetchedCompressedBytes > 0 {
			m.fetchedCompressedBytes.WithLabelValues(component).Add(float64(t.fetchedCompressedBytes))
		}
		for i, op := range objectStoreOperations {
			if t.requestsByOperation[i] > 0 {
				m.objectStoreRequests.WithLabelValues(component, op.label).Add(float64(t.requestsByOperation[i]))
			}
			if t.failuresByOperation[i] > 0 {
				m.objectStoreRequestsFailed.WithLabelValues(component, op.label).Add(float64(t.failuresByOperation[i]))
			}
		}
	}
}

// componentForRootRegion maps the name of a root region to its component label.
func componentForRootRegion(name string) string {
	switch {
	case name == regionStreamsReader:
		return componentStreamsReader
	case name == logs.RegionRead:
		return componentLogsReader
	case strings.HasPrefix(name, componentMetastore):
		return componentMetastore
	default:
		return componentOther
	}
}

// regionInt64 returns the integer value region recorded for stat, or 0 if it recorded none.
func regionInt64(region *xcap.Region, stat xcap.Statistic) int64 {
	value, _ := region.ObservationForStatistic(stat).Int64()
	return value
}
