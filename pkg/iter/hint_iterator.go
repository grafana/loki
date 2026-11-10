package iter

import (
	"sort"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
)

type hintTimeRange struct {
	start int64
	end   int64
}

// HintTimeRanges distinguishes an absent hint list from a supplied list whose
// ranges do not overlap the query. An absent or empty list is passthrough; a
// supplied list with no effective ranges matches nothing.
type HintTimeRanges struct {
	enabled bool
	ranges  []hintTimeRange
}

// NewHintTimeRanges clips, sorts, and merges query hint ranges without
// modifying the input.
func NewHintTimeRanges(hints []logproto.HintTimeRange, from, through time.Time) HintTimeRanges {
	result := HintTimeRanges{enabled: len(hints) > 0}
	if !result.enabled {
		return result
	}

	result.ranges = make([]hintTimeRange, 0, len(hints))
	for _, hint := range hints {
		clipped, ok := clipHintRange(hint.Start, hint.End, from, through)
		if !ok {
			continue
		}
		result.ranges = append(result.ranges, clipped)
	}

	sort.Slice(result.ranges, func(i, j int) bool {
		if result.ranges[i].start == result.ranges[j].start {
			return result.ranges[i].end < result.ranges[j].end
		}
		return result.ranges[i].start < result.ranges[j].start
	})

	merged := result.ranges[:0]
	for _, current := range result.ranges {
		if len(merged) == 0 || current.start > merged[len(merged)-1].end {
			merged = append(merged, current)
			continue
		}
		merged[len(merged)-1].end = max(merged[len(merged)-1].end, current.end)
	}
	result.ranges = merged
	return result
}

// clipHintRange clips the half-open interval [start, end) to [from, through)
// before converting to unix nanoseconds. ok is false when the clipped interval
// is empty. Clipping happens first so extreme wire timestamps are not passed to
// UnixNano.
func clipHintRange(start, end, from, through time.Time) (hintTimeRange, bool) {
	if start.Before(from) {
		start = from
	}
	if end.After(through) {
		end = through
	}
	if !start.Before(end) {
		return hintTimeRange{}, false
	}
	return hintTimeRange{
		start: start.UnixNano(),
		end:   end.UnixNano(),
	}, true
}

// Enabled reports whether hints were supplied.
func (r HintTimeRanges) Enabled() bool {
	return r.enabled
}

// Bounds returns the envelope around the normalized ranges.
func (r HintTimeRanges) Bounds() (time.Time, time.Time, bool) {
	if len(r.ranges) == 0 {
		return time.Time{}, time.Time{}, false
	}
	return time.Unix(0, r.ranges[0].start), time.Unix(0, r.ranges[len(r.ranges)-1].end), true
}

// Overlaps reports whether the half-open bounds [from, through) intersect any
// hint range.
func (r HintTimeRanges) Overlaps(from, through time.Time) bool {
	if !r.enabled {
		return true
	}
	return r.overlaps(from.UnixNano(), through.UnixNano(), false)
}

// OverlapsClosed reports whether the closed bounds [from, through] intersect
// any half-open hint range.
func (r HintTimeRanges) OverlapsClosed(from, through time.Time) bool {
	if !r.enabled {
		return true
	}
	return r.overlaps(from.UnixNano(), through.UnixNano(), true)
}

func (r HintTimeRanges) overlaps(from, through int64, throughInclusive bool) bool {
	i := sort.Search(len(r.ranges), func(i int) bool {
		return r.ranges[i].end > from
	})
	if i == len(r.ranges) {
		return false
	}
	if throughInclusive {
		return r.ranges[i].start <= through
	}
	return r.ranges[i].start < through
}

func (r HintTimeRanges) contains(ts int64) bool {
	if !r.enabled {
		return true
	}
	i := sort.Search(len(r.ranges), func(i int) bool {
		return r.ranges[i].end > ts
	})
	return i < len(r.ranges) && r.ranges[i].start <= ts
}

type hintIterator[T logprotoType] struct {
	StreamIterator[T]
	ranges    HintTimeRanges
	timestamp func(T) int64

	current    T
	labels     string
	streamHash uint64
}

func newHintIterator[T logprotoType](it StreamIterator[T], ranges HintTimeRanges, timestamp func(T) int64) StreamIterator[T] {
	if !ranges.enabled {
		return it
	}
	return &hintIterator[T]{
		StreamIterator: it,
		ranges:         ranges,
		timestamp:      timestamp,
	}
}

func (i *hintIterator[T]) Next() bool {
	for i.StreamIterator.Next() {
		current := i.StreamIterator.At()
		if !i.ranges.contains(i.timestamp(current)) {
			continue
		}
		i.current = current
		i.labels = i.StreamIterator.Labels()
		i.streamHash = i.StreamIterator.StreamHash()
		return true
	}
	return false
}

func (i *hintIterator[T]) At() T {
	return i.current
}

func (i *hintIterator[T]) Labels() string {
	return i.labels
}

func (i *hintIterator[T]) StreamHash() uint64 {
	return i.streamHash
}

// NewHintEntryIterator filters entries to the supplied hint ranges.
func NewHintEntryIterator(it EntryIterator, ranges HintTimeRanges) EntryIterator {
	return newHintIterator(it, ranges, func(entry logproto.Entry) int64 {
		return entry.Timestamp.UnixNano()
	})
}

// NewHintSampleIterator filters samples to the supplied hint ranges.
func NewHintSampleIterator(it SampleIterator, ranges HintTimeRanges) SampleIterator {
	return newHintIterator(it, ranges, func(sample logproto.Sample) int64 {
		return sample.Timestamp
	})
}
