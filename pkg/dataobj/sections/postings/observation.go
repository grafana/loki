package postings

import "time"

// TimeRange is an inclusive range of log record timestamps. The minimum is
// never after the maximum.
//
// The zero value is the range that holds only the zero time.Time.
type TimeRange struct {
	min, max time.Time
}

// At returns the range that holds only ts.
func At(ts time.Time) TimeRange {
	return TimeRange{min: ts, max: ts}
}

// Extend returns the smallest range that holds both r and ts.
func (r TimeRange) Extend(ts time.Time) TimeRange {
	if ts.Before(r.min) {
		r.min = ts
	}
	if ts.After(r.max) {
		r.max = ts
	}
	return r
}

// Min returns the earliest timestamp in the range.
func (r TimeRange) Min() time.Time { return r.min }

// Max returns the latest timestamp in the range.
func (r TimeRange) Max() time.Time { return r.max }

// LabelObservation describes an observation of a label posting (a stream
// label value) for the postings index. One observation can cover one or more
// log records of the same stream.
//
// Multiple observations sharing the same (ObjectPath, SectionIndex, ColumnName,
// LabelValue) are aggregated into a single posting that tracks the union of
// stream IDs, the min/max timestamps seen, and the total uncompressed size.
type LabelObservation struct {
	// ObjectPath is the path of the data object that originated this posting.
	ObjectPath string
	// ShardBuckets is the total number of shard buckets used by ObjectPath.
	ShardBuckets int64
	// SectionIndex is the index of the logs section within ObjectPath.
	SectionIndex int64
	// ColumnName is the name of the labels column being indexed.
	ColumnName string
	// LabelValue is the observed value of the label.
	LabelValue string
	// StreamID is the (rewritten) stream ID associated with the observation.
	StreamID int64
	// Time is the range of timestamps of the log records that produced the
	// observation.
	Time TimeRange
	// UncompressedSize is the total uncompressed size in bytes of the log
	// records that produced the observation. It is summed across all
	// observations in the aggregated posting.
	UncompressedSize int64
	// The shard bucket associated with the log stream being observed.
	ShardBucket uint32
}

// BloomObservation describes a single observation of a bloom posting (a
// metadata column value) for the postings index.
//
// Multiple observations sharing the same (ObjectPath, SectionIndex, ColumnName)
// are aggregated into a single posting whose bloom filter contains every
// observed Value, alongside the union of stream IDs, the min/max timestamps
// seen, and the total uncompressed size.
//
// PrepareBloomColumn must be called for the same (ObjectPath, SectionIndex,
// ColumnName) before any observations are recorded.
type BloomObservation struct {
	// ObjectPath is the path of the data object that originated this posting.
	ObjectPath string
	// ShardBuckets is the number of shard buckets used by ObjectPath.
	ShardBuckets int64
	// SectionIndex is the index of the logs section within ObjectPath.
	SectionIndex int64
	// ColumnName is the name of the metadata column being indexed.
	ColumnName string
	// Value is the observed column value to add to the bloom filter.
	Value string
	// StreamID is the (rewritten) stream ID associated with the observation.
	StreamID int64
	// Timestamp is the timestamp of the log record that produced the observation.
	Timestamp time.Time
	// UncompressedSize is the uncompressed size in bytes of the log record that
	// produced the observation. It is summed across all observations in the
	// aggregated posting.
	UncompressedSize int64
}
