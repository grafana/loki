package queryfrontend

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestCoarsenHintRanges(t *testing.T) {
	at := func(minute int) time.Time { return time.Date(2025, 1, 1, 12, minute, 0, 0, time.UTC) }
	hint := func(start, end int) hintprovider.HintTimeRange {
		return hintprovider.HintTimeRange{Start: at(start), End: at(end)}
	}
	inShard := func(shard uint32, start, end int) hintprovider.HintTimeRange {
		return hintprovider.HintTimeRange{Start: at(start), End: at(end), DocumentShard: shard, DocumentShardBits: 5}
	}
	want := func(start, end int) logproto.HintTimeRange {
		return logproto.HintTimeRange{Start: at(start), End: at(end)}
	}
	wantInShard := func(shard uint32, start, end int) logproto.HintTimeRange {
		return logproto.HintTimeRange{Start: at(start), End: at(end), DocumentShard: shard, DocumentShardBits: 5}
	}
	shards := []hintprovider.HintTimeRange{
		hint(0, 1), hint(3, 4),
		inShard(3, 0, 1), inShard(3, 10, 11), inShard(3, 12, 13),
		inShard(4, 5, 6), inShard(4, 30, 31),
	}

	for _, tc := range []struct {
		name          string
		ranges        []hintprovider.HintTimeRange
		start, end    time.Time
		maxRanges     int
		want          []logproto.HintTimeRange
		wantCoarsened bool
	}{
		{
			name:      "merges the smallest gaps",
			ranges:    []hintprovider.HintTimeRange{hint(0, 1), hint(6, 7), hint(8, 9), hint(12, 13), hint(15, 16)},
			start:     at(0),
			end:       at(59),
			maxRanges: 3,
			// Gaps of 5, 1, 3 and 2 minutes: the 1 and 2 minute gaps merge.
			want:          []logproto.HintTimeRange{want(0, 1), want(6, 9), want(12, 16)},
			wantCoarsened: true,
		},
		{
			name: "spreads equal gaps evenly",
			ranges: []hintprovider.HintTimeRange{
				hint(0, 1), hint(2, 3), hint(4, 5), hint(6, 7), hint(8, 9),
				hint(10, 11), hint(12, 13), hint(14, 15), hint(16, 17),
			},
			start:         at(0),
			end:           at(59),
			maxRanges:     5,
			want:          []logproto.HintTimeRange{want(0, 3), want(4, 7), want(8, 11), want(12, 15), want(16, 17)},
			wantCoarsened: true,
		},
		{
			name:      "never merges document shards",
			ranges:    shards,
			start:     at(0),
			end:       at(59),
			maxRanges: 5,
			// Gaps of 2 (no shard), 9 and 1 (shard 3), and 24 (shard 4) minutes.
			want: []logproto.HintTimeRange{
				want(0, 4),
				wantInShard(3, 0, 1), wantInShard(3, 10, 13),
				wantInShard(4, 5, 6), wantInShard(4, 30, 31),
			},
			wantCoarsened: true,
		},
		{
			name:          "keeps one range per document shard under the shard count",
			ranges:        shards,
			start:         at(0),
			end:           at(59),
			maxRanges:     2,
			want:          []logproto.HintTimeRange{want(0, 4), wantInShard(3, 0, 13), wantInShard(4, 5, 31)},
			wantCoarsened: true,
		},
		{
			name: "clips to the window and converts the pre-min-date sentinel",
			ranges: []hintprovider.HintTimeRange{
				{End: at(10), Source: hintprovider.HintSourcePreMinDate},
				hint(12, 13), hint(20, 21), hint(22, 23), hint(40, 41),
			},
			start:         at(5),
			end:           at(30),
			maxRanges:     2,
			want:          []logproto.HintTimeRange{want(5, 13), want(20, 23)},
			wantCoarsened: true,
		},
		{
			name:      "ignores ranges that only touch the window",
			ranges:    []hintprovider.HintTimeRange{hint(0, 5), hint(6, 7), hint(10, 11)},
			start:     at(5),
			end:       at(10),
			maxRanges: 1,
			want:      []logproto.HintTimeRange{want(6, 7)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ranges := hintprovider.NormalizeRanges(tc.ranges)
			spans := overlappingSpans(ranges, nil, tc.start, tc.end)
			got, coarsened := coarsenHintRanges(ranges, spans, tc.start, tc.end, tc.maxRanges)
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.wantCoarsened, coarsened)
		})
	}
}

func TestHintRangesForSpans(t *testing.T) {
	at := func(minute int) time.Time { return time.Date(2025, 1, 1, 12, minute, 0, 0, time.UTC) }
	ranges := []hintprovider.HintTimeRange{
		{Start: at(0), End: at(1)},
		{Start: at(5), End: at(6)},
		{Start: at(7), End: at(8)},
	}
	spans := overlappingSpans(ranges, nil, at(0), at(59))
	clipped := clipHintRangesForInterval(ranges, at(0), at(59))

	for _, tc := range []struct {
		name          string
		maxRanges     int
		want          []logproto.HintTimeRange
		wantCoarsened bool
	}{
		{"no cap", 0, clipped, false},
		{"at the cap", 3, clipped, false},
		{"over the cap", 2, []logproto.HintTimeRange{{Start: at(0), End: at(1)}, {Start: at(5), End: at(8)}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, coarsened := hintRangesForSpans(ranges, spans, at(0), at(59), tc.maxRanges)
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.wantCoarsened, coarsened)
		})
	}
}

// A document shard whose ranges only touch the window has nothing in it, so
// it doesn't take a range of the cap from the shards that do.
func TestHintRangesForSpans_TouchingWindow(t *testing.T) {
	at := func(minute int) time.Time { return time.Date(2025, 1, 1, 12, minute, 0, 0, time.UTC) }
	inShard := func(shard uint32, start, end int) hintprovider.HintTimeRange {
		return hintprovider.HintTimeRange{Start: at(start), End: at(end), DocumentShard: shard, DocumentShardBits: 5}
	}
	want := []logproto.HintTimeRange{
		{Start: at(0), End: at(5), DocumentShard: 3, DocumentShardBits: 5},
		{Start: at(20), End: at(25), DocumentShard: 3, DocumentShardBits: 5},
	}

	for _, tc := range []struct {
		name     string
		touching hintprovider.HintTimeRange
	}{
		{"starts at the window end", inShard(4, 30, 31)},
		{"ends at the window start", inShard(4, -1, 0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ranges := hintprovider.NormalizeRanges([]hintprovider.HintTimeRange{inShard(3, 0, 5), inShard(3, 20, 25), tc.touching})
			spans := overlappingSpans(ranges, nil, at(0), at(30))
			got, coarsened := hintRangesForSpans(ranges, spans, at(0), at(30), 2)
			require.Equal(t, want, got)
			require.False(t, coarsened)
		})
	}
}

// Coarsening only fills gaps between a document shard's clipped ranges, keeps
// the largest ones, and stays within the cap.
func TestCoarsenHintRanges_Random(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	var coarsenedCases int
	for i := range 500 {
		// Half the cases put every time on a one-minute grid, as v5 documents
		// and split boundaries are, so ranges often only touch the window.
		grid := time.Second
		if rng.IntN(2) == 0 {
			grid = time.Minute
		}
		ranges := randomNormalizedRanges(rng, base, grid)
		start := base.Add(randomDuration(rng, 600, grid))
		end := start.Add(grid + randomDuration(rng, 3600, grid))
		spans := overlappingSpans(ranges, nil, start, end)
		clipped := clipHintRangesForInterval(spanRanges(ranges, spans), start, end)
		maxRanges := 1 + rng.IntN(len(clipped)+2)

		got, coarsened := coarsenHintRanges(ranges, spans, start, end, maxRanges)

		// Each document shard in the clip keeps at least one range, so the
		// result fits when the clip has no more ranges than this.
		shards := documentShardCount(clipped)
		fits := max(maxRanges, shards)
		require.Len(t, spans, shards, "case %d: a span has no range in the window", i)
		require.Equal(t, len(clipped) > fits, coarsened, "case %d", i)
		if !coarsened {
			require.Equal(t, clipped, got, "case %d: nothing merged, so the result is the clip", i)
			continue
		}
		require.Len(t, got, fits, "case %d", i)
		coarsenedCases++

		owner := coveringRanges(t, clipped, got)
		var keptGaps int
		minKept, maxMerged := time.Duration(-1), time.Duration(0)
		for j := 1; j < len(clipped); j++ {
			if documentShardKey(clipped[j]) != documentShardKey(clipped[j-1]) {
				continue
			}
			gap := clipped[j].Start.Sub(clipped[j-1].End)
			if owner[j] == owner[j-1] {
				maxMerged = max(maxMerged, gap)
				continue
			}
			keptGaps++
			if minKept < 0 || gap < minKept {
				minKept = gap
			}
		}
		if minKept >= 0 {
			require.LessOrEqual(t, maxMerged, minKept, "case %d: a merged gap is wider than a kept one", i)
		}
		require.Equal(t, max(maxRanges-shards, 0), keptGaps, "case %d", i)
	}
	require.Greater(t, coarsenedCases, 100, "too few cases coarsen to test it")
}

// documentShardCount returns the number of document shards in ranges, which
// are grouped by document shard.
func documentShardCount(ranges []logproto.HintTimeRange) int {
	n := 0
	for j := range ranges {
		if j == 0 || documentShardKey(ranges[j]) != documentShardKey(ranges[j-1]) {
			n++
		}
	}
	return n
}

// coveringRanges checks that every clipped range lies in an output range of
// the same document shard, and that each output range starts and ends where
// clipped ranges do. It returns the index of the output range covering each
// clipped range.
func coveringRanges(t *testing.T, clipped, got []logproto.HintTimeRange) []int {
	t.Helper()
	owner := make([]int, len(clipped))
	o := 0
	for j, c := range clipped {
		for o < len(got) && (documentShardKey(got[o]) != documentShardKey(c) || !got[o].End.After(c.Start)) {
			o++
		}
		require.Less(t, o, len(got), "clipped range %d is not covered", j)
		require.False(t, got[o].Start.After(c.Start), "clipped range %d starts before its output range", j)
		require.False(t, got[o].End.Before(c.End), "clipped range %d ends after its output range", j)
		owner[j] = o
	}
	for o, g := range got {
		require.Contains(t, owner, o, "output range %d covers no clipped range", o)
		first, last := -1, -1
		for j := range clipped {
			if owner[j] == o {
				if first < 0 {
					first = j
				}
				last = j
			}
		}
		require.Equal(t, clipped[first].Start, g.Start, "output range %d starts between clipped ranges", o)
		require.Equal(t, clipped[last].End, g.End, "output range %d ends between clipped ranges", o)
	}
	return owner
}

// randomNormalizedRanges returns ranges without a document shard, sometimes
// with a pre-min-date sentinel, and sometimes v5 ranges in up to 32 document
// shards, normalized like a lookup result. Every time is on grid.
func randomNormalizedRanges(rng *rand.Rand, base time.Time, grid time.Duration) []hintprovider.HintTimeRange {
	var ranges []hintprovider.HintTimeRange
	if rng.IntN(4) == 0 {
		ranges = append(ranges, hintprovider.HintTimeRange{End: base.Add(randomDuration(rng, 900, grid))})
	}
	group := func(shard uint32, bits uint8) {
		for range rng.IntN(40) {
			start := base.Add(randomDuration(rng, 4800, grid))
			ranges = append(ranges, hintprovider.HintTimeRange{
				Start:             start,
				End:               start.Add(grid + randomDuration(rng, 120, grid)),
				DocumentShard:     shard,
				DocumentShardBits: bits,
			})
		}
	}
	group(0, 0)
	if rng.IntN(2) == 0 {
		for range rng.IntN(6) {
			group(uint32(rng.IntN(32)), 5)
		}
	}
	return hintprovider.NormalizeRanges(ranges)
}

// randomDuration returns a random duration under n seconds, rounded down to
// grid.
func randomDuration(rng *rand.Rand, n int, grid time.Duration) time.Duration {
	return (time.Duration(rng.IntN(n)) * time.Second).Truncate(grid)
}
