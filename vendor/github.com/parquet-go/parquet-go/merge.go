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
	// Optimization: detect non-overlapping row groups and create segments
	rowGroupSegments := make([]RowGroup, 0)
	for segment := range overlappingRowGroups(mergedRowGroups, schema, mergedSortingColumns, mergedCompare) {
		if len(segment) == 1 {
			rowGroupSegments = append(rowGroupSegments, segment[0])
		} else {
			merged := &mergedRowGroup{compare: mergedCompare}
			merged.init(schema, mergedSortingColumns, segment)
			rowGroupSegments = append(rowGroupSegments, merged)
		}
	}

	if len(rowGroupSegments) == 1 {
		return rowGroupSegments[0], nil
	}

	return newMultiRowGroup(schema, mergedSortingColumns, rowGroupSegments), nil
}

// overlappingRowGroups analyzes row groups to find non-overlapping segments
// Returns groups of row groups where each group either:
// 1. Contains a single non-overlapping row group (can be concatenated)
// 2. Contains multiple overlapping row groups (need to be merged)
func overlappingRowGroups(rowGroups []RowGroup, schema *Schema, sorting []SortingColumn, compare func(Row, Row) int) iter.Seq[[]RowGroup] {
	return func(yield func([]RowGroup) bool) {
		type rowGroupRange struct {
			rowGroup RowGroup
			minRow   Row
			maxRow   Row
		}

		rowGroupRanges := make([]rowGroupRange, 0, len(rowGroups))
		for _, rg := range rowGroups {
			if rg.NumRows() == 0 {
				continue
			}
			minRow, maxRow, err := rowGroupRangeOfSortedColumns(rg, schema, sorting)
			if err != nil {
				yield(rowGroups)
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
			yield([]RowGroup{rowGroupRanges[0].rowGroup})
			return
		}

		// Sort row groups by their minimum values
		slices.SortFunc(rowGroupRanges, func(a, b rowGroupRange) int {
			return compare(a.minRow, b.minRow)
		})

		// Detect overlapping segments
		currentSegment := []RowGroup{rowGroupRanges[0].rowGroup}
		currentMax := rowGroupRanges[0].maxRow

		for _, rr := range rowGroupRanges[1:] {
			if cmp := compare(rr.minRow, currentMax); cmp <= 0 {
				// Overlapping - add to current segment and extend max if necessary
				currentSegment = append(currentSegment, rr.rowGroup)
				if cmp > 0 {
					currentMax = rr.maxRow
				}
			} else {
				// Non-overlapping - yield current segment
				if !yield(currentSegment) {
					return
				}
				currentSegment = []RowGroup{rr.rowGroup}
				currentMax = rr.maxRow
			}
		}

		if len(currentSegment) > 0 {
			yield(currentSegment)
		}
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

		// Since data is sorted by sorting columns, we can efficiently get min/max:
		// - Min value = min of first non-null page
		// - Max value = max of last non-null page
		numPages := columnIndex.NumPages()

		// Find first non-null page for min value
		var globalMin Value
		var found bool
		for pageIdx := range numPages {
			if !columnIndex.NullPage(pageIdx) {
				if minValue := columnIndex.MinValue(pageIdx); !minValue.IsNull() {
					globalMin, found = minValue, true
					break
				}
			}
		}
		if !found {
			return nil, nil, fmt.Errorf("no valid pages found in column index for column %s", sortingColumnPath)
		}

		// Find last non-null page for max value
		var globalMax Value
		for pageIdx := numPages - 1; pageIdx >= 0; pageIdx-- {
			if !columnIndex.NullPage(pageIdx) {
				if maxValue := columnIndex.MaxValue(pageIdx); !maxValue.IsNull() {
					globalMax = maxValue
					break
				}
			}
		}

		// Set the min/max values with proper levels
		minValues[sortingColumnIndex] = globalMin.Level(0, 1, sortingColumnIndex)
		maxValues[sortingColumnIndex] = globalMax.Level(0, 1, sortingColumnIndex)
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
	compare func(Row, Row) int
}

func (m *mergedRowGroup) Rows() Rows {
	// The row group needs to respect a sorting order; the merged row reader
	// uses a heap to merge rows from the row groups.
	rows := make([]Rows, len(m.rowGroups))
	for i := range rows {
		rows[i] = m.rowGroups[i].Rows()
	}
	return &mergedRowGroupRows{
		merge:  mergeRowReaders(rows, m.compare),
		rows:   rows,
		schema: m.schema,
	}
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
		readers := make([]*bufferedRowReader, len(rows))
		for i, r := range rows {
			buffers[i].rows = r
			readers[i] = &buffers[i]
		}
		return &mergedRowReader{
			compare: compare,
			readers: readers,
		}
	}
}

// mergedRowReader2 is a specialized implementation for merging exactly 2 readers
// that avoids heap overhead by doing direct comparisons
type mergedRowReader2 struct {
	compare     func(Row, Row) int
	readers     [2]*bufferedRowReader
	buffers     [2]bufferedRowReader
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
				rows[n] = append(rows[n][:0], r0.head()...)
				n++
				hasNext0 = r0.next()
				hasNext1 = true
			case cmp > 0:
				rows[n] = append(rows[n][:0], r1.head()...)
				n++
				hasNext0 = true
				hasNext1 = r1.next()
			default:
				rows[n] = append(rows[n][:0], r0.head()...)
				n++
				hasNext0 = r0.next()
				if n < len(rows) {
					rows[n] = append(rows[n][:0], r1.head()...)
					n++
					hasNext1 = r1.next()
				}
			}
			if !hasNext0 || !hasNext1 {
				break
			}
		}
	}

	return n, nil
}

type mergedRowReader struct {
	compare     func(Row, Row) int
	readers     []*bufferedRowReader
	initialized bool
}

func (m *mergedRowReader) initialize() error {
	for i, r := range m.readers {
		switch err := r.read(); err {
		case nil:
		case io.EOF:
			m.readers[i] = nil
		default:
			m.readers = nil
			return err
		}
	}

	n := 0
	for _, r := range m.readers {
		if r != nil {
			m.readers[n] = r
			n++
		}
	}

	clear := m.readers[n:]
	for i := range clear {
		clear[i] = nil
	}

	m.readers = m.readers[:n]
	m.heapInit()
	return nil
}

func (m *mergedRowReader) ReadRows(rows []Row) (n int, err error) {
	if !m.initialized {
		m.initialized = true

		if err := m.initialize(); err != nil {
			return 0, err
		}
	}

	for n < len(rows) && len(m.readers) != 0 {
		r := m.readers[0]
		if r.empty() { // This readers buffer has been exhausted, repopulate it.
			if err := r.read(); err != nil {
				if err == io.EOF {
					m.heapPop()
					continue
				}
				return n, err
			} else {
				if !m.heapDown(0, len(m.readers)) { // heap.Fix
					m.heapUp(0)
				}
				continue
			}
		}

		rows[n] = append(rows[n][:0], r.head()...)
		n++

		if !r.next() {
			return n, nil
		}
		if !m.heapDown(0, len(m.readers)) { // heap.Fix
			m.heapUp(0)
		}
	}

	if len(m.readers) == 0 {
		err = io.EOF
	}

	return n, err
}

func (m *mergedRowReader) heapInit() {
	n := len(m.readers)
	for i := n/2 - 1; i >= 0; i-- {
		m.heapDown(i, n)
	}
}

func (m *mergedRowReader) heapPop() {
	n := len(m.readers) - 1
	m.heapSwap(0, n)
	m.heapDown(0, n)
	m.readers = m.readers[:n]
}

func (m *mergedRowReader) heapUp(j int) {
	for {
		i := (j - 1) / 2 // parent
		if i == j || !(m.compare(m.readers[j].head(), m.readers[i].head()) < 0) {
			break
		}
		m.heapSwap(i, j)
		j = i
	}
}

func (m *mergedRowReader) heapDown(i0, n int) bool {
	i := i0
	for {
		j1 := 2*i + 1
		if j1 >= n || j1 < 0 { // j1 < 0 after int overflow
			break
		}
		j := j1 // left child
		if j2 := j1 + 1; j2 < n && m.compare(m.readers[j2].head(), m.readers[j1].head()) < 0 {
			j = j2 // = 2*i + 2  // right child
		}
		if !(m.compare(m.readers[j].head(), m.readers[i].head()) < 0) {
			break
		}
		m.heapSwap(i, j)
		i = j
	}
	return i > i0
}

func (m *mergedRowReader) heapSwap(i, j int) {
	m.readers[i], m.readers[j] = m.readers[j], m.readers[i]
}

type bufferedRowReader struct {
	rows RowReader
	off  int32
	end  int32
	buf  [24]Row
}

func (r *bufferedRowReader) empty() bool {
	return r.end == r.off
}

func (r *bufferedRowReader) head() Row {
	return r.buf[r.off]
}

func (r *bufferedRowReader) next() bool {
	r.off++
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
	n, err := r.rows.ReadRows(r.buf[r.end:])
	if err != nil && n == 0 {
		return err
	}
	r.end += int32(n)
	return nil
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
		if encoding != nil {
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
			switch {
			case logicalType.List != nil:
				merged = &listNode{group}
			case logicalType.Map != nil:
				merged = &mapNode{group}
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
