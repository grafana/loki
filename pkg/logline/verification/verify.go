package verification

import (
	"slices"
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

// Entry is a log entry's timestamp and its stream's fingerprint
// (logline.StreamFingerprint).
type Entry struct {
	Timestamp   time.Time
	Fingerprint uint64
}

// VerifyStreamEntries is VerifyEntries for ranges limited to document shards:
// a range covers an entry only if it also covers the entry's stream.
func VerifyStreamEntries(ranges []hintprovider.HintTimeRange, entries []Entry) Report {
	r := Report{
		HintRanges:   len(ranges),
		TotalEntries: len(entries),
	}
	for _, e := range entries {
		if slices.ContainsFunc(ranges, func(h hintprovider.HintTimeRange) bool { return RangeCoversEntry(h, e) }) {
			r.CoveredEntries++
		} else {
			r.FalseNegativeTimestamps = append(r.FalseNegativeTimestamps, e.Timestamp)
		}
	}
	r.FalseNegatives = r.TotalEntries - r.CoveredEntries
	return r
}

// CountStreamFalsePositives counts hint ranges that cover no entry.
func CountStreamFalsePositives(ranges []hintprovider.HintTimeRange, entries []Entry) int {
	count := 0
	for _, h := range ranges {
		if !slices.ContainsFunc(entries, func(e Entry) bool { return RangeCoversEntry(h, e) }) {
			count++
		}
	}
	return count
}

// RangeCoversEntry returns true when r covers e's timestamp and stream.
func RangeCoversEntry(r hintprovider.HintTimeRange, e Entry) bool {
	return RangeCoversTimestamp(r, e.Timestamp) && r.MatchesFingerprint(e.Fingerprint)
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
