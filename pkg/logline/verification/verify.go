package verification

import (
	"sort"
	"time"

	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
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
}

// Correct returns true when there are no false negatives.
func (r *Report) Correct() bool {
	return r.FalseNegatives == 0
}

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
		return report
	}
	group := MetricMatcherGroup(expr)
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
	return report
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

// MetricMatcherGroup returns the single matcher group for a supported metric query.
func MetricMatcherGroup(expr syntax.Expr) syntax.MatcherRange {
	groups, err := syntax.MatcherGroups(expr)
	if err != nil || len(groups) != 1 {
		return syntax.MatcherRange{}
	}
	return groups[0]
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
