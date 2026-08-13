package querier

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/xcap"
)

const (
	dataObjComponentMetastore     = "metastore"
	dataObjComponentStreamsReader = "streams-reader"
	dataObjComponentLogsReader    = "logs-reader"
	dataObjComponentOther         = "other"
)

// dataObjMetrics holds the querier's data-object read metrics.
type dataObjMetrics struct {
	// fetchedCompressedBytes counts compressed bytes read from object storage. It is the transfer cost.
	fetchedCompressedBytes *prometheus.CounterVec

	// processedUncompressedBytes counts decompressed row bytes the engine read. It excludes bytes that
	// were fetched but never processed.
	processedUncompressedBytes *prometheus.CounterVec
}

// newDataObjMetrics registers the data-object read metrics.
func newDataObjMetrics(reg prometheus.Registerer) *dataObjMetrics {
	return &dataObjMetrics{
		fetchedCompressedBytes: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_querier_dataobj_fetched_compressed_bytes_total",
			Help: "Compressed bytes the querier read from object storage for data-object queries (head prefetch, section metadata, and page reads).",
		}, []string{"component"}),
		processedUncompressedBytes: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_querier_dataobj_processed_uncompressed_bytes_total",
			Help: "Uncompressed row bytes the querier processed from data objects.",
		}, []string{"component"}),
	}
}

// record folds a finished query's byte counts into the counters, attributing each xcap region to the
// component of its root region.
func (m *dataObjMetrics) record(capture *xcap.Capture) {
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

	// Attribute each region's bytes to its root region, not its own name. The metastore's index
	// reader opens nested streams and pointers regions; rooting them keeps their bytes under
	// "metastore" instead of scattering into "other".
	type totals struct{ fetched, processed int64 }
	perComponent := map[string]*totals{}
	for _, r := range regions {
		comp := rootComponent(r)
		t := perComponent[comp]
		if t == nil {
			t = &totals{}
			perComponent[comp] = t
		}
		t.fetched += regionValue(r, dataobj.StatObjectBytesDownloaded)
		t.processed += regionValue(r, dataobj.StatDatasetPrimaryRowBytes) + regionValue(r, dataobj.StatDatasetSecondaryRowBytes)
	}

	for comp, t := range perComponent {
		if t.fetched > 0 {
			m.fetchedCompressedBytes.WithLabelValues(comp).Add(float64(t.fetched))
		}
		if t.processed > 0 {
			m.processedUncompressedBytes.WithLabelValues(comp).Add(float64(t.processed))
		}
	}
}

// componentForRootRegion maps a root region name to its component label.
func componentForRootRegion(rootName string) string {
	// streams-reader and logs-reader match their region names exactly. A prefix match on "streams"
	// would wrongly pull the metastore's nested streams.Reader.* reads into streams-reader. metastore
	// matches by prefix: its root span is "metastore.Sections".
	switch {
	case rootName == dataObjComponentStreamsReader:
		return dataObjComponentStreamsReader
	case rootName == logs.RegionRead:
		return dataObjComponentLogsReader
	case strings.HasPrefix(rootName, "metastore"):
		return dataObjComponentMetastore
	default:
		return dataObjComponentOther
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
