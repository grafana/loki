package verification

import (
	"sort"
	"time"

	"github.com/grafana/loki/v3/pkg/logql/syntax"

	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
)

// Report summarizes whether provided hint ranges cover actual query results.
type Report struct {
	HintRanges     int
	TotalEntries   int
	CoveredEntries int
	FalseNegatives int
	// FalseNegativeTimestamps records timestamps of entries not covered by any
	// hint range. Only populated when the caller opts in via VerifyEntries.
	FalseNegativeTimestamps []time.Time
	// FalseNegativeRanges are metric lookbacks with no overlapping hint.
	// Each range is that step's whole lookback. Log checks leave this empty.
	FalseNegativeRanges []hintprovider.HintTimeRange
	// FalsePositives counts hint ranges that contain no result entries.
	// Only populated when CountFalsePositives is called.
	FalsePositives int
	// Correct is true when no scored sample or log line was missed. An empty
	// report is correct.
	//
	// TODO: Confirm with the team whether metric queries should also require a
	// scored sample. The correctness log check still requires one.
	Correct bool
}

// IsAbsentOverTime reports whether expr is an absent_over_time aggregation,
// including one wrapped in vector aggregations.
func IsAbsentOverTime(expr syntax.Expr) bool {
	switch e := expr.(type) {
	case *syntax.VectorAggregationExpr:
		return IsAbsentOverTime(e.Left)
	case *syntax.RangeAggregationExpr:
		return e.Operation == syntax.OpRangeTypeAbsent
	default:
		return false
	}
}

// RangesOverlap reports whether any half-open hint [Start, End) overlaps the
// lookback (start, end]. A hint that only ends at start shares no time with
// the lookback. Ranges must be sorted by Start.
func RangesOverlap(ranges []hintprovider.HintTimeRange, start, end time.Time) bool {
	if len(ranges) == 0 {
		return false
	}
	idx := sort.Search(len(ranges), func(i int) bool {
		return ranges[i].End.After(start)
	})
	return idx < len(ranges) && !ranges[idx].Start.After(end)
}

// MetricLookupBounds rounds onto the step, then widens by the range and offset.
// The query-frontend splitter floors the start and rounds the end up before
// evaluating samples. A zero interval or a zero step is only widened.
func MetricLookupBounds(group syntax.MatcherRange, from, through time.Time, stepMillis int64) (time.Time, time.Time) {
	if group.Interval > 0 && stepMillis > 0 {
		from, through = alignMetricStep(from, through, stepMillis)
	}
	return group.Widen(from, through)
}

// alignMetricStep matches metricQuerySplitter.alignStartEnd: floor the start and
// round the end up to the next step.
func alignMetricStep(start, end time.Time, stepMillis int64) (time.Time, time.Time) {
	stepNs := stepMillis * int64(time.Millisecond)
	startNs := start.UnixNano()
	endNs := end.UnixNano()
	if mod := endNs % stepNs; mod != 0 {
		endNs += stepNs - mod
	}
	return time.Unix(0, startNs-startNs%stepNs).UTC(), time.Unix(0, endNs).UTC()
}

// MetricSample is one value from a metric query, at the time it was evaluated.
type MetricSample struct {
	Time  time.Time
	Value float64
}

// VerifySamples checks hint overage against a list of metric samples.
// A sample is considered "covered" when its Value > 0 and any hint overlaps its lookback.
// Records the full lookback period as a false-negative range if no hints cover it.
func VerifySamples(expr syntax.Expr, hints []hintprovider.HintTimeRange, samples []MetricSample, eligibleEnd time.Time) Report {
	report := Report{HintRanges: len(hints)}
	if IsAbsentOverTime(expr) {
		report.Correct = true
		return report
	}
	group := metricMatcherGroup(expr)
	for _, sample := range samples {
		if sample.Value <= 0 {
			continue
		}
		winStart, winEnd := group.Widen(sample.Time, sample.Time)
		if !eligibleEnd.IsZero() && winEnd.After(eligibleEnd) {
			continue
		}
		report.TotalEntries++
		if !RangesOverlap(hints, winStart, winEnd) {
			report.FalseNegativeRanges = append(report.FalseNegativeRanges, hintprovider.HintTimeRange{
				Start: winStart,
				End:   winEnd,
			})
			continue
		}
		report.CoveredEntries++
	}
	report.FalseNegatives = report.TotalEntries - report.CoveredEntries
	report.Correct = report.FalseNegatives == 0
	return report
}

func metricMatcherGroup(expr syntax.Expr) syntax.MatcherRange {
	groups, err := syntax.MatcherGroups(expr)
	if err != nil || len(groups) != 1 {
		return syntax.MatcherRange{}
	}
	return groups[0]
}

// VerifyEntries checks hint coverage against a list of log entry timestamps.
// Each timestamp is tested against all hint ranges. Records false-negative
// timestamps for diagnostic use.
func VerifyEntries(ranges []hintprovider.HintTimeRange, timestamps []time.Time) Report {
	r := Report{
		HintRanges:   len(ranges),
		TotalEntries: len(timestamps),
	}
	for _, ts := range timestamps {
		if TimestampCovered(ranges, ts) {
			r.CoveredEntries++
		} else {
			r.FalseNegativeTimestamps = append(r.FalseNegativeTimestamps, ts)
		}
	}
	r.FalseNegatives = r.TotalEntries - r.CoveredEntries
	r.Correct = r.FalseNegatives == 0
	return r
}

// CountFalsePositives counts hint ranges that contain no entry from timestamps.
func CountFalsePositives(ranges []hintprovider.HintTimeRange, timestamps []time.Time) int {
	count := 0
	for _, r := range ranges {
		hasEntry := false
		for _, ts := range timestamps {
			if RangeCoversTimestamp(r, ts) {
				hasEntry = true
				break
			}
		}
		if !hasEntry {
			count++
		}
	}
	return count
}

// TimestampCovered returns true when ts falls within at least one hint range.
func TimestampCovered(ranges []hintprovider.HintTimeRange, ts time.Time) bool {
	for _, r := range ranges {
		if RangeCoversTimestamp(r, ts) {
			return true
		}
	}
	return false
}

// RangeCoversTimestamp returns true when ts falls within [start, end).
// Start is inclusive, end is exclusive but equality is also accepted to handle
// the edge case where an entry timestamp equals the range end.
func RangeCoversTimestamp(r hintprovider.HintTimeRange, ts time.Time) bool {
	return (ts.Equal(r.Start) || ts.After(r.Start)) &&
		(ts.Equal(r.End) || ts.Before(r.End))
}
