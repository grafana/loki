package parquet

import (
	"container/heap"
	"errors"
	"fmt"
	"io"
	"sync"
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
		return newEmptyRowGroup(schema), nil
	}
	if schema == nil {
		schema = rowGroups[0].Schema()

		for _, rowGroup := range rowGroups[1:] {
			if !EqualNodes(schema, rowGroup.Schema()) {
				return nil, ErrRowGroupSchemaMismatch
			}
		}
	}

	mergedRowGroups := make([]RowGroup, len(rowGroups))
	copy(mergedRowGroups, rowGroups)

	for i, rowGroup := range mergedRowGroups {
		if rowGroupSchema := rowGroup.Schema(); !EqualNodes(schema, rowGroupSchema) {
			conv, err := Convert(schema, rowGroupSchema)
			if err != nil {
				return nil, fmt.Errorf("cannot merge row groups: %w", err)
			}
			mergedRowGroups[i] = ConvertRowGroup(rowGroup, conv)
		}
	}

	m := &mergedRowGroup{sorting: config.Sorting.SortingColumns}
	m.init(schema, mergedRowGroups)

	if len(m.sorting) == 0 {
		// When the row group has no ordering, use a simpler version of the
		// merger which simply concatenates rows from each of the row groups.
		// This is preferable because it makes the output deterministic, the
		// heap merge may otherwise reorder rows across groups.
		return &m.multiRowGroup, nil
	}

	for _, rowGroup := range m.rowGroups {
		if !sortingColumnsHavePrefix(rowGroup.SortingColumns(), m.sorting) {
			return nil, ErrRowGroupSortingColumnsMismatch
		}
	}

	m.compare = compareRowsFuncOf(schema, m.sorting)
	return m, nil
}

type mergedRowGroup struct {
	multiRowGroup
	sorting []SortingColumn
	compare func(Row, Row) int
}

func (m *mergedRowGroup) SortingColumns() []SortingColumn {
	return m.sorting
}

func (m *mergedRowGroup) Rows() Rows {
	// The row group needs to respect a sorting order; the merged row reader
	// uses a heap to merge rows from the row groups.
	rows := make([]Rows, len(m.rowGroups))
	for i := range rows {
		rows[i] = m.rowGroups[i].Rows()
	}
	return &mergedRowGroupRows{
		merge: mergedRowReader{
			compare: m.compare,
			readers: makeBufferedRowReaders(len(rows), func(i int) RowReader { return rows[i] }),
		},
		rows:   rows,
		schema: m.schema,
	}
}

type mergedRowGroupRows struct {
	merge     mergedRowReader
	rowIndex  int64
	seekToRow int64
	rows      []Rows
	schema    *Schema
}

func (r *mergedRowGroupRows) WriteRowsTo(w RowWriter) (n int64, err error) {
	b := newMergeBuffer()
	b.setup(r.rows, r.merge.compare)
	n, err = b.WriteRowsTo(w)
	r.rowIndex += int64(n)
	b.release()
	return
}

func (r *mergedRowGroupRows) readInternal(rows []Row) (int, error) {
	n, err := r.merge.ReadRows(rows)
	r.rowIndex += int64(n)
	return n, err
}

func (r *mergedRowGroupRows) Close() (lastErr error) {
	r.merge.close()
	r.rowIndex = 0
	r.seekToRow = 0

	for _, rows := range r.rows {
		if err := rows.Close(); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

func (r *mergedRowGroupRows) ReadRows(rows []Row) (int, error) {
	for r.rowIndex < r.seekToRow {
		n := int(r.seekToRow - r.rowIndex)
		if n > len(rows) {
			n = len(rows)
		}
		n, err := r.readInternal(rows[:n])
		if err != nil {
			return 0, err
		}
	}

	return r.readInternal(rows)
}

func (r *mergedRowGroupRows) SeekToRow(rowIndex int64) error {
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
func MergeRowReaders(readers []RowReader, compare func(Row, Row) int) RowReader {
	return &mergedRowReader{
		compare: compare,
		readers: makeBufferedRowReaders(len(readers), func(i int) RowReader { return readers[i] }),
	}
}

func makeBufferedRowReaders(numReaders int, readerAt func(int) RowReader) []*bufferedRowReader {
	buffers := make([]bufferedRowReader, numReaders)
	readers := make([]*bufferedRowReader, numReaders)

	for i := range readers {
		buffers[i].rows = readerAt(i)
		readers[i] = &buffers[i]
	}

	return readers
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
	heap.Init(m)
	return nil
}

func (m *mergedRowReader) close() {
	for _, r := range m.readers {
		r.close()
	}
	m.readers = nil
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
		if r.end == r.off { // This readers buffer has been exhausted, repopulate it.
			if err := r.read(); err != nil {
				if err == io.EOF {
					heap.Pop(m)
					continue
				}
				return n, err
			} else {
				heap.Fix(m, 0)
				continue
			}
		}

		rows[n] = append(rows[n][:0], r.head()...)
		n++

		if err := r.next(); err != nil {
			if err != io.EOF {
				return n, err
			}
			return n, nil
		} else {
			heap.Fix(m, 0)
		}
	}

	if len(m.readers) == 0 {
		err = io.EOF
	}

	return n, err
}

func (m *mergedRowReader) Less(i, j int) bool {
	return m.compare(m.readers[i].head(), m.readers[j].head()) < 0
}

func (m *mergedRowReader) Len() int {
	return len(m.readers)
}

func (m *mergedRowReader) Swap(i, j int) {
	m.readers[i], m.readers[j] = m.readers[j], m.readers[i]
}

func (m *mergedRowReader) Push(x interface{}) {
	panic("NOT IMPLEMENTED")
}

func (m *mergedRowReader) Pop() interface{} {
	i := len(m.readers) - 1
	r := m.readers[i]
	m.readers = m.readers[:i]
	return r
}

type bufferedRowReader struct {
	rows RowReader
	off  int32
	end  int32
	buf  [10]Row
}

func (r *bufferedRowReader) head() Row {
	return r.buf[r.off]
}

func (r *bufferedRowReader) next() error {
	if r.off++; r.off == r.end {
		r.off = 0
		r.end = 0
		// We need to read more rows, however it is unsafe to do so here because we haven't
		// returned the current rows to the caller yet which may cause buffer corruption.
		return io.EOF
	}
	return nil
}

func (r *bufferedRowReader) read() error {
	if r.rows == nil {
		return io.EOF
	}
	n, err := r.rows.ReadRows(r.buf[r.end:])
	if err != nil && n == 0 {
		return err
	}
	r.end += int32(n)
	return nil
}

func (r *bufferedRowReader) close() {
	r.rows = nil
	r.off = 0
	r.end = 0
}

type mergeBuffer struct {
	compare func(Row, Row) int
	rows    []Rows
	buffer  [][]Row
	head    []int
	len     int
	copy    [mergeBufferSize]Row
}

const mergeBufferSize = 1 << 10

func newMergeBuffer() *mergeBuffer {
	return mergeBufferPool.Get().(*mergeBuffer)
}

var mergeBufferPool = &sync.Pool{
	New: func() any {
		return new(mergeBuffer)
	},
}

func (m *mergeBuffer) setup(rows []Rows, compare func(Row, Row) int) {
	m.compare = compare
	m.rows = append(m.rows, rows...)
	size := len(rows)
	if len(m.buffer) < size {
		extra := size - len(m.buffer)
		b := make([][]Row, extra)
		for i := range b {
			b[i] = make([]Row, 0, mergeBufferSize)
		}
		m.buffer = append(m.buffer, b...)
		m.head = append(m.head, make([]int, extra)...)
	}
	m.len = size
}

func (m *mergeBuffer) reset() {
	for i := range m.rows {
		m.buffer[i] = m.buffer[i][:0]
		m.head[i] = 0
	}
	m.rows = m.rows[:0]
	m.compare = nil
	for i := range m.copy {
		m.copy[i] = nil
	}
	m.len = 0
}

func (m *mergeBuffer) release() {
	m.reset()
	mergeBufferPool.Put(m)
}

func (m *mergeBuffer) fill() error {
	m.len = len(m.rows)
	for i := range m.rows {
		if m.head[i] < len(m.buffer[i]) {
			// There is still rows data in m.buffer[i]. Skip filling the row buffer until
			// all rows have been read.
			continue
		}
		m.head[i] = 0
		m.buffer[i] = m.buffer[i][:mergeBufferSize]
		n, err := m.rows[i].ReadRows(m.buffer[i])
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return err
			}
		}
		m.buffer[i] = m.buffer[i][:n]
	}
	heap.Init(m)
	return nil
}

func (m *mergeBuffer) Less(i, j int) bool {
	x := m.buffer[i]
	if len(x) == 0 {
		return false
	}
	y := m.buffer[j]
	if len(y) == 0 {
		return true
	}
	return m.compare(x[m.head[i]], y[m.head[j]]) == -1
}

func (m *mergeBuffer) Pop() interface{} {
	m.len--
	// We don't use the popped value.
	return nil
}

func (m *mergeBuffer) Len() int {
	return m.len
}

func (m *mergeBuffer) Swap(i, j int) {
	m.buffer[i], m.buffer[j] = m.buffer[j], m.buffer[i]
	m.head[i], m.head[j] = m.head[j], m.head[i]
}

func (m *mergeBuffer) Push(x interface{}) {
	panic("NOT IMPLEMENTED")
}

func (m *mergeBuffer) WriteRowsTo(w RowWriter) (n int64, err error) {
	err = m.fill()
	if err != nil {
		return 0, err
	}
	var count int
	for m.left() {
		size := m.read()
		if size == 0 {
			break
		}
		count, err = w.WriteRows(m.copy[:size])
		if err != nil {
			return
		}
		n += int64(count)
		err = m.fill()
		if err != nil {
			return
		}
	}
	return
}

func (m *mergeBuffer) left() bool {
	for i := 0; i < m.len; i++ {
		if m.head[i] < len(m.buffer[i]) {
			return true
		}
	}
	return false
}

func (m *mergeBuffer) read() (n int64) {
	for n < int64(len(m.copy)) && m.Len() != 0 {
		r := m.buffer[:m.len][0]
		if len(r) == 0 {
			heap.Pop(m)
			continue
		}
		m.copy[n] = append(m.copy[n][:0], r[m.head[0]]...)
		m.head[0]++
		n++
		if m.head[0] < len(r) {
			// There is still rows in this row group. Adjust  the heap
			heap.Fix(m, 0)
		} else {
			heap.Pop(m)
		}
	}
	return
}

var (
	_ RowReaderWithSchema = (*mergedRowGroupRows)(nil)
	_ RowWriterTo         = (*mergedRowGroupRows)(nil)
	_ heap.Interface      = (*mergeBuffer)(nil)
	_ RowWriterTo         = (*mergeBuffer)(nil)
)
