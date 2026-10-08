// Package dataobjmetrics folds a finished query's xcap Capture into per-component
// data-object read metrics. It is shared by every process that reads data objects
// (the querier and the index-gateway), so the component attribution is identical
// across them.
package dataobjmetrics

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// Component label values. ComponentStreamsReader also names the xcap region the
// querier's sample store opens, so it is read from other packages too.
const (
	ComponentMetastore     = "metastore"
	ComponentStreamsReader = "streams-reader"
	ComponentLogsReader    = "logs-reader"
	ComponentOther         = "other"
)

// Operation label values for the request counter. Each maps to one per-operation
// stat recorded at the object-store reader layer.
const (
	OperationAttributes = "attributes"
	OperationGet        = "get"
	OperationGetRange   = "get_range"
)

// requestOperations maps each object-store request stat to its operation label.
var requestOperations = []struct {
	operation string
	stat      xcap.Statistic
}{
	{OperationAttributes, dataobj.StatObjectRequestsAttributes},
	{OperationGet, dataobj.StatObjectRequestsGet},
	{OperationGetRange, dataobj.StatObjectRequestsGetRange},
}

// Metrics holds the data-object read metrics for one process.
type Metrics struct {
	// FetchedCompressedBytes counts compressed bytes read from object storage. It is the transfer cost.
	FetchedCompressedBytes *prometheus.CounterVec

	// ProcessedUncompressedBytes counts decompressed row bytes the engine read. It excludes bytes that
	// were fetched but never processed.
	ProcessedUncompressedBytes *prometheus.CounterVec

	// ObjectStoreRequests counts object-store requests, split by the underlying operation.
	ObjectStoreRequests *prometheus.CounterVec
}

// New registers the data-object read metrics on reg. The caller wraps reg with the expected prefix.
func New(reg prometheus.Registerer) *Metrics {
	return &Metrics{
		FetchedCompressedBytes: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "dataobj_fetched_compressed_bytes_total",
			Help: "Compressed bytes read from object storage for data-object queries (head prefetch, section metadata, and page reads).",
		}, []string{"component"}),
		ProcessedUncompressedBytes: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "dataobj_processed_uncompressed_bytes_total",
			Help: "Uncompressed row bytes processed from data objects.",
		}, []string{"component"}),
		ObjectStoreRequests: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "dataobj_object_store_requests_total",
			Help: "Object-store requests issued for data-object queries, by component and operation (attributes, get, get_range).",
		}, []string{"component", "operation"}),
	}
}

// Record folds a finished query's counts into the per-component counters.
func (m *Metrics) Record(capture *xcap.Capture) {
	if m == nil || capture == nil {
		return
	}

	regions := capture.Regions()
	byID := make(map[xcap.ID]*xcap.Region, len(regions))
	for _, r := range regions {
		byID[r.ID()] = r
	}
	rootComponent := func(r *xcap.Region) string {
		for {
			pid := r.ParentID()
			if pid.IsZero() {
				break
			}
			parent, ok := byID[pid]
			if !ok {
				break
			}
			r = parent
		}
		return componentForRootRegion(r.Name())
	}

	// Attribute each region's counts to its root region, not its own name. The metastore's index
	// reader opens nested streams and pointers regions; rooting them keeps their counts under
	// "metastore" instead of scattering into "other".
	type totals struct {
		fetched, processed int64
		requests           map[string]int64
	}
	perComponent := map[string]*totals{}
	for _, r := range regions {
		comp := rootComponent(r)
		t := perComponent[comp]
		if t == nil {
			t = &totals{requests: map[string]int64{}}
			perComponent[comp] = t
		}
		t.fetched += regionValue(r, dataobj.StatObjectBytesDownloaded)
		t.processed += regionValue(r, dataobj.StatDatasetPrimaryRowBytes) + regionValue(r, dataobj.StatDatasetSecondaryRowBytes)
		for _, ro := range requestOperations {
			t.requests[ro.operation] += regionValue(r, ro.stat)
		}
	}

	for comp, t := range perComponent {
		if t.fetched > 0 {
			m.FetchedCompressedBytes.WithLabelValues(comp).Add(float64(t.fetched))
		}
		if t.processed > 0 {
			m.ProcessedUncompressedBytes.WithLabelValues(comp).Add(float64(t.processed))
		}
		for op, n := range t.requests {
			if n > 0 {
				m.ObjectStoreRequests.WithLabelValues(comp, op).Add(float64(n))
			}
		}
	}
}

// componentForRootRegion maps a root region name to its component label.
func componentForRootRegion(rootName string) string {
	// streams-reader and logs-reader match their region names exactly. A prefix match on "streams"
	// would wrongly pull the metastore's nested streams.Reader.* reads into streams-reader. metastore
	// matches by prefix: its root span is "metastore.Sections".
	switch {
	case rootName == ComponentStreamsReader:
		return ComponentStreamsReader
	case rootName == logs.RegionRead:
		return ComponentLogsReader
	case strings.HasPrefix(rootName, ComponentMetastore):
		return ComponentMetastore
	default:
		return ComponentOther
	}
}

// regionValue returns the region's own aggregated value for stat, or 0 if it recorded none.
func regionValue(r *xcap.Region, stat xcap.Statistic) int64 {
	for _, obs := range r.Observations() {
		if obs.Statistic.Key() == stat.Key() {
			if v, ok := obs.Value.(int64); ok {
				return v
			}
		}
	}
	return 0
}
