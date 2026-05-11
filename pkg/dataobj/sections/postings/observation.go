package postings

import "time"

// LabelObservation describes a single observation of a label posting (a stream
// label value) for the postings index.
//
// Multiple observations sharing the same (ObjectPath, SectionIndex, ColumnName,
// LabelValue) are aggregated into a single posting that tracks the union of
// stream IDs, the min/max timestamps seen, and the total uncompressed size.
type LabelObservation struct {
	// ObjectPath is the path of the data object that originated this posting.
	ObjectPath string
	// SectionIndex is the index of the logs section within ObjectPath.
	SectionIndex int64
	// ColumnName is the name of the labels column being indexed.
	ColumnName string
	// LabelValue is the observed value of the label.
	LabelValue string
	// StreamID is the (rewritten) stream ID associated with the observation.
	StreamID int64
	// Timestamp is the timestamp of the log record that produced the observation.
	Timestamp time.Time
	// UncompressedSize is the uncompressed size in bytes of the log record that
	// produced the observation. It is summed across all observations in the
	// aggregated posting.
	UncompressedSize int64
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
