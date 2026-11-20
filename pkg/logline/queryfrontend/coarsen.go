package queryfrontend

import (
	"math"
	"sort"
	"time"

	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
)

// hintSpan is the [lo, hi) index range of one document shard's normalized hint
// ranges that overlap a sub-request's read window.
type hintSpan struct {
	lo, hi int
}

// overlappingSpans returns, for each document shard whose ranges
//   - can hold a stream in one of queryShards, or any stream when the request
//     is not sharded, and
//   - overlap the queried [start, end),
//
// the span of those ranges. It does not copy them. A range that only touches
// the window clips to nothing and is left out, because every span takes a
// range of the cap.
//
// ranges must be normalized: grouped by document shard, each group sorted by
// Start.
func overlappingSpans(ranges []hintprovider.HintTimeRange, queryShards logql.Shards, start, end time.Time) []hintSpan {
	var spans []hintSpan
	for rest := ranges; len(rest) > 0; {
		group, next := nextDocumentShardGroup(rest)
		offset := len(ranges) - len(rest)
		rest = next
		if !overlapsQueryShards(group[0], queryShards) {
			continue
		}
		// A group has no overlaps, so End is sorted like Start.
		first := sort.Search(len(group), func(i int) bool { return group[i].End.After(start) })
		last := sort.Search(len(group), func(i int) bool { return !group[i].Start.Before(end) })
		if first < last {
			spans = append(spans, hintSpan{lo: offset + first, hi: offset + last})
		}
	}
	return spans
}

// spanLen returns the number of ranges in spans.
func spanLen(spans []hintSpan) int {
	n := 0
	for _, s := range spans {
		n += s.hi - s.lo
	}
	return n
}

// spanRanges returns the ranges in spans. One span is returned without a copy.
func spanRanges(ranges []hintprovider.HintTimeRange, spans []hintSpan) []hintprovider.HintTimeRange {
	if len(spans) == 1 {
		return ranges[spans[0].lo:spans[0].hi]
	}
	out := make([]hintprovider.HintTimeRange, 0, spanLen(spans))
	for _, s := range spans {
		out = append(out, ranges[s.lo:s.hi]...)
	}
	return out
}

// hintRangesForSpans clips the ranges in spans to [start, end). When there are
// more than maxRanges, it coarsens them instead and reports whether any gap
// was merged. A maxRanges of 0 is no cap.
func hintRangesForSpans(ranges []hintprovider.HintTimeRange, spans []hintSpan, start, end time.Time, maxRanges int) ([]logproto.HintTimeRange, bool) {
	if maxRanges <= 0 || spanLen(spans) <= maxRanges {
		return clipHintRangesForInterval(spanRanges(ranges, spans), start, end), false
	}
	return coarsenHintRanges(ranges, spans, start, end, maxRanges)
}

// coarsenHintRanges clips the ranges in spans to [start, end) like
// clipHintRangesForInterval, then merges the smallest gaps between the ranges
// of one document shard until at most maxRanges are left. The wider ranges
// still cover every match. Each span keeps at least one range, so the result
// has more than maxRanges when there are more spans.
//
// Ranges of different document shards never merge, and every range keeps its
// shard. A querier copies a range without a shard into every shard's list.
//
// It reports whether any gap was merged.
func coarsenHintRanges(ranges []hintprovider.HintTimeRange, spans []hintSpan, start, end time.Time, maxRanges int) ([]logproto.HintTimeRange, bool) {
	cut, coarsened := selectGaps(ranges, spans, start, end, maxRanges-len(spans))
	out := make([]logproto.HintTimeRange, 0, max(maxRanges, len(spans)))
	forEachRun(ranges, spans, start, end, func(run logproto.HintTimeRange, gap time.Duration) {
		if gap == 0 || cut.keep(gap) {
			out = append(out, run)
			return
		}
		out[len(out)-1].End = run.End
	})
	return out, coarsened
}

// selectGaps picks the keep largest gaps between runs, and reports whether
// there are more gaps than that. Memory is bounded by keep, not by the number
// of ranges.
func selectGaps(ranges []hintprovider.HintTimeRange, spans []hintSpan, start, end time.Time, keep int) (gapCut, bool) {
	var (
		gaps    int
		largest gapHeap
	)
	if keep > 0 {
		largest = make(gapHeap, 0, min(keep, spanLen(spans)))
	}
	forEachRun(ranges, spans, start, end, func(_ logproto.HintTimeRange, gap time.Duration) {
		if gap == 0 {
			return
		}
		gaps++
		switch {
		case keep <= 0:
		case len(largest) < keep:
			largest = append(largest, gap)
			if len(largest) == keep {
				largest.init()
			}
		case gap > largest[0]:
			largest[0] = gap
			largest.down(0)
		}
	})
	if gaps <= max(keep, 0) {
		return gapCut{}, false
	}
	if keep <= 0 {
		return gapCut{threshold: math.MaxInt64}, true
	}

	// largest holds every gap above its minimum and some of the gaps equal to
	// it. Keep that many of the equal gaps.
	cut := gapCut{threshold: largest[0]}
	for _, gap := range largest {
		if gap == cut.threshold {
			cut.keepTies++
		}
	}
	forEachRun(ranges, spans, start, end, func(_ logproto.HintTimeRange, gap time.Duration) {
		if gap == cut.threshold {
			cut.ties++
		}
	})
	return cut, true
}

// gapCut keeps the gaps above threshold, merges the ones below it, and keeps
// keepTies of the ties gaps equal to it. The kept ties are spread evenly in
// scan order. v5 documents have a fixed width, so many gaps can be equal, and
// keeping the first ones would merge the rest of the window into one range.
type gapCut struct {
	threshold      time.Duration
	ties, keepTies int64
	seen           int64
}

// keep reports whether the gap stays. Call it once per gap, in scan order.
func (c *gapCut) keep(gap time.Duration) bool {
	switch {
	case gap > c.threshold:
		return true
	case gap < c.threshold || c.ties == 0:
		return false
	}
	// Keep tie i when x*keepTies/ties, rounded down, goes up from x = i to
	// x = i+1. That happens keepTies times across the ties, about every
	// ties/keepTies. For example, keeping 3 of 7 ties keeps the 3rd, 5th and
	// 7th.
	i := c.seen
	c.seen++
	return (i+1)*c.keepTies/c.ties > i*c.keepTies/c.ties
}

// forEachRun calls fn for each run of the ranges in spans, in order. A run is
// a range clipped to [start, end) like clipHintRangesForInterval, merged with
// the ranges after it that overlap or touch it. gap is the time since the
// previous run of the same span, or 0 for a span's first run.
func forEachRun(ranges []hintprovider.HintTimeRange, spans []hintSpan, start, end time.Time, fn func(run logproto.HintTimeRange, gap time.Duration)) {
	for _, s := range spans {
		var (
			run  logproto.HintTimeRange
			gap  time.Duration
			open bool
		)
		for _, r := range ranges[s.lo:s.hi] {
			clipped, ok := clipHintRange(r, start, end)
			switch {
			case !ok:
			case !open:
				run, open = clipped, true
			case !clipped.Start.After(run.End):
				if clipped.End.After(run.End) {
					run.End = clipped.End
				}
			default:
				fn(run, gap)
				gap = clipped.Start.Sub(run.End)
				run = clipped
			}
		}
		if open {
			fn(run, gap)
		}
	}
}

// gapHeap is a min-heap of gaps.
type gapHeap []time.Duration

func (h gapHeap) init() {
	for i := len(h)/2 - 1; i >= 0; i-- {
		h.down(i)
	}
}

func (h gapHeap) down(i int) {
	for {
		smallest := 2*i + 1
		if smallest >= len(h) {
			return
		}
		if right := smallest + 1; right < len(h) && h[right] < h[smallest] {
			smallest = right
		}
		if h[i] <= h[smallest] {
			return
		}
		h[i], h[smallest] = h[smallest], h[i]
		i = smallest
	}
}
