package dataobjread

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/dataobj/bucketstat"
	"github.com/grafana/loki/v3/pkg/dataobj/metadata"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// The component label values of the object-store and metadata metrics. Each names the part of the
// read path that issued the reads. The values are part of the metrics API.
const (
	componentMetastore     = "metastore"
	componentStreamsReader = "streams-reader"
	componentLogsReader    = "logs-reader"
	componentOther         = "other"
)

// The source and phase label values of the metadata metrics. The values are part of the metrics API.
const (
	metadataSourceCache   = "cache"
	metadataSourceStorage = "storage"

	metadataPhasePrefetch = "prefetch"
	metadataPhaseOnDemand = "on_demand"
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
	{"attributes", bucketstat.StatObjectRequestsAttributes, bucketstat.StatObjectRequestFailuresAttributes},
	{"get", bucketstat.StatObjectRequestsGet, bucketstat.StatObjectRequestFailuresGet},
	{"get_range", bucketstat.StatObjectRequestsGetRange, bucketstat.StatObjectRequestFailuresGetRange},
}

// metadataStat ties the statistics that count the bytes and the requests of one source of metadata,
// in one phase, to the source and phase labels of the metadata metrics.
//
// The metrics leave out the bytes that a prefetched range served to a section read. The prefetch phase
// already counts them, so the metrics would count them twice.
type metadataStat struct {
	source   string
	phase    string
	bytes    xcap.Statistic
	requests xcap.Statistic
}

// metadataStats lists the series of the metadata metrics. The prefetch phase is the work of the
// metadata prefetcher. The on_demand phase is every other read: the reads of a section, the reads of an
// open that does not use the prefetcher, the metadata cache, and the reads of a table of contents.
var metadataStats = []metadataStat{
	{metadataSourceCache, metadataPhasePrefetch, metadata.StatMetadataPrefetchBytesFromCache, metadata.StatMetadataPrefetchRequestsToCache},
	{metadataSourceStorage, metadataPhasePrefetch, metadata.StatMetadataPrefetchBytesFromStorage, metadata.StatMetadataPrefetchRequestsToStorage},
	{metadataSourceCache, metadataPhaseOnDemand, metadata.StatMetadataBytesFromCache, metadata.StatMetadataRequestsToCache},
	{metadataSourceStorage, metadataPhaseOnDemand, metadata.StatMetadataBytesFromStorage, metadata.StatMetadataRequestsToStorage},
}

// Metrics are the read path's metrics. One set is shared by every query a store serves.
type Metrics struct {
	taskWaitSeconds prometheus.Counter
	taskScanSeconds prometheus.Counter

	fetchedCompressedBytes    *prometheus.CounterVec
	objectStoreRequests       *prometheus.CounterVec
	objectStoreRequestsFailed *prometheus.CounterVec

	metadataBytes    *prometheus.CounterVec
	metadataRequests *prometheus.CounterVec
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
			Help: "Total time data-object section readers spent scanning logs sections.",
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
		metadataBytes: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_querier_dataobj_metadata_bytes_total",
			Help: "Bytes of metadata that data-object queries took from the cache or from object storage, by read-path component, source (cache, storage) and phase (prefetch, on_demand). Cache bytes are the length of the metadata that the cache returned. Storage bytes are the bytes downloaded, and can include bytes that the query does not keep. A read that memory serves is not counted.",
		}, []string{"component", "source", "phase"}),
		metadataRequests: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_querier_dataobj_metadata_requests_total",
			Help: "Requests that data-object queries sent to the cache or to object storage to read metadata, by read-path component, source (cache, storage) and phase (prefetch, on_demand). A cache request is one lookup, whatever the number of keys. A request counts when it is sent, whether or not it fails. Every storage request is also counted in loki_querier_dataobj_object_store_requests_total.",
		}, []string{"component", "source", "phase"}),
	}

	// Create every series at 0, so rate and increase see the first increment of a series.
	for _, component := range components {
		m.fetchedCompressedBytes.WithLabelValues(component)
		for _, s := range metadataStats {
			m.metadataBytes.WithLabelValues(component, s.source, s.phase)
			m.metadataRequests.WithLabelValues(component, s.source, s.phase)
		}
		for _, op := range objectStoreOperations {
			m.objectStoreRequests.WithLabelValues(component, op.label)
			m.objectStoreRequestsFailed.WithLabelValues(component, op.label)
		}
	}

	return m
}

// Record adds the object-store requests, downloaded bytes, metadata reads and metadata prefetches of
// a finished query to the per-component counters. The object-store request counter includes the
// failed requests, and the failed counter holds those alone.
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
		metadataBytes          []int64
		metadataRequests       []int64
		requestsByOperation    []int64
		failuresByOperation    []int64
	}
	byComponent := map[string]*totals{}

	for root, tree := range capture.RootRegions() {
		component := componentForRootRegion(root.Name())
		t, ok := byComponent[component]
		if !ok {
			t = &totals{
				metadataBytes:       make([]int64, len(metadataStats)),
				metadataRequests:    make([]int64, len(metadataStats)),
				requestsByOperation: make([]int64, len(objectStoreOperations)),
				failuresByOperation: make([]int64, len(objectStoreOperations)),
			}
			byComponent[component] = t
		}

		for _, r := range tree {
			t.fetchedCompressedBytes += r.Int64Value(bucketstat.StatObjectBytesDownloaded)
			for i, s := range metadataStats {
				t.metadataBytes[i] += r.Int64Value(s.bytes)
				t.metadataRequests[i] += r.Int64Value(s.requests)
			}
			for i, op := range objectStoreOperations {
				t.requestsByOperation[i] += r.Int64Value(op.requests)
				t.failuresByOperation[i] += r.Int64Value(op.failures)
			}
		}
	}

	for component, t := range byComponent {
		if t.fetchedCompressedBytes > 0 {
			m.fetchedCompressedBytes.WithLabelValues(component).Add(float64(t.fetchedCompressedBytes))
		}
		for i, s := range metadataStats {
			if t.metadataBytes[i] > 0 {
				m.metadataBytes.WithLabelValues(component, s.source, s.phase).Add(float64(t.metadataBytes[i]))
			}
			if t.metadataRequests[i] > 0 {
				m.metadataRequests.WithLabelValues(component, s.source, s.phase).Add(float64(t.metadataRequests[i]))
			}
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
