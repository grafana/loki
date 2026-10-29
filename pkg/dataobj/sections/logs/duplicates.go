package logs

import (
	"bytes"
	"slices"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/prometheus/prometheus/model/labels"
)

// DuplicateTracker detects & counts duplicate records in a stream of records.
//
// Records must arrive grouped by stream ID and timestamp. The order of records
// within a group does not matter.
//
// Equal stream IDs must mean equal labels. Local IDs from different objects
// are not comparable.
//
// The zero value is ready to use.
type DuplicateTracker struct {
	duplicates int

	groupStreamID  int64
	groupTimestamp time.Time
	groupRecords   []trackedRecord
}

type trackedRecord struct {
	lineHash uint64
	line     []byte
	metadata labels.Labels
}

// IsDuplicate counts rec if it is a duplicate, and reports whether it is. A record
// is a duplicate if an earlier record has the same stream, timestamp, line,
// and metadata.
//
// IsDuplicate keeps references to rec.Line and rec.Metadata until the group
// changes. The caller must not modify them afterwards.
//
// IsDuplicate compares each record against every distinct record in its group.
// The cost is quadratic in the group size, but each comparison is one uint64
// compare unless the line hashes match.
func (c *DuplicateTracker) IsDuplicate(rec Record) bool {
	if !c.inCurrentGroup(rec) {
		c.startGroup(rec)
	}

	tracked := newTrackedRecord(rec)
	if c.seenInGroup(tracked) {
		c.duplicates++
		return true
	}
	c.groupRecords = append(c.groupRecords, tracked)
	return false
}

// Duplicates returns the number of duplicate records observed.
func (c *DuplicateTracker) Duplicates() int {
	return c.duplicates
}

// inCurrentGroup reports whether rec has the stream and timestamp of the
// current group.
func (c *DuplicateTracker) inCurrentGroup(rec Record) bool {
	return rec.StreamID == c.groupStreamID && rec.Timestamp.Equal(c.groupTimestamp)
}

// startGroup forgets the records of the current group and starts a new group
// for the stream and timestamp of rec.
func (c *DuplicateTracker) startGroup(rec Record) {
	c.groupRecords = c.groupRecords[:0]
	c.groupStreamID = rec.StreamID
	c.groupTimestamp = rec.Timestamp
}

// seenInGroup reports whether the current group already has a record equal
// to r.
func (c *DuplicateTracker) seenInGroup(r trackedRecord) bool {
	return slices.ContainsFunc(c.groupRecords, r.equal)
}

func newTrackedRecord(rec Record) trackedRecord {
	return trackedRecord{lineHash: xxhash.Sum64(rec.Line), line: rec.Line, metadata: rec.Metadata}
}

// equal reports whether r and o have the same line and metadata. It compares
// the line hashes first because they differ for almost every pair of records.
func (r trackedRecord) equal(o trackedRecord) bool {
	return r.lineHash == o.lineHash && bytes.Equal(r.line, o.line) && labels.Equal(r.metadata, o.metadata)
}
