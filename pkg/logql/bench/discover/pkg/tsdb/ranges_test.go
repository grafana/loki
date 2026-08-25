package tsdb

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	tsdbindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func TestTSDBRangesBucketsBothThresholdsAt5m(t *testing.T) {
	// 10 entries in a chunk within 5m window → both thresholds met at 5m.
	base := model.TimeFromUnixNano(time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC).UnixNano())
	chunks := []tsdbindex.ChunkMeta{
		{MinTime: int64(base), MaxTime: int64(base + model.Time(5*time.Minute/time.Millisecond)), Entries: 10, KB: 1, Checksum: 1},
	}

	minR, minI := SelectMinRangeFromChunks(chunks)
	require.Equal(t, 5*time.Minute, minR, "range threshold met at 5m")
	require.Equal(t, 5*time.Minute, minI, "instant threshold met at 5m")
}

func TestTSDBRangesBucketsRangeAt5mInstantAt10m(t *testing.T) {
	// 7 entries in 5m window → meets range (>=5) but not instant (>=10).
	// Additional chunk in 10m window brings total to 15 → instant met at 10m.
	base := model.TimeFromUnixNano(time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC).UnixNano())
	chunks := []tsdbindex.ChunkMeta{
		{MinTime: int64(base + model.Time(5*time.Minute/time.Millisecond)), MaxTime: int64(base + model.Time(10*time.Minute/time.Millisecond)), Entries: 7, KB: 1, Checksum: 1},
		{MinTime: int64(base), MaxTime: int64(base + model.Time(3*time.Minute/time.Millisecond)), Entries: 8, KB: 1, Checksum: 2},
	}

	minR, minI := SelectMinRangeFromChunks(chunks)
	require.Equal(t, 5*time.Minute, minR, "range met at 5m (7 entries in window)")
	require.Equal(t, 10*time.Minute, minI, "instant met at 10m (15 entries total)")
}

func TestTSDBRangesThresholdsNeitherMet(t *testing.T) {
	// Very few entries — neither threshold met across any bucket.
	base := model.TimeFromUnixNano(time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC).UnixNano())
	chunks := []tsdbindex.ChunkMeta{
		{MinTime: int64(base), MaxTime: int64(base + model.Time(2*time.Minute/time.Millisecond)), Entries: 2, KB: 1, Checksum: 1},
	}

	minR, minI := SelectMinRangeFromChunks(chunks)
	require.Equal(t, time.Duration(0), minR, "no range threshold met")
	require.Equal(t, time.Duration(0), minI, "no instant threshold met")
}

func TestTSDBRangesThresholdsExactBoundaries(t *testing.T) {
	// Exactly rangeEntryThreshold=5 and instantEntryThreshold=10.
	base := model.TimeFromUnixNano(time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC).UnixNano())

	tests := []struct {
		name      string
		entries   uint32
		wantRange time.Duration
		wantInstR time.Duration
	}{
		{"4 entries: below range threshold", 4, 0, 0},
		{"5 entries: exactly range threshold", 5, 5 * time.Minute, 0},
		{"9 entries: range met, instant below", 9, 5 * time.Minute, 0},
		{"10 entries: both thresholds met", 10, 5 * time.Minute, 5 * time.Minute},
		{"11 entries: both thresholds met", 11, 5 * time.Minute, 5 * time.Minute},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			chunks := []tsdbindex.ChunkMeta{
				{MinTime: int64(base), MaxTime: int64(base + model.Time(5*time.Minute/time.Millisecond)), Entries: tc.entries, KB: 1, Checksum: 1},
			}
			minR, minI := SelectMinRangeFromChunks(chunks)
			require.Equal(t, tc.wantRange, minR)
			require.Equal(t, tc.wantInstR, minI)
		})
	}
}

func TestTSDBRangesThresholdsEmptyChunks(t *testing.T) {
	minR, minI := SelectMinRangeFromChunks(nil)
	require.Equal(t, time.Duration(0), minR)
	require.Equal(t, time.Duration(0), minI)
}

func TestTSDBRangesMergeOverlappingChangesOutcome(t *testing.T) {
	// With a single index file, 4 entries are below range threshold.
	// With two overlapping index files merged, the combined entries
	// push the total above the threshold.
	base := model.TimeFromUnixNano(time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC).UnixNano())
	selector := `{service_name="querier"}`

	// Single-source: 4 entries → below range threshold.
	singleChunks := []tsdbindex.ChunkMeta{
		{MinTime: int64(base), MaxTime: int64(base + model.Time(5*time.Minute/time.Millisecond)), Entries: 4, KB: 1, Checksum: 1},
	}

	singleStreams := map[string]MergedStream{
		selector: {Selector: selector, ChunkMetas: singleChunks, SourceCount: 1},
	}
	singleResult := RunRangeDerivation(singleStreams)
	require.Empty(t, singleResult.MetadataBySelector, "single source should not meet threshold")
	require.Equal(t, 1, singleResult.TotalSkipped)

	// Merged-source: 4 + 3 = 7 entries → meets range threshold.
	mergedChunks := []tsdbindex.ChunkMeta{
		{MinTime: int64(base), MaxTime: int64(base + model.Time(5*time.Minute/time.Millisecond)), Entries: 4, KB: 1, Checksum: 1},
		{MinTime: int64(base + model.Time(2*time.Minute/time.Millisecond)), MaxTime: int64(base + model.Time(4*time.Minute/time.Millisecond)), Entries: 3, KB: 1, Checksum: 2},
	}
	mergedStreams := map[string]MergedStream{
		selector: {Selector: selector, ChunkMetas: mergedChunks, SourceCount: 2},
	}
	mergedResult := RunRangeDerivation(mergedStreams)
	require.Contains(t, mergedResult.MetadataBySelector, selector, "merged source should meet threshold")
	require.Equal(t, 5*time.Minute, mergedResult.MetadataBySelector[selector].MinRange)
}

func TestTSDBRangesDeterministic(t *testing.T) {
	base := model.TimeFromUnixNano(time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC).UnixNano())

	streams := map[string]MergedStream{
		`{svc="a"}`: {
			Selector: `{svc="a"}`,
			ChunkMetas: []tsdbindex.ChunkMeta{
				{MinTime: int64(base), MaxTime: int64(base + model.Time(5*time.Minute/time.Millisecond)), Entries: 10, KB: 1, Checksum: 1},
			},
			SourceCount: 1,
		},
		`{svc="b"}`: {
			Selector: `{svc="b"}`,
			ChunkMetas: []tsdbindex.ChunkMeta{
				{MinTime: int64(base), MaxTime: int64(base + model.Time(5*time.Minute/time.Millisecond)), Entries: 3, KB: 1, Checksum: 2},
			},
			SourceCount: 1,
		},
	}

	first := RunRangeDerivation(streams)
	second := RunRangeDerivation(streams)

	require.Equal(t, first.MetadataBySelector, second.MetadataBySelector)
	require.Equal(t, first.TotalProbed, second.TotalProbed)
	require.Equal(t, first.TotalSkipped, second.TotalSkipped)
}

func TestTSDBRangesMixedSelectors(t *testing.T) {
	base := model.TimeFromUnixNano(time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC).UnixNano())

	streams := map[string]MergedStream{
		`{svc="meets-both"}`: {
			Selector: `{svc="meets-both"}`,
			ChunkMetas: []tsdbindex.ChunkMeta{
				{MinTime: int64(base), MaxTime: int64(base + model.Time(5*time.Minute/time.Millisecond)), Entries: 15, KB: 1, Checksum: 1},
			},
			SourceCount: 1,
		},
		`{svc="meets-range-only"}`: {
			Selector: `{svc="meets-range-only"}`,
			ChunkMetas: []tsdbindex.ChunkMeta{
				{MinTime: int64(base), MaxTime: int64(base + model.Time(5*time.Minute/time.Millisecond)), Entries: 7, KB: 1, Checksum: 2},
			},
			SourceCount: 1,
		},
		`{svc="meets-neither"}`: {
			Selector: `{svc="meets-neither"}`,
			ChunkMetas: []tsdbindex.ChunkMeta{
				{MinTime: int64(base), MaxTime: int64(base + model.Time(5*time.Minute/time.Millisecond)), Entries: 2, KB: 1, Checksum: 3},
			},
			SourceCount: 1,
		},
	}

	result := RunRangeDerivation(streams)

	require.Equal(t, 3, result.TotalProbed)
	require.Equal(t, 1, result.TotalSkipped, "only meets-neither should be skipped")

	require.Contains(t, result.MetadataBySelector, `{svc="meets-both"}`)
	require.Equal(t, 5*time.Minute, result.MetadataBySelector[`{svc="meets-both"}`].MinRange)
	require.Equal(t, 5*time.Minute, result.MetadataBySelector[`{svc="meets-both"}`].MinInstantRange)

	require.Contains(t, result.MetadataBySelector, `{svc="meets-range-only"}`)
	require.Equal(t, 5*time.Minute, result.MetadataBySelector[`{svc="meets-range-only"}`].MinRange)
	require.Equal(t, time.Duration(0), result.MetadataBySelector[`{svc="meets-range-only"}`].MinInstantRange)

	require.NotContains(t, result.MetadataBySelector, `{svc="meets-neither"}`)
}

func TestTSDBRangesGraduatedBuckets(t *testing.T) {
	// Chunks are spread across time so only wider buckets accumulate enough entries.
	base := model.TimeFromUnixNano(time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC).UnixNano())

	// Chunk at base+55m..base+60m with 3 entries (in 5m window)
	// Chunk at base+45m..base+50m with 4 entries (in 15m window with above = 7)
	// Chunk at base+30m..base+35m with 6 entries (in 30m window with above = 13)
	endMs := model.Time(60 * time.Minute / time.Millisecond)
	chunks := []tsdbindex.ChunkMeta{
		{MinTime: int64(base + model.Time(55*time.Minute/time.Millisecond)), MaxTime: int64(base + endMs), Entries: 3, KB: 1, Checksum: 1},
		{MinTime: int64(base + model.Time(45*time.Minute/time.Millisecond)), MaxTime: int64(base + model.Time(50*time.Minute/time.Millisecond)), Entries: 4, KB: 1, Checksum: 2},
		{MinTime: int64(base + model.Time(30*time.Minute/time.Millisecond)), MaxTime: int64(base + model.Time(35*time.Minute/time.Millisecond)), Entries: 6, KB: 1, Checksum: 3},
	}

	minR, minI := SelectMinRangeFromChunks(chunks)

	// 5m window [55m, 60m]: 3 entries → neither threshold
	// 10m window [50m, 60m]: 3 entries → neither (45-50 chunk doesn't overlap; MaxTime=50m < windowStart=50m... let's check)
	// Actually: window anchored at max chunk MaxTime (60m).
	// 10m: [50m, 60m]: chunk[1] has MaxTime=50m, MinTime=45m → 45m <= 60m && 50m >= 50m → overlaps. So 3+4=7.
	// 15m: [45m, 60m]: all first two chunks overlap. 3+4=7.
	// But 7>=5 for range at 10m. 7<10 for instant. So range=10m.
	// 30m: [30m, 60m]: all 3 chunks overlap. 3+4+6=13. 13>=10. instant=30m.
	require.Equal(t, 10*time.Minute, minR, "range threshold met at 10m (7 entries)")
	require.Equal(t, 30*time.Minute, minI, "instant threshold met at 30m (13 entries)")
}
