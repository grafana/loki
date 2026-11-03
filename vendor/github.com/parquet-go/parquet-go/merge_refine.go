package parquet

import (
	"slices"
	"sort"
)

// This file refines overlapping merge segments: when sorted row groups overlap
// only in part of their key ranges (e.g. time-ordered files overlapping around
// their boundaries), the stretches of key space covered by a single row group
// do not need to be merged row by row — they can be sliced off as row-range
// views (see row_range.go) and read or written through the column-oriented
// paths, leaving only the truly overlapping stretches for the loser-tree merge.
//
// Cuts are only ever applied to the row group that is alone in a stretch of
// key space (a "lone stretch"), and are validated against the exact
// row-group-level min/max keys of the other row groups. This avoids a subtle
// hazard: page-granular cuts of different row groups at a common boundary key
// land at different actual keys, which could reorder rows across regions.
// Cutting only the lone row group — with the rows of its boundary pages pushed
// into the adjacent merged region — guarantees that every row of a region
// compares at most equal to every row of the following regions, so
// concatenating the regions preserves sorted order. Ties across row groups
// always land in merged regions, whose loser-tree resolves them.
//
// The refined plan is equivalent to the full merge as a sorted sequence: the
// same multiset of rows, with each input row group's rows in their original
// relative order. The interleaving of rows with equal sort keys across row
// groups may differ from the unrefined merge (as it already does between merge
// arities), which is why refinement is not applied when dropping duplicated
// rows: which physical row of an equal-key run survives deduplication depends
// on that interleaving.

// disableMergeRefinement forces overlapping segments through the unrefined
// merge. It exists only so tests and benchmarks can compare refined plans
// against the reference behavior.
var disableMergeRefinement bool

// minStreamedRegionRows is the minimum number of rows a lone stretch must span
// to be worth slicing out of the merge. Below this, the overhead of an extra
// segment (a seek per column, a row group boundary when writing) outweighs the
// merge work saved, so the stretch stays in the adjacent merged region.
const minStreamedRegionRows = 1024

// refineTarget describes one row group of an overlapping segment for the
// planner. minRow and maxRow are the exact bounds of the sorting columns, as
// computed by rowGroupRangeOfSortedColumns. cutAbove and cutBelow are
// conservative page-granular row lookups on the first sorting column; either
// may be nil when the row group does not support them, in which case the row
// group is never sliced.
type refineTarget struct {
	rowGroup RowGroup
	minRow   Row
	maxRow   Row
	numRows  int64

	// cutAbove returns the smallest row index such that every row at or after
	// it has a sort key strictly greater than key, rounded down to a page
	// boundary of the first sorting column (the rows of the page containing
	// key stay below the cut).
	cutAbove func(key Row) int64

	// cutBelow returns the largest row index such that every row before it
	// has a sort key strictly less than key, rounded down to a page boundary
	// of the first sorting column (the rows of the page containing key stay
	// at or above the cut).
	cutBelow func(key Row) int64
}

// newRefineTargets builds the planner inputs for one overlapping segment. It
// returns nil when the segment cannot be refined at all (bounds unavailable).
// Individual row groups whose page index does not support cut lookups get nil
// cut functions and are never sliced, but still participate in the plan.
func newRefineTargets(segment []rowGroupRange, schema *Schema, sorting []SortingColumn) []refineTarget {
	if len(sorting) == 0 {
		return nil
	}

	// Locate the leaf column of the first sorting column; cuts are computed on
	// its page bounds only. (Strict inequality on the first sort column implies
	// strict inequality on the full sorting tuple, so cuts remain conservative
	// for multi-column sorts.)
	sortingColumnIndex := -1
	sortingColumnPath := columnPath(sorting[0].Path())
	for columnIndex, path := range schema.Columns() {
		if slices.Equal(path, sortingColumnPath) {
			sortingColumnIndex = columnIndex
			break
		}
	}
	if sortingColumnIndex < 0 {
		return nil
	}
	descending := sorting[0].Descending()

	targets := make([]refineTarget, len(segment))
	for i, rr := range segment {
		if rr.minRow == nil || rr.maxRow == nil {
			return nil
		}
		targets[i] = refineTarget{
			rowGroup: rr.rowGroup,
			minRow:   rr.minRow,
			maxRow:   rr.maxRow,
			numRows:  rr.rowGroup.NumRows(),
		}
		targets[i].cutAbove, targets[i].cutBelow = newCutLookups(rr.rowGroup, sortingColumnIndex, descending)
	}
	return targets
}

// newCutLookups builds the page-granular row cut lookups for the sorting leaf
// column of a row group, or (nil, nil) when the page index does not support
// them (missing column or offset index, or null pages in the sorting column,
// whose position in the sort order is not knowable from the index).
func newCutLookups(rg RowGroup, columnIndex int, descending bool) (cutAbove, cutBelow func(Row) int64) {
	chunks := rg.ColumnChunks()
	if columnIndex >= len(chunks) {
		return nil, nil
	}
	chunk := chunks[columnIndex]
	columnType := chunk.Type()

	ci, err := chunk.ColumnIndex()
	if err != nil || ci == nil || ci.NumPages() == 0 {
		return nil, nil
	}
	oi, err := chunk.OffsetIndex()
	if err != nil || oi == nil || oi.NumPages() != ci.NumPages() {
		return nil, nil
	}
	numPages := ci.NumPages()
	for p := range numPages {
		if ci.NullPage(p) {
			return nil, nil
		}
	}
	numRows := rg.NumRows()

	// In-order page bounds: the first value of a page in sort order is its
	// minimum for ascending sorts and its maximum for descending sorts.
	earliest := ci.MinValue
	latest := ci.MaxValue
	if descending {
		earliest, latest = latest, earliest
	}
	// orderCompare orders values by the sort direction.
	orderCompare := func(a, b Value) int {
		return compareValues(a, b, columnType, descending)
	}
	pageEnd := func(p int) int64 {
		if p+1 < numPages {
			return oi.FirstRowIndex(p + 1)
		}
		return numRows
	}

	// cutAbove: end of the last page whose earliest value is at or before key
	// (every row from the cut on belongs to a later page, whose values are all
	// strictly after key).
	cutAbove = func(key Row) int64 {
		kv := key[columnIndex]
		if kv.IsNull() {
			return numRows // no safe cut: the slice comes out empty
		}
		p := sort.Search(numPages, func(p int) bool {
			return orderCompare(earliest(p), kv) > 0
		})
		if p == 0 {
			return 0
		}
		return pageEnd(p - 1)
	}

	// cutBelow: start of the first page whose latest value is at or after key
	// (every row before the cut belongs to an earlier page, whose values are
	// all strictly before key).
	cutBelow = func(key Row) int64 {
		kv := key[columnIndex]
		if kv.IsNull() {
			return 0 // no safe cut: the slice comes out empty
		}
		p := sort.Search(numPages, func(p int) bool {
			return orderCompare(latest(p), kv) >= 0
		})
		if p == numPages {
			return numRows
		}
		return oi.FirstRowIndex(p)
	}

	return cutAbove, cutBelow
}

// refineSegment plans the refinement of one overlapping segment, returning the
// refined sub-segments: an alternation of merged regions (built with
// makeMerged from two or more participants) and single-source row groups or
// range views. It returns nil when no refinement applies, in which case the
// caller merges the whole segment as usual.
func refineSegment(targets []refineTarget, compare func(Row, Row) int, makeMerged func([]RowGroup) RowGroup) []RowGroup {
	if len(targets) < 2 {
		return nil
	}

	type event struct {
		key   Row
		start bool
		index int
	}

	events := make([]event, 0, 2*len(targets))
	for i := range targets {
		events = append(events,
			event{key: targets[i].minRow, start: true, index: i},
			event{key: targets[i].maxRow, start: false, index: i},
		)
	}
	slices.SortStableFunc(events, func(a, b event) int {
		if c := compare(a.key, b.key); c != 0 {
			return c
		}
		// Starts sort before ends so that touching ranges (a max equal to
		// another's min) produce a merged region for the shared key instead
		// of being falsely torn apart.
		switch {
		case a.start && !b.start:
			return -1
		case !a.start && b.start:
			return +1
		default:
			return 0
		}
	})

	// regionPart tracks the target index of each participant so merged
	// regions can list participants deterministically in minRow order.
	type regionPart struct {
		index int
		part  RowGroup
	}

	var (
		plan    []RowGroup
		region  []regionPart     // participants of the merged region being built
		cursors map[int]int64    // rows of each target consumed by earlier regions
		active  map[int]struct{} // targets whose key interval covers the sweep position
		sliced  bool             // whether any lone slice was emitted

		// pendingLone is set while the sweep is inside a stretch of key space
		// covered by a single target; leftK is the exclusive lower bound of
		// the stretch (nil at the start of the segment).
		pendingLone  = -1
		pendingLeftK Row
	)
	cursors = make(map[int]int64, len(targets))
	active = make(map[int]struct{}, len(targets))

	remainder := func(i int) (RowGroup, bool) {
		t := &targets[i]
		off := cursors[i]
		if off >= t.numRows {
			return nil, false
		}
		cursors[i] = t.numRows
		if off == 0 {
			return t.rowGroup, true
		}
		return newRowRangeRowGroup(t.rowGroup, off, t.numRows-off), true
	}

	closeRegion := func() {
		switch len(region) {
		case 0:
		case 1:
			plan = append(plan, region[0].part)
		default:
			slices.SortStableFunc(region, func(a, b regionPart) int { return a.index - b.index })
			parts := make([]RowGroup, len(region))
			for i, p := range region {
				parts[i] = p.part
			}
			plan = append(plan, makeMerged(parts))
		}
		region = nil
	}

	// resolveLone ends the pending lone stretch, bounded above by rightK (nil
	// at the end of the segment). When the stretch is sliceable and large
	// enough, the region below it is closed and the slice emitted; otherwise
	// the stretch simply remains part of the current region.
	resolveLone := func(rightK Row) {
		i := pendingLone
		pendingLone = -1
		t := &targets[i]
		if t.cutAbove == nil || t.cutBelow == nil {
			return
		}
		off := int64(0)
		if pendingLeftK != nil {
			off = t.cutAbove(pendingLeftK)
		}
		end := t.numRows
		if rightK != nil {
			end = t.cutBelow(rightK)
		}
		off = max(off, cursors[i])
		end = min(end, t.numRows)
		if end-off < minStreamedRegionRows {
			return
		}

		// Close the region below the slice: every participant contributes its
		// remaining rows (their key intervals ended inside the region), and
		// the lone target contributes its rows before the slice.
		if off > cursors[i] {
			region = append(region, regionPart{index: i, part: newRowRangeRowGroup(t.rowGroup, cursors[i], off-cursors[i])})
		}
		closeRegion()

		if off == 0 && end == t.numRows {
			plan = append(plan, t.rowGroup)
		} else {
			plan = append(plan, newRowRangeRowGroup(t.rowGroup, off, end-off))
		}
		cursors[i] = end
		sliced = true
	}

	for _, ev := range events {
		if ev.start {
			if pendingLone >= 0 {
				resolveLone(ev.key)
			}
			active[ev.index] = struct{}{}
			if len(active) == 1 {
				// First target of the segment: it is alone until the next
				// start event.
				pendingLone = ev.index
				pendingLeftK = nil
			}
		} else {
			// If the ending target is the pending lone one, its stretch runs
			// to the end of its rows: resolve before consuming the remainder.
			if ev.index == pendingLone {
				resolveLone(nil)
			}
			delete(active, ev.index)
			if r, ok := remainder(ev.index); ok {
				region = append(region, regionPart{index: ev.index, part: r})
			}
			if len(active) == 1 && pendingLone < 0 {
				for lone := range active {
					pendingLone = lone
				}
				pendingLeftK = ev.key
			}
		}
	}
	closeRegion()

	if !sliced {
		return nil
	}
	return plan
}
