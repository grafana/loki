package parquet

import (
	"cmp"
	"fmt"
	"io"
	"iter"
	"slices"

	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

// MergeRowGroups constructs a row group which is a merged view of rowGroups. If
// rowGroups are sorted and the passed options include sorting, the merged row
// group will also be sorted.
//
// The function validates the input to ensure that the merge operation is
// possible, ensuring that the schemas match or can be converted to an
// optionally configured target schema passed as argument in the option list.
//
// The sorting columns of each row group are also consulted to determine whether
// the output can be represented. If sorting columns are configured on the merge
// they must be a prefix of sorting columns of all row groups being merged.
func MergeRowGroups(rowGroups []RowGroup, options ...RowGroupOption) (RowGroup, error) {
	config, err := NewRowGroupConfig(options...)
	if err != nil {
		return nil, err
	}

	schema := config.Schema
	if len(rowGroups) == 0 {
		if schema == nil {
			return nil, fmt.Errorf("cannot merge empty row groups without a schema")
		}
		return newEmptyRowGroup(schema), nil
	}
	if schema == nil {
		schemas := make([]Node, len(rowGroups))
		for i, rowGroup := range rowGroups {
			schemas[i] = rowGroup.Schema()
		}
		schema = NewSchema(rowGroups[0].Schema().Name(), MergeNodes(schemas...))
	}

	mergedRowGroups := slices.Clone(rowGroups)
	for i, rowGroup := range mergedRowGroups {
		rowGroupSchema := rowGroup.Schema()
		// Always apply conversion when merging multiple row groups to ensure
		// column indices match the merged schema layout. The merge process can
		// reorder fields even when schemas are otherwise identical.
		conv, err := Convert(schema, rowGroupSchema)
		if err != nil {
			return nil, fmt.Errorf("cannot merge row groups: %w", err)
		}
		mergedRowGroups[i] = ConvertRowGroup(rowGroup, conv)
	}

	// Determine the effective sorting columns for the merge
	mergedSortingColumns := slices.Clone(config.Sorting.SortingColumns)
	if len(mergedSortingColumns) == 0 {
		// Auto-detect common sorting columns from input row groups
		sortingColumns := make([][]SortingColumn, len(mergedRowGroups))
		for i, rowGroup := range mergedRowGroups {
			sortingColumns[i] = rowGroup.SortingColumns()
		}
		mergedSortingColumns = MergeSortingColumns(sortingColumns...)
	}

	if len(mergedSortingColumns) == 0 {
		// When there are no effective sorting columns, use a simpler version of the
		// merger which simply concatenates rows from each of the row groups.
		// This is preferable because it makes the output deterministic, the
		// heap merge may otherwise reorder rows across groups.
		//
		// IMPORTANT: We need to ensure conversions are applied even in the simple
		// concatenation path. Instead of returning the multiRowGroup directly
		// (which bypasses row-level conversion), we create a simple concatenating
		// row reader that preserves the conversion logic.
		return newMultiRowGroup(schema, nil, mergedRowGroups), nil
	}

	mergedCompare := compareRowsFuncOf(schema, mergedSortingColumns)
	dropDuplicatedRows := config.Sorting.DropDuplicatedRows

	// Optimization: detect non-overlapping row groups and create segments
	makeMerged := func(rowGroups []RowGroup) RowGroup {
		merged := &mergedRowGroup{
			compare:            mergedCompare,
			dropDuplicatedRows: dropDuplicatedRows,
		}
		merged.init(schema, mergedSortingColumns, rowGroups)
		return merged
	}

	rowGroupSegments := make([]RowGroup, 0)
	for segment := range overlappingRowGroups(mergedRowGroups, schema, mergedSortingColumns, mergedCompare) {
		if len(segment) == 1 {
			rowGroupSegments = append(rowGroupSegments, segment[0].rowGroup)
			continue
		}
		// Refine partially overlapping segments: stretches of key space
		// covered by a single row group are sliced off as row-range views,
		// leaving only the truly overlapping stretches for the merge. Not
		// applied when dropping duplicated rows: which physical row of an
		// equal-key run survives depends on the interleaving of ties across
		// row groups, which refinement does not preserve.
		if !dropDuplicatedRows && !disableMergeRefinement {
			if refined := refineSegment(newRefineTargets(segment, schema, mergedSortingColumns), mergedCompare, makeMerged); refined != nil {
				rowGroupSegments = append(rowGroupSegments, refined...)
				continue
			}
		}
		rowGroups := make([]RowGroup, len(segment))
		for i := range segment {
			rowGroups[i] = segment[i].rowGroup
		}
		rowGroupSegments = append(rowGroupSegments, makeMerged(rowGroups))
	}

	if len(rowGroupSegments) == 1 {
		rg := rowGroupSegments[0]
		if dropDuplicatedRows {
			if _, isMerged := rg.(*mergedRowGroup); !isMerged {
				return &dedupRowGroup{
					RowGroup: rg,
					compare:  mergedCompare,
				}, nil
			}
		}
		return rg, nil
	}

	m := newMultiRowGroup(schema, mergedSortingColumns, rowGroupSegments)
	return &sortedSegmentRowGroup{
		multiRowGroup:      *m,
		segments:           rowGroupSegments,
		compare:            mergedCompare,
		dropDuplicatedRows: dropDuplicatedRows,
	}, nil
}

// rowGroupRange associates a row group with the bounds of its sorting columns
// (nil when the bounds could not be determined).
type rowGroupRange struct {
	rowGroup RowGroup
	minRow   Row
	maxRow   Row
}

// overlappingRowGroups analyzes row groups to find non-overlapping segments
// Returns groups of row groups where each group either:
// 1. Contains a single non-overlapping row group (can be concatenated)
// 2. Contains multiple overlapping row groups (need to be merged)
func overlappingRowGroups(rowGroups []RowGroup, schema *Schema, sorting []SortingColumn, compare func(Row, Row) int) iter.Seq[[]rowGroupRange] {
	return func(yield func([]rowGroupRange) bool) {
		rowGroupRanges := make([]rowGroupRange, 0, len(rowGroups))
		for _, rg := range rowGroups {
			if rg.NumRows() == 0 {
				continue
			}
			minRow, maxRow, err := rowGroupRangeOfSortedColumns(rg, schema, sorting)
			if err != nil {
				// Bounds unavailable: yield all row groups as a single segment
				// with nil bounds, which the caller merges without refinement.
				all := make([]rowGroupRange, len(rowGroups))
				for i, rg := range rowGroups {
					all[i] = rowGroupRange{rowGroup: rg}
				}
				yield(all)
				return
			}
			rowGroupRanges = append(rowGroupRanges, rowGroupRange{
				rowGroup: rg,
				minRow:   minRow,
				maxRow:   maxRow,
			})
		}
		if len(rowGroupRanges) == 0 {
			return
		}
		if len(rowGroupRanges) == 1 {
			yield(rowGroupRanges[:1])
			return
		}

		// Sort row groups by their minimum values
		slices.SortFunc(rowGroupRanges, func(a, b rowGroupRange) int {
			return compare(a.minRow, b.minRow)
		})

		// Detect overlapping segments
		segmentStart := 0
		currentMax := rowGroupRanges[0].maxRow

		for i, rr := range rowGroupRanges[1:] {
			if compare(rr.minRow, currentMax) <= 0 {
				// Overlapping - extend max if necessary
				if compare(rr.maxRow, currentMax) > 0 {
					currentMax = rr.maxRow
				}
			} else {
				// Non-overlapping - yield current segment
				if !yield(rowGroupRanges[segmentStart : i+1]) {
					return
				}
				segmentStart = i + 1
				currentMax = rr.maxRow
			}
		}

		yield(rowGroupRanges[segmentStart:])
	}
}

func rowGroupRangeOfSortedColumns(rg RowGroup, schema *Schema, sorting []SortingColumn) (minRow, maxRow Row, err error) {
	// Extract min/max values from column indices
	columnChunks := rg.ColumnChunks()
	columns := schema.Columns()
	minValues := make([]Value, len(columns))
	maxValues := make([]Value, len(columns))

	// Fill in default null values for non-sorting columns
	for i := range columns {
		minValues[i] = Value{}.Level(0, 0, i)
		maxValues[i] = Value{}.Level(0, 0, i)
	}

	for _, sortingColumn := range sorting {
		// Find column index
		sortingColumnIndex := -1
		sortingColumnPath := columnPath(sortingColumn.Path())
		for columnIndex, columnPath := range columns {
			if slices.Equal(columnPath, sortingColumnPath) {
				sortingColumnIndex = columnIndex
				break
			}
		}
		if sortingColumnIndex < 0 {
			return nil, nil, fmt.Errorf("sorting column %v not found in schema", sortingColumnPath)
		}

		columnChunk := columnChunks[sortingColumnIndex]
		columnIndex, err := columnChunk.ColumnIndex()
		if err != nil || columnIndex == nil || columnIndex.NumPages() == 0 {
			// No column index available - fall back to merging
			return nil, nil, fmt.Errorf("column index not available for sorting column %s", sortingColumnPath)
		}

		// Since data is sorted by sorting columns, we can bound the first and
		// last rows of the row group from the first and last non-null pages of
		// the column index. The bounds are expressed in sort order: when the
		// column is sorted in descending order, the first row holds the
		// largest value and the last row holds the smallest one, so the page
		// statistics to read are inverted.
		numPages := columnIndex.NumPages()
		descending := sortingColumn.Descending()

		// Bound the first row in sort order from the first non-null page
		var firstValue Value
		var found bool
		for pageIdx := range numPages {
			if !columnIndex.NullPage(pageIdx) {
				value := columnIndex.MinValue(pageIdx)
				if descending {
					value = columnIndex.MaxValue(pageIdx)
				}
				if !value.IsNull() {
					firstValue, found = value, true
					break
				}
			}
		}
		if !found {
			return nil, nil, fmt.Errorf("no valid pages found in column index for column %s", sortingColumnPath)
		}

		// Bound the last row in sort order from the last non-null page
		var lastValue Value
		for pageIdx := numPages - 1; pageIdx >= 0; pageIdx-- {
			if !columnIndex.NullPage(pageIdx) {
				value := columnIndex.MaxValue(pageIdx)
				if descending {
					value = columnIndex.MinValue(pageIdx)
				}
				if !value.IsNull() {
					lastValue = value
					break
				}
			}
		}

		// Set the min/max values with proper levels
		minValues[sortingColumnIndex] = firstValue.Level(0, 1, sortingColumnIndex)
		maxValues[sortingColumnIndex] = lastValue.Level(0, 1, sortingColumnIndex)
	}

	minRow = Row(minValues)
	maxRow = Row(maxValues)
	return
}

// compareValues compares two parquet values, taking into account the descending flag
func compareValues(a, b Value, columnType Type, descending bool) int {
	cmp := columnType.Compare(a, b)
	if descending {
		return -cmp
	}
	return cmp
}

type mergedRowGroup struct {
	multiRowGroup
	compare            func(Row, Row) int
	dropDuplicatedRows bool
}

// rowGroupSegments opts out of segment splitting: a mergedRowGroup interleaves
// rows from overlapping row groups via a heap merge, so its row groups cannot be
// written independently without losing the merged ordering. It overrides the
// method promoted from the embedded multiRowGroup.
func (m *mergedRowGroup) rowGroupSegments() []RowGroup { return nil }

func (m *mergedRowGroup) Rows() Rows {
	// The row group needs to respect a sorting order; the merged row reader
	// uses a heap to merge rows from the row groups.
	rows := make([]Rows, len(m.rowGroups))
	for i := range rows {
		rows[i] = m.rowGroups[i].Rows()
	}
	var merge RowReader = mergeRowReaders(rows, m.compare)
	if m.dropDuplicatedRows {
		merge = DedupeRowReader(merge, m.compare)
	}
	return &mergedRowGroupRows{
		merge:  merge,
		rows:   rows,
		schema: m.schema,
	}
}

// sortedSegmentRowGroup wraps a multiRowGroup but overrides Rows() to
// concatenate from each segment's Rows() reader in sequence. This preserves
// the heap merge ordering within mergedRowGroup segments, which would be
// bypassed if multiRowGroup.Rows() read column pages directly.
type sortedSegmentRowGroup struct {
	multiRowGroup
	segments           []RowGroup
	compare            func(Row, Row) int
	dropDuplicatedRows bool
}

// rowGroupSegments implements orderedRowGroupSegments: the segments are already
// in sorted order and each segment's own Rows() preserves its internal ordering,
// so writing them in sequence preserves the global order. It returns nil when
// dropping duplicates, because deduplication spans segment boundaries and would
// be lost if segments were written independently.
func (s *sortedSegmentRowGroup) rowGroupSegments() []RowGroup {
	if s.dropDuplicatedRows {
		return nil
	}
	return s.segments
}

func (s *sortedSegmentRowGroup) Rows() Rows {
	readers := make([]Rows, len(s.segments))
	for i, seg := range s.segments {
		readers[i] = seg.Rows()
	}
	var reader RowReader = &concatenatingRows{
		readers: readers,
		schema:  s.schema,
	}
	if s.dropDuplicatedRows {
		reader = DedupeRowReader(reader, s.compare)
	}
	return &concatenatingRowsWrapper{
		reader:  reader,
		readers: readers,
		schema:  s.schema,
	}
}

// dedupRowGroup wraps a single RowGroup and applies deduplication to its Rows().
type dedupRowGroup struct {
	RowGroup
	compare func(Row, Row) int
}

func (d *dedupRowGroup) Rows() Rows {
	rows := d.RowGroup.Rows()
	reader := DedupeRowReader(rows, d.compare)
	return &concatenatingRowsWrapper{
		reader:  reader,
		readers: []Rows{rows},
		schema:  d.RowGroup.Schema(),
	}
}

// concatenatingRows reads from a sequence of Rows readers, advancing to the
// next reader when the current one returns io.EOF.
type concatenatingRows struct {
	readers []Rows
	index   int
	schema  *Schema
}

func (c *concatenatingRows) ReadRows(rows []Row) (int, error) {
	for c.index < len(c.readers) {
		n, err := c.readers[c.index].ReadRows(rows)
		if err == io.EOF {
			c.index++
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
	return 0, io.EOF
}

// concatenatingRowsWrapper implements the Rows interface by wrapping a
// RowReader (which may include dedup) and managing the underlying Rows
// readers for Close and SeekToRow.
type concatenatingRowsWrapper struct {
	reader   RowReader
	readers  []Rows
	schema   *Schema
	rowIndex int64
}

func (c *concatenatingRowsWrapper) ReadRows(rows []Row) (int, error) {
	n, err := c.reader.ReadRows(rows)
	c.rowIndex += int64(n)
	return n, err
}

func (c *concatenatingRowsWrapper) Close() (lastErr error) {
	for _, r := range c.readers {
		if err := r.Close(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

func (c *concatenatingRowsWrapper) SeekToRow(rowIndex int64) error {
	if rowIndex < c.rowIndex {
		return fmt.Errorf("SeekToRow: concatenating row reader cannot seek backward from row %d to %d", c.rowIndex, rowIndex)
	}
	// Forward seek by reading and discarding rows
	discard := make([]Row, 64)
	for c.rowIndex < rowIndex {
		n := min(int(rowIndex-c.rowIndex), len(discard))
		n, err := c.reader.ReadRows(discard[:n])
		c.rowIndex += int64(n)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *concatenatingRowsWrapper) Schema() *Schema {
	return c.schema
}

type mergedRowGroupRows struct {
	merge     RowReader
	rowIndex  int64
	seekToRow int64
	rows      []Rows
	schema    *Schema
}

func (r *mergedRowGroupRows) Close() (lastErr error) {
	r.rowIndex = -1
	r.seekToRow = 0

	for _, rows := range r.rows {
		if err := rows.Close(); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

func (r *mergedRowGroupRows) ReadRows(rows []Row) (int, error) {
	if r.rowIndex < 0 {
		return 0, io.EOF
	}

	for r.rowIndex < r.seekToRow {
		n := min(int(r.seekToRow-r.rowIndex), len(rows))
		n, err := r.merge.ReadRows(rows[:n])
		if err != nil {
			return 0, err
		}
		rows = rows[n:]
		r.rowIndex += int64(n)
	}

	n, err := r.merge.ReadRows(rows)
	r.rowIndex += int64(n)
	return n, err
}

func (r *mergedRowGroupRows) SeekToRow(rowIndex int64) error {
	if r.rowIndex < 0 {
		return fmt.Errorf("SeekToRow: cannot seek to %d on closed merged row group rows", rowIndex)
	}
	if rowIndex >= r.rowIndex {
		r.seekToRow = rowIndex
		return nil
	}
	return fmt.Errorf("SeekToRow: merged row reader cannot seek backward from row %d to %d", r.rowIndex, rowIndex)
}

func (r *mergedRowGroupRows) Schema() *Schema {
	return r.schema
}

// MergeRowReader constructs a RowReader which creates an ordered sequence of
// all the readers using the given compare function as the ordering predicate.
func MergeRowReaders(rows []RowReader, compare func(Row, Row) int) RowReader {
	return mergeRowReaders(rows, compare)
}

func mergeRowReaders[T RowReader](rows []T, compare func(Row, Row) int) RowReader {
	switch len(rows) {
	case 0:
		return emptyRows{}
	case 1:
		return rows[0]
	case 2:
		return &mergedRowReader2{
			compare: compare,
			buffers: [2]bufferedRowReader{
				{rows: rows[0]},
				{rows: rows[1]},
			},
		}
	default:
		buffers := make([]bufferedRowReader, len(rows))
		for i, r := range rows {
			buffers[i].rows = r
		}
		return &mergedRowReader{
			compare: compare,
			buffers: buffers,
		}
	}
}

// mergedRowReader2 is a specialized implementation for merging exactly 2 readers
// that avoids heap overhead by doing direct comparisons
type mergedRowReader2 struct {
	compare     func(Row, Row) int
	readers     [2]*bufferedRowReader
	buffers     [2]bufferedRowReader
	prev        int   // <0 if r0 won the previous game, >0 if r1 won, 0 otherwise
	streak      int32 // consecutive games won by the same reader
	initialized bool
}

func (m *mergedRowReader2) initialize() error {
	for i := range m.buffers {
		r := &m.buffers[i]
		switch err := r.read(); err {
		case nil:
			m.readers[i] = r
		case io.EOF:
			m.readers[i] = nil
		default:
			return err
		}
	}
	return nil
}

func (m *mergedRowReader2) ReadRows(rows []Row) (n int, err error) {
	if !m.initialized {
		m.initialized = true
		if err := m.initialize(); err != nil {
			return 0, err
		}
	}

	r0 := m.readers[0]
	r1 := m.readers[1]

	if r0 != nil && r0.empty() {
		if err := r0.read(); err != nil {
			if err != io.EOF {
				return 0, err
			}
			r0, m.readers[0] = nil, nil
		}
	}

	if r1 != nil && r1.empty() {
		if err := r1.read(); err != nil {
			if err != io.EOF {
				return 0, err
			}
			r1, m.readers[1] = nil, nil
		}
	}

	if r0 == nil && r1 == nil {
		return 0, io.EOF
	}

	switch {
	case r0 == nil:
		for n < len(rows) {
			rows[n] = append(rows[n][:0], r1.head()...)
			n++
			if !r1.next() {
				break
			}
		}

	case r1 == nil:
		for n < len(rows) {
			rows[n] = append(rows[n][:0], r0.head()...)
			n++
			if !r0.next() {
				break
			}
		}

	default:
		var hasNext0 bool
		var hasNext1 bool

		for n < len(rows) {
			switch cmp := m.compare(r0.head(), r1.head()); {
			case cmp < 0:
				if m.prev < 0 {
					m.streak++
				} else {
					m.streak = 0
				}
				m.prev = -1
				if m.streak >= runDetectionStreak {
					// r0 has been winning: gallop through its buffered rows
					// for the run that sorts strictly before r1's head (ties
					// are emitted pairwise by the case below) and emit it in
					// bulk. Interleaved inputs rarely reach the streak
					// threshold and pay only the cost of the counter.
					n, hasNext0 = m.emitRun(rows, n, r0, r1.head())
					hasNext1 = true
				} else {
					rows[n] = append(rows[n][:0], r0.head()...)
					n++
					hasNext0 = r0.next()
					hasNext1 = true
				}
			case cmp > 0:
				if m.prev > 0 {
					m.streak++
				} else {
					m.streak = 0
				}
				m.prev = 1
				if m.streak >= runDetectionStreak {
					n, hasNext1 = m.emitRun(rows, n, r1, r0.head())
					hasNext0 = true
				} else {
					rows[n] = append(rows[n][:0], r1.head()...)
					n++
					hasNext0 = true
					hasNext1 = r1.next()
				}
			default:
				rows[n] = append(rows[n][:0], r0.head()...)
				n++
				hasNext0 = r0.next()
				if n < len(rows) {
					rows[n] = append(rows[n][:0], r1.head()...)
					n++
					hasNext1 = r1.next()
				}
				m.prev = 0
				m.streak = 0
			}
			if !hasNext0 || !hasNext1 {
				break
			}
		}
	}

	return n, nil
}

// emitRun bulk-emits the run of buffered rows of r that sort strictly before
// bound; the head of r must already be known to. It returns the new number of
// rows produced and whether r has more rows buffered.
func (m *mergedRowReader2) emitRun(rows []Row, n int, r *bufferedRowReader, bound Row) (int, bool) {
	window := r.window()
	if max := len(rows) - n; len(window) > max {
		window = window[:max]
	}
	run := 1
	if len(window) > 1 {
		run += runLength(window[1:], bound, m.compare, -1)
	}
	for _, row := range window[:run] {
		rows[n] = append(rows[n][:0], row...)
		n++
	}
	return n, r.advance(int32(run))
}

// mergedRowReader merges k buffered row readers using a tournament tree of
// losers, which performs ~log2(k) comparisons per row instead of the ~2*log2(k)
// required to sift down a binary min-heap.
//
// The tree is laid out as the first k positions of a 2k binary heap: internal
// node i stores the loser of the game played at that position, identified by
// the index of its buffer, and the leaf for buffer i sits at implicit position
// k+i. The overall winner is kept out of the tree in the winner field, and its
// leaf position in winnerLeaf. Advancing the merge replays the games on the
// path from the winner's leaf to the root, comparing the new head of the
// winner against the losers stored along the way. Exhausted readers are
// represented by a negative value which loses every game it plays.
type mergedRowReader struct {
	compare     func(Row, Row) int
	buffers     []bufferedRowReader
	losers      []int32
	count       int
	winner      int32
	winnerLeaf  int32
	streak      int32 // consecutive games won by the current winner
	initialized bool
}

// runDetectionStreak is the number of consecutive games the same reader must
// win before the merge switches to run mode. When the merged inputs are mostly
// disjoint (e.g. sorted row groups overlapping only at their boundaries), the
// same reader wins long streaks and replaying ~log2(k) games per row is wasted
// work; run mode computes the second-smallest head once and then emits rows
// with a single comparison each. Interleaved inputs never reach the threshold
// and pay only the cost of maintaining the streak counter.
const runDetectionStreak = 3

func (m *mergedRowReader) initialize() error {
	k := len(m.buffers)
	m.losers = make([]int32, k)
	m.count = k

	leaves := make([]int32, k)
	for i := range leaves {
		leaves[i] = int32(i)
	}

	for i := range m.buffers {
		switch err := m.buffers[i].read(); err {
		case nil:
		case io.EOF:
			leaves[i] = -1
			m.count--
		default:
			m.count = 0
			return err
		}
	}

	if m.count > 0 {
		m.winner = m.playInitialGames(0, leaves)
		m.winnerLeaf = int32(k) + m.winner
	}
	return nil
}

func (m *mergedRowReader) ReadRows(rows []Row) (n int, err error) {
	if !m.initialized {
		m.initialized = true

		if err := m.initialize(); err != nil {
			return 0, err
		}
	}

	for n < len(rows) && m.count != 0 {
		c := &m.buffers[m.winner]

		if c.empty() { // This reader's buffer has been exhausted, repopulate it.
			switch err := c.read(); err {
			case nil:
			case io.EOF:
				m.winner = -1
				m.count--
				if m.count == 0 {
					break
				}
			default:
				return n, err
			}
			// The winner's head changed (or the winner was exhausted), replay
			// the games on the path from its leaf to the root to determine the
			// new winner.
			prev := m.winner
			m.replayGames()
			if m.winner != prev {
				m.streak = 0
			}
			continue
		}

		rows[n] = append(rows[n][:0], c.head()...)
		n++

		if !c.next() {
			return n, nil
		}

		if m.streak >= runDetectionStreak {
			// The same reader has been winning; emit its run in bulk. The run
			// bound is the second-smallest head across all readers, so the
			// winner keeps winning as long as its head does not exceed it
			// (ties favor the incumbent, matching the strict comparison in
			// replayGames). Other readers are not consumed during the run, so
			// the bound remains valid until it is crossed. The length of the
			// run within the buffered window is found with O(log n)
			// comparisons: a single comparison of the last buffered row
			// settles the common case where the whole window is part of the
			// run.
			bound := m.runBound()
			for n < len(rows) {
				window := c.window()
				if max := len(rows) - n; len(window) > max {
					window = window[:max]
				}
				run := len(window)
				if bound != nil {
					run = runLength(window, bound, m.compare, 0)
				}
				for _, row := range window[:run] {
					rows[n] = append(rows[n][:0], row...)
					n++
				}
				if !c.advance(int32(run)) {
					return n, nil
				}
				if run < len(window) {
					break // the run bound was crossed
				}
			}
			m.streak = 0
			m.replayGames()
			continue
		}

		prev := m.winner
		m.replayGames()
		if m.winner == prev {
			m.streak++
		} else {
			m.streak = 0
		}
	}

	if m.count == 0 {
		err = io.EOF
	}

	return n, err
}

// runBound returns the second-smallest head among the merged readers, i.e. the
// row that the current winner's head must not exceed for the winner to keep
// winning. In a tournament tree of losers the runner-up necessarily lost its
// game directly against the overall winner, so it is stored at one of the
// nodes on the winner's path from leaf to root; the minimum over those players
// is the runner-up. Returns nil when the winner is the only reader left.
func (m *mergedRowReader) runBound() (bound Row) {
	for offset := (m.winnerLeaf - 1) / 2; ; offset = (offset - 1) / 2 {
		if player := m.losers[offset]; player >= 0 {
			if head := m.buffers[player].head(); bound == nil || m.compare(head, bound) < 0 {
				bound = head
			}
		}
		if offset == 0 {
			return bound
		}
	}
}

// playInitialGames recursively plays the tournament rooted at position i,
// storing the loser of each game at the internal node where it was played,
// and returns the winner of the subtree.
func (m *mergedRowReader) playInitialGames(i int32, leaves []int32) int32 {
	k := int32(len(m.buffers))
	if i >= k { // leaf or out of bounds
		if i -= k; int(i) < len(leaves) {
			return leaves[i]
		}
		return -1
	}
	n1 := m.playInitialGames(2*i+1, leaves)
	n2 := m.playInitialGames(2*i+2, leaves)
	loser, winner := m.playGame(n1, n2)
	m.losers[i] = loser
	return winner
}

func (m *mergedRowReader) playGame(n1, n2 int32) (loser, winner int32) {
	if n1 < 0 {
		return n1, n2
	}
	if n2 < 0 {
		return n2, n1
	}
	if m.compare(m.buffers[n1].head(), m.buffers[n2].head()) < 0 {
		return n2, n1
	}
	return n1, n2
}

// replayGames walks the path from the current winner's leaf to the root,
// playing the winner against the losers stored along the way and exchanging
// them when they win, then records the new overall winner.
func (m *mergedRowReader) replayGames() {
	winner := m.winner
	for offset := (m.winnerLeaf - 1) / 2; ; offset = (offset - 1) / 2 {
		player := m.losers[offset]

		if player >= 0 {
			if winner < 0 || m.compare(m.buffers[player].head(), m.buffers[winner].head()) < 0 {
				m.losers[offset] = winner
				winner = player
			}
		}

		if offset == 0 {
			break
		}
	}
	m.winner = winner
	m.winnerLeaf = int32(len(m.buffers)) + winner
}

// minRowBufferSize is the initial buffer size of a bufferedRowReader, and
// maxRowBufferSize the size it can grow to. Buffers grow exponentially as
// long as the underlying reader keeps filling them completely, so merges of
// small row groups do not pay for full-size buffers while sustained merges
// amortize refills and extend the reach of the bulk run emission paths,
// which are bounded by the buffered window.
const (
	minRowBufferSize = 24
	maxRowBufferSize = 192
)

type bufferedRowReader struct {
	rows RowReader
	off  int32
	end  int32
	full bool // the last read filled the buffer completely
	buf  []Row
}

func (r *bufferedRowReader) empty() bool {
	return r.end == r.off
}

func (r *bufferedRowReader) head() Row {
	return r.buf[r.off]
}

func (r *bufferedRowReader) next() bool {
	return r.advance(1)
}

// window returns the buffered rows that have not been consumed yet.
func (r *bufferedRowReader) window() []Row {
	return r.buf[r.off:r.end]
}

// advance consumes n buffered rows and reports whether more remain buffered.
func (r *bufferedRowReader) advance(n int32) bool {
	r.off += n
	hasNext := r.off < r.end
	if !hasNext {
		// We need to read more rows, however it is unsafe to do so here because we haven't
		// returned the current rows to the caller yet which may cause buffer corruption.
		r.off = 0
		r.end = 0
	}
	return hasNext
}

func (r *bufferedRowReader) read() error {
	if r.buf == nil {
		r.buf = make([]Row, minRowBufferSize)
	} else if r.full && r.off == 0 && r.end == 0 && len(r.buf) < maxRowBufferSize {
		// The reader keeps filling the buffer completely: grow it to amortize
		// refills and extend the bulk run emission paths. The previous rows
		// are carried over so their backing arrays keep being reused.
		buf := make([]Row, min(2*len(r.buf), maxRowBufferSize))
		copy(buf, r.buf)
		r.buf = buf
	}
	n, err := r.rows.ReadRows(r.buf[r.end:])
	if err != nil && n == 0 {
		return err
	}
	r.end += int32(n)
	r.full = r.end == int32(len(r.buf))
	return nil
}

// runLength returns the number of leading rows of window whose comparison
// against bound is at most max (0 to include ties, -1 to exclude them). The
// window must be sorted by the same comparison function, so the qualifying
// rows form a prefix; the function checks the first and last rows to settle
// empty and complete runs with a single comparison, then gallops with a
// binary search refinement, costing O(log n) comparisons instead of the O(n)
// of a linear scan.
func runLength(window []Row, bound Row, compare func(Row, Row) int, max int) int {
	if len(window) == 0 || compare(window[0], bound) > max {
		return 0
	}
	if compare(window[len(window)-1], bound) <= max {
		return len(window)
	}
	lo, hi := 0, 1
	for hi < len(window) && compare(window[hi], bound) <= max {
		lo = hi
		hi *= 2
	}
	hi = min(hi, len(window))
	for lo+1 < hi {
		if mid := int(uint(lo+hi) >> 1); compare(window[mid], bound) <= max {
			lo = mid
		} else {
			hi = mid
		}
	}
	return hi
}

var (
	_ RowReaderWithSchema = (*mergedRowGroupRows)(nil)
)

// MergeNodes takes a list of nodes and greedily retains properties of the schemas:
// - keeps last compression that is not nil
// - keeps last non-plain encoding that is not nil
// - keeps last non-zero field id
// - union of all columns for group nodes
// - retains the most permissive repetition (required < optional < repeated)
func MergeNodes(nodes ...Node) Node {
	switch len(nodes) {
	case 0:
		return nil
	case 1:
		return nodes[0]
	default:
		merged := nodes[0]
		for _, node := range nodes[1:] {
			merged = mergeTwoNodes(merged, node)
		}
		return merged
	}
}

// mergeTwoNodes merges two nodes using greedy property retention
func mergeTwoNodes(a, b Node) Node {
	leaf1 := a.Leaf()
	leaf2 := b.Leaf()
	// Both must be either leaf or group nodes
	if leaf1 != leaf2 {
		// Cannot merge leaf with group - return the last one
		return b
	}

	var merged Node
	if leaf1 {
		// Prefer the type with a logical type annotation if one exists.
		// This ensures that logical types like JSON are preserved when merging
		// a typed node (from an authoritative schema) with a plain node (from
		// reflection-based schema generation).
		merged = Leaf(selectLogicalType(b.Type(), a.Type()))

		// Apply compression (keep last non-nil)
		compression1 := a.Compression()
		compression2 := b.Compression()
		compression := cmp.Or(compression2, compression1)
		if compression != nil {
			merged = Compressed(merged, compression)
		}

		// Apply encoding (keep last non-plain, non-nil)
		encoding := encoding.Encoding(&Plain)
		encoding1 := a.Encoding()
		encoding2 := b.Encoding()
		if !isPlainEncoding(encoding1) {
			encoding = encoding1
		}
		if !isPlainEncoding(encoding2) {
			encoding = encoding2
		}
		if encoding != nil && canEncode(encoding, merged.Type().Kind()) {
			merged = Encoded(merged, encoding)
		}
	} else {
		fields1 := slices.Clone(a.Fields())
		fields2 := slices.Clone(b.Fields())
		sortFields(fields1)
		sortFields(fields2)

		group := make(Group, len(fields1))
		i1 := 0
		i2 := 0
		for i1 < len(fields1) && i2 < len(fields2) {
			name1 := fields1[i1].Name()
			name2 := fields2[i2].Name()
			switch {
			case name1 < name2:
				group[name1] = nullable(fields1[i1])
				i1++
			case name1 > name2:
				group[name2] = nullable(fields2[i2])
				i2++
			default:
				group[name1] = mergeTwoNodes(fields1[i1], fields2[i2])
				i1++
				i2++
			}
		}

		for _, field := range fields1[i1:] {
			group[field.Name()] = nullable(field)
		}

		for _, field := range fields2[i2:] {
			group[field.Name()] = nullable(field)
		}

		merged = group

		if logicalType := b.Type().LogicalType(); logicalType != nil {
			switch logicalType.Value.(type) {
			case *format.ListType:
				merged = &listNode{group}
			case *format.MapType:
				merged = &mapNode{group}
			case *format.VariantType:
				merged = &variantNode{group}
			}
		}
	}

	// Apply repetition (most permissive: required < optional < repeated)
	if a.Repeated() || b.Repeated() {
		merged = Repeated(merged)
	} else if a.Optional() || b.Optional() {
		merged = Optional(merged)
	} else {
		merged = Required(merged)
	}

	// Apply field ID (keep last non-zero)
	return FieldID(merged, cmp.Or(b.ID(), a.ID()))
}

// isPlainEncoding checks if the encoding is plain encoding
func isPlainEncoding(enc encoding.Encoding) bool {
	return enc == nil || enc.Encoding() == format.Plain
}

func nullable(n Node) Node {
	if !n.Repeated() {
		return Optional(n)
	}
	return n
}

func selectLogicalType(t1, t2 Type) Type {
	if t1.LogicalType() != nil {
		return t1
	}
	return t2
}

// MergeSortingColumns returns the common prefix of all sorting columns passed as arguments.
// This function is used to determine the resulting sorting columns when merging multiple
// row groups that each have their own sorting columns.
//
// The function returns the longest common prefix where all sorting columns match exactly
// (same path, same descending flag, same nulls first flag). If any row group has no
// sorting columns, or if there's no common prefix, an empty slice is returned.
//
// Example:
//
//	columns1 := []SortingColumn{Ascending("A"), Ascending("B"), Descending("C")}
//	columns2 := []SortingColumn{Ascending("A"), Ascending("B"), Ascending("D")}
//	result := MergeSortingColumns(columns1, columns2)
//	// result will be []SortingColumn{Ascending("A"), Ascending("B")}
func MergeSortingColumns(sortingColumns ...[]SortingColumn) []SortingColumn {
	if len(sortingColumns) == 0 {
		return nil
	}
	merged := slices.Clone(sortingColumns[0])
	for _, columns := range sortingColumns[1:] {
		merged = commonSortingPrefix(merged, columns)
	}
	return merged
}

// commonSortingPrefix returns the common prefix of two sorting column slices
func commonSortingPrefix(a, b []SortingColumn) []SortingColumn {
	minLen := min(len(a), len(b))
	for i := range minLen {
		if !equalSortingColumn(a[i], b[i]) {
			return a[:i]
		}
	}
	return a[:minLen]
}
