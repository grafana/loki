package tsdb

import (
	"time"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/loghttp"
	tsdbindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// StructuralConfig controls one TSDB structural discovery run.
type StructuralConfig struct {
	// UserID is accepted for tsdb.Index compatibility. It may be empty for
	// single-tenant indexes.
	UserID string

	// From bounds the series/chunk scan start time. Zero means "use index
	// bounds".
	From time.Time

	// To bounds the series/chunk scan end time. Zero means "use index bounds".
	To time.Time

	// MaxStreams caps the number of canonical selectors in the output using
	// service-diversity selection. Zero means no cap (all unique streams are
	// returned). When set, selectWithDiversity is applied after enumeration.
	MaxStreams int

	// Selector is an optional LogQL-style label matcher clause (without outer
	// braces) appended to the ForSeries call to narrow the set of streams
	// enumerated from the index. Example: `namespace="loki-ops-002"`.
	// Empty means enumerate all streams.
	Selector string

	// ProgressWriter, when non-nil, receives periodic progress updates during
	// ForSeries enumeration. The function is called with (rawCount, uniqueCount)
	// approximately every ProgressInterval (or 10s by default).
	ProgressWriter func(rawCount, uniqueCount int)

	// ProgressInterval controls how often ProgressWriter is called. Zero
	// defaults to 10 seconds.
	ProgressInterval time.Duration
}

// StructuralSeriesPayload is the callback-owned input shape copied during
// TSDB traversal before readers are closed.
type StructuralSeriesPayload struct {
	Labels      loghttp.LabelSet
	Fingerprint model.Fingerprint
	ChunkMetas  []tsdbindex.ChunkMeta
}

// MergedStream is the deduplicated aggregate for one canonical selector.
// Labels and ChunkMetas are deep-copied values, never callback-owned refs.
type MergedStream struct {
	Selector    string
	Labels      loghttp.LabelSet
	ChunkMetas  []tsdbindex.ChunkMeta
	SourceCount int
}

// StructuralResult is the structural discovery output produced from local
// TSDB indexes only.
type StructuralResult struct {
	AllSelectors  []string
	LabelSets     map[string]loghttp.LabelSet
	ByServiceName map[string][]string
	ByLabelKey    map[string][]string
	TotalRaw      int
	TotalUnique   int
	TotalSelected int
	MergedStreams map[string]MergedStream
}
