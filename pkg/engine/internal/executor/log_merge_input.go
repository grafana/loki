package executor

import (
	"bytes"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
)

// mergeInputTracker accumulates input size and duplicate counts over the
// merged record stream of a LogMerge task.
//
// Records must arrive grouped by stream ID and timestamp, as the merge
// iterator yields them. The order of records within a group does not matter.
type mergeInputTracker struct {
	bytes      int64
	duplicates int

	groupStreamID  int64
	groupTimestamp time.Time
	group          []trackedRecord
}

type trackedRecord struct {
	lineHash uint64
	line     []byte
	metadata labels.Labels
}

// observe adds rec to the totals. A record is a duplicate if an earlier record
// has the same stream, timestamp, line, and metadata.
//
// observe keeps references to rec.Line and rec.Metadata until the group
// changes. The caller must not modify them afterwards.
//
// The duplicate check compares each record against every distinct record in
// its group. The cost is quadratic in the group size, but each comparison is
// one uint64 compare unless the line hashes match.
func (t *mergeInputTracker) observe(rec logs.Record) {
	t.bytes += recordBytes(rec)

	if len(t.group) == 0 || rec.StreamID != t.groupStreamID || !rec.Timestamp.Equal(t.groupTimestamp) {
		clear(t.group)
		t.group = t.group[:0]
		t.groupStreamID = rec.StreamID
		t.groupTimestamp = rec.Timestamp
	}

	lineHash := xxhash.Sum64(rec.Line)
	for _, prev := range t.group {
		if prev.lineHash == lineHash && bytes.Equal(prev.line, rec.Line) && labels.Equal(prev.metadata, rec.Metadata) {
			t.duplicates++
			return
		}
	}
	t.group = append(t.group, trackedRecord{lineHash: lineHash, line: rec.Line, metadata: rec.Metadata})
}

// recordBytes returns the size of the line plus all structured metadata
// values. Metadata names are not counted because the logs section stores
// each name once per column, not once per record.
func recordBytes(rec logs.Record) int64 {
	size := int64(len(rec.Line))
	rec.Metadata.Range(func(l labels.Label) {
		size += int64(len(l.Value))
	})
	return size
}
