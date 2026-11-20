package executor

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// comparePostingsRow orders two postings rows using the postings package's
// canonical physical order. Delegating keeps the K-way merge in lockstep with
// the section encoder's write order, which is the single source of truth.
func comparePostingsRow(a, b postings.Row) int {
	return postings.CompareRows(a, b)
}

// postingsDedup finds the rows of the merge stream that repeat a posting
// identity. The identity of a row is its kind, column name, label value, object
// path and section index. Two source index objects that reference the same
// logs section give rows with the same identity.
//
// Rows must arrive in [postings.CompareRows] order. The posting key fields sort
// first, so all rows with one identity have the same posting key and arrive in
// one run. postingsDedup remembers only the rows of the current run.
//
// Memory grows with the longest run. All bloom rows of one column form one
// run, so that run has one entry per logs section that has the column.
//
// The zero value is ready to use.
type postingsDedup struct {
	key    postings.Key
	hasKey bool

	// timeRanges holds the time range of each logs section in the current
	// posting key run.
	timeRanges map[postingsSectionRef]postingsTimeRange

	// peakLen is the largest length of timeRanges since startRun made it. The
	// map keeps the capacity of its peak after clear.
	peakLen int
}

type postingsSectionRef struct {
	objectPath   string
	sectionIndex int64
}

// postingsTimeRange is the time range of a postings row, in unix nanoseconds.
type postingsTimeRange struct {
	min, max int64
}

// postingsDedupShrinkFactor sets when [postingsDedup] replaces its map instead
// of clearing it. It replaces the map when a run uses less than
// 1/postingsDedupShrinkFactor of the peak length.
//
// clear costs time in proportion to the capacity of the map. Without a limit,
// one long run would make each later short run slow. Growing a new map for
// each run is also slow, because runs of one merge often have similar lengths.
const postingsDedupShrinkFactor = 8

// add records row. It returns true if an earlier row is equal to row in
// [postings.CompareRows] order. The merge must drop such a row.
//
// add returns an error if an earlier row has the identity of row but a
// different time range. The source index objects then disagree about one logs
// section, and the merge cannot pick the correct row.
func (d *postingsDedup) add(row postings.Row) (bool, error) {
	if key := row.Key(); !d.hasKey || key.Compare(d.key) != 0 {
		d.startRun(key)
	}

	ref := postingsSectionRef{objectPath: row.ObjectPath, sectionIndex: row.SectionIndex}
	rowRange := postingsTimeRange{min: row.MinTimestamp, max: row.MaxTimestamp}
	earlier, ok := d.timeRanges[ref]
	if !ok {
		d.timeRanges[ref] = rowRange
		return false, nil
	}
	if earlier != rowRange {
		return false, fmt.Errorf("postings rows (kind %d, column %q, value %q) for object %q, section %d have different time ranges [%d, %d] and [%d, %d]",
			row.Kind, row.ColumnName, row.LabelValue, row.ObjectPath, row.SectionIndex, earlier.min, earlier.max, rowRange.min, rowRange.max)
	}
	return true, nil
}

func (d *postingsDedup) startRun(key postings.Key) {
	d.key, d.hasKey = key, true

	runLen := len(d.timeRanges)
	d.peakLen = max(d.peakLen, runLen)
	if d.timeRanges == nil || runLen*postingsDedupShrinkFactor < d.peakLen {
		d.timeRanges = make(map[postingsSectionRef]postingsTimeRange, runLen)
		d.peakLen = runLen
		return
	}
	clear(d.timeRanges)
}
