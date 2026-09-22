package verification

import (
	"time"

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
	// FalsePositives counts hint ranges that contain no result entries.
	// Only populated when CountFalsePositives is called.
	FalsePositives int
}

// Correct returns true when there are no false negatives.
func (r *Report) Correct() bool {
	return r.FalseNegatives == 0
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
