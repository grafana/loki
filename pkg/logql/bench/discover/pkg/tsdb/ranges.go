package tsdb

import (
	"time"

	bench "github.com/grafana/loki/v3/pkg/logql/bench"
	tsdbindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// SelectMinRangeFromChunks computes the minimum range and instant-range
// durations for a single stream using its merged TSDB chunk evidence.
//
// It evaluates rangeBuckets in ascending order (5m, 10m, 15m, 30m, 1h),
// counting entries from chunks that overlap each bucket window anchored at
// the latest chunk end time. The first bucket meeting >=rangeEntryThreshold
// entries becomes MinRange; the first meeting >=instantEntryThreshold becomes
// MinInstantRange. Returns zero durations when no bucket meets a threshold.
func SelectMinRangeFromChunks(chunks []tsdbindex.ChunkMeta) (minRange, minInstantRange time.Duration) {
	if len(chunks) == 0 {
		return 0, 0
	}

	// Find the latest MaxTime across all chunks to anchor the bucket window.
	var maxTime int64
	for _, c := range chunks {
		if c.MaxTime > maxTime {
			maxTime = c.MaxTime
		}
	}

	rangeMet, instantMet := false, false

	for _, d := range rangeBuckets {
		if rangeMet && instantMet {
			break
		}

		// Bucket window: [maxTime - d, maxTime]
		windowStart := maxTime - d.Milliseconds()

		var totalEntries uint64
		for _, c := range chunks {
			if chunksOverlap(c.MinTime, c.MaxTime, windowStart, maxTime) {
				//TODO: this isn't entirely correct as the entries are for the whole chunk, not just the overlapping portion.
				// However, this doesn't have noticeable impact on results so keeping it for now.
				totalEntries += uint64(c.Entries)
			}
		}

		if !rangeMet && totalEntries >= rangeEntryThreshold {
			minRange = d
			rangeMet = true
		}
		if !instantMet && totalEntries >= instantEntryThreshold {
			minInstantRange = d
			instantMet = true
		}
	}

	return minRange, minInstantRange
}

// chunksOverlap returns true if the chunk time range [chunkMin, chunkMax]
// overlaps with the window [windowStart, windowEnd]. Both ranges are
// inclusive on both ends (matching TSDB chunk semantics).
func chunksOverlap(chunkMin, chunkMax, windowStart, windowEnd int64) bool {
	return chunkMin <= windowEnd && chunkMax >= windowStart
}

// RunRangeDerivation computes a RangeResult from merged chunk
// evidence, producing the same output contract as RunRangeProbing but
// without any API calls.
//
// For each selector in mergedStreams, it uses SelectMinRangeFromChunks to
// derive min-range/min-instant-range from chunk metadata. Selectors that
// meet neither threshold are counted as TotalSkipped.
func RunRangeDerivation(mergedStreams map[string]MergedStream) *RangeResult {
	out := &RangeResult{
		MetadataBySelector: make(map[string]*bench.SerializableStreamMetadata),
		TotalProbed:        len(mergedStreams),
	}

	for selector, stream := range mergedStreams {
		minR, minI := SelectMinRangeFromChunks(stream.ChunkMetas)

		if minR > 0 || minI > 0 {
			out.MetadataBySelector[selector] = &bench.SerializableStreamMetadata{
				MinRange:        minR,
				MinInstantRange: minI,
			}
		} else {
			out.TotalSkipped++
		}
	}

	return out
}
