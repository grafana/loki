package executor

import (
	"bytes"
	"slices"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
)

// duplicateCounter counts duplicate records in the merged record stream of a
// LogMerge task.
//
// Records must arrive grouped by stream ID and timestamp, as the merge
// iterator yields them. The order of records within a group does not matter.
//
// Stream IDs must be global IDs from one stream table, so that equal IDs mean
// equal labels. Local IDs from different source objects are not comparable.
// sortmerge.MixedRunIterator yields global IDs because it remaps every section
// through its source's local-to-global map.
type duplicateCounter struct {
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

// observe counts rec if it is a duplicate. A record is a duplicate if an
// earlier record has the same stream, timestamp, line, and metadata.
//
// observe keeps references to rec.Line and rec.Metadata until the group
// changes. The caller must not modify them afterwards.
//
// observe compares each record against every distinct record in its group.
// The cost is quadratic in the group size, but each comparison is one uint64
// compare unless the line hashes match.
func (c *duplicateCounter) observe(rec logs.Record) {
	if !c.inCurrentGroup(rec) {
		c.startGroup(rec)
	}

	tracked := newTrackedRecord(rec)
	if c.seenInGroup(tracked) {
		c.duplicates++
		return
	}
	c.groupRecords = append(c.groupRecords, tracked)
}

// inCurrentGroup reports whether rec has the stream and timestamp of the
// current group.
func (c *duplicateCounter) inCurrentGroup(rec logs.Record) bool {
	return rec.StreamID == c.groupStreamID && rec.Timestamp.Equal(c.groupTimestamp)
}

// startGroup forgets the records of the current group and starts a new group
// for the stream and timestamp of rec.
func (c *duplicateCounter) startGroup(rec logs.Record) {
	c.groupRecords = c.groupRecords[:0]
	c.groupStreamID = rec.StreamID
	c.groupTimestamp = rec.Timestamp
}

// seenInGroup reports whether the current group already has a record equal
// to r.
func (c *duplicateCounter) seenInGroup(r trackedRecord) bool {
	return slices.ContainsFunc(c.groupRecords, r.equal)
}

func newTrackedRecord(rec logs.Record) trackedRecord {
	return trackedRecord{lineHash: xxhash.Sum64(rec.Line), line: rec.Line, metadata: rec.Metadata}
}

// equal reports whether r and o have the same line and metadata. It compares
// the line hashes first because they differ for almost every pair of records.
func (r trackedRecord) equal(o trackedRecord) bool {
	return r.lineHash == o.lineHash && bytes.Equal(r.line, o.line) && labels.Equal(r.metadata, o.metadata)
}
