package deletion

import (
	"sort"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util"
)

// TimeRangeDelete is a delete request whose selector carries no line filters,
// so its effect on a stream is a plain time-range removal that can be computed
// from index metadata alone.
type TimeRangeDelete struct {
	Matchers   []*labels.Matcher
	Start, End int64 // nanoseconds, inclusive
}

// Interval is a merged deleted time range in nanoseconds.
type Interval struct {
	Start, End int64
}

// TimeRangeDeletes parses the given deletes and returns those whose selector
// has no line filters. Deletes with line filters can only be evaluated by
// reading chunk data, so they are skipped here; index stats and volume remain
// estimates in their presence.
func TimeRangeDeletes(deletes []*logproto.Delete) ([]TimeRangeDelete, error) {
	var res []TimeRangeDelete
	for _, d := range deletes {
		expr, err := syntax.ParseLogSelector(d.Selector, true)
		if err != nil {
			return nil, err
		}

		if expr.HasFilter() {
			continue
		}

		res = append(res, TimeRangeDelete{
			Matchers: expr.Matchers(),
			Start:    d.Start,
			End:      d.End,
		})
	}

	return res, nil
}

// DeletedIntervals returns the sorted, merged time ranges deleted for a stream
// with the given labels.
func DeletedIntervals(deletes []TimeRangeDelete, lbls labels.Labels) []Interval {
	var intervals []Interval
outer:
	for _, d := range deletes {
		for _, m := range d.Matchers {
			if !m.Matches(lbls.Get(m.Name)) {
				continue outer
			}
		}
		intervals = append(intervals, Interval{Start: d.Start, End: d.End})
	}

	if len(intervals) <= 1 {
		return intervals
	}

	sort.Slice(intervals, func(i, j int) bool { return intervals[i].Start < intervals[j].Start })

	merged := intervals[:1]
	for _, in := range intervals[1:] {
		last := &merged[len(merged)-1]
		if in.Start <= last.End {
			last.End = max(last.End, in.End)
			continue
		}
		merged = append(merged, in)
	}

	return merged
}

// UndeletedFactor returns the fraction of the chunk spanning
// [minTime, maxTime] that overlaps [from, through] and is not covered by the
// given merged deleted intervals. All arguments are in the same time unit as
// the deleted intervals (nanoseconds).
func UndeletedFactor(from, through, minTime, maxTime int64, deleted []Interval) float64 {
	factor := util.GetFactorOfTime(from, through, minTime, maxTime)
	if factor == 0 || len(deleted) == 0 {
		return factor
	}

	if minTime == maxTime {
		// single-entry chunk: the factor is all-or-nothing
		for _, d := range deleted {
			if d.Start <= minTime && minTime <= d.End {
				return 0
			}
		}
		return factor
	}

	start, end := max(from, minTime), min(through, maxTime)
	total := float64(maxTime - minTime)
	for _, d := range deleted {
		if overlap := min(end, d.End) - max(start, d.Start); overlap > 0 {
			factor -= float64(overlap) / total
		}
	}

	return max(factor, 0)
}
