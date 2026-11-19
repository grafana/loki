package tsdb

import (
	"time"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// hintChunkFilter includes the chunks that overlap a hint range of their series.
// It uses the same overlap rule as the querier, which only fetches those chunks.
type hintChunkFilter struct {
	ranges iter.HintTimeRanges
}

// newHintChunkFilter returns a filter that includes the chunks overlapping
// hints within [from, through), or nil when hints is nil. If hints is empty,
// the filter includes no chunk.
func newHintChunkFilter(hints *logproto.HintTimeRanges, from, through model.Time) index.ChunkFilter {
	if hints == nil {
		return nil
	}
	return hintChunkFilter{ranges: iter.NewHintTimeRangeSet(hints, from.Time(), through.Time())}
}

func (f hintChunkFilter) Overlaps(fp uint64, minT, maxT int64) bool {
	return f.ranges.ForFingerprint(fp).OverlapsClosed(time.UnixMilli(minT), time.UnixMilli(maxT))
}

func (f hintChunkFilter) Covers(fp uint64, minT, maxT int64) bool {
	return f.ranges.ForFingerprint(fp).CoversClosed(time.UnixMilli(minT), time.UnixMilli(maxT))
}
