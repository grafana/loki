package parquet

import (
	"io"
	"slices"
)

// MultiRowGroup wraps multiple row groups to appear as if it was a single
// RowGroup. RowGroups must have the same schema or it will error.
func MultiRowGroup(rowGroups ...RowGroup) RowGroup {
	if len(rowGroups) == 0 {
		return &emptyRowGroup{}
	}
	if len(rowGroups) == 1 {
		return rowGroups[0]
	}
	schema, err := compatibleSchemaOf(rowGroups)
	if err != nil {
		panic(err)
	}
	return newMultiRowGroup(schema, nil, slices.Clone(rowGroups))
}

func newMultiRowGroup(schema *Schema, sorting []SortingColumn, rowGroups []RowGroup) *multiRowGroup {
	m := new(multiRowGroup)
	m.init(schema, sorting, rowGroups)
	return m
}

func (m *multiRowGroup) init(schema *Schema, sorting []SortingColumn, rowGroups []RowGroup) *multiRowGroup {
	columns := make([]multiColumnChunk, len(schema.Columns()))
	columnChunks := make([][]ColumnChunk, len(rowGroups))

	for i, rowGroup := range rowGroups {
		columnChunks[i] = rowGroup.ColumnChunks()
	}

	for i := range columns {
		columns[i].rowGroup = m
		columns[i].column = i
		columns[i].chunks = make([]ColumnChunk, len(columnChunks))

		for j, chunks := range columnChunks {
			columns[i].chunks[j] = chunks[i]
		}
	}

	m.schema = schema
	m.sorting = sorting
	m.rowGroups = rowGroups
	m.columns = make([]ColumnChunk, len(columns))

	for i := range columns {
		m.columns[i] = &columns[i]
	}

	return m
}

func compatibleSchemaOf(rowGroups []RowGroup) (*Schema, error) {
	schema := rowGroups[0].Schema()

	// Fast path: Many times all row groups have the exact same schema so a
	// pointer comparison is cheaper.
	samePointer := true
	for _, rowGroup := range rowGroups[1:] {
		if rowGroup.Schema() != schema {
			samePointer = false
			break
		}
	}
	if samePointer {
		return schema, nil
	}

	// Slow path: The schema pointers are not the same, but they still have to
	// be compatible.
	for _, rowGroup := range rowGroups[1:] {
		if !EqualNodes(schema, rowGroup.Schema()) {
			return nil, ErrRowGroupSchemaMismatch
		}
	}

	return schema, nil
}

type multiRowGroup struct {
	schema    *Schema
	rowGroups []RowGroup
	columns   []ColumnChunk
	sorting   []SortingColumn
}

func (m *multiRowGroup) NumRows() (numRows int64) {
	for _, rowGroup := range m.rowGroups {
		numRows += rowGroup.NumRows()
	}
	return numRows
}

func (m *multiRowGroup) ColumnChunks() []ColumnChunk { return m.columns }

func (m *multiRowGroup) SortingColumns() []SortingColumn { return m.sorting }

func (m *multiRowGroup) Schema() *Schema { return m.schema }

func (m *multiRowGroup) Rows() Rows { return NewRowGroupRowReader(m) }

type multiColumnChunk struct {
	rowGroup *multiRowGroup
	column   int
	chunks   []ColumnChunk
}

func (c *multiColumnChunk) Type() Type {
	if len(c.chunks) != 0 {
		return c.chunks[0].Type() // all chunks should be of the same type
	}
	return nil
}

func (c *multiColumnChunk) NumValues() int64 {
	n := int64(0)
	for i := range c.chunks {
		n += c.chunks[i].NumValues()
	}
	return n
}

func (c *multiColumnChunk) Column() int {
	return c.column
}

func (c *multiColumnChunk) Pages() Pages {
	return &multiPages{column: c}
}

func (c *multiColumnChunk) ColumnIndex() (ColumnIndex, error) {
	if len(c.chunks) == 0 {
		return emptyColumnIndex{}, nil
	}

	// Collect indexes from all chunks
	indexes := make([]ColumnIndex, len(c.chunks))
	numPages := 0

	for i, chunk := range c.chunks {
		index, err := chunk.ColumnIndex()
		if err != nil {
			return nil, err
		}
		indexes[i] = index
		numPages += index.NumPages()
	}

	// Build cumulative offsets for page index mapping
	offsets := make([]int, len(indexes)+1)
	for i, index := range indexes {
		offsets[i+1] = offsets[i] + index.NumPages()
	}

	return &multiColumnIndex{
		column:   c,
		indexes:  indexes,
		offsets:  offsets,
		numPages: numPages,
		typ:      c.Type(),
	}, nil
}

func (c *multiColumnChunk) OffsetIndex() (OffsetIndex, error) {
	if len(c.chunks) == 0 {
		return emptyOffsetIndex{}, nil
	}

	// Collect indexes from all chunks
	indexes := make([]OffsetIndex, len(c.chunks))
	numPages := 0

	for i, chunk := range c.chunks {
		index, err := chunk.OffsetIndex()
		if err != nil {
			return nil, err
		}
		indexes[i] = index
		numPages += index.NumPages()
	}

	// Build cumulative page offsets
	offsets := make([]int, len(indexes)+1)
	for i, index := range indexes {
		offsets[i+1] = offsets[i] + index.NumPages()
	}

	// Build cumulative row offsets for each chunk
	rowOffsets := make([]int64, len(c.chunks)+1)
	for i := range c.chunks {
		rowOffsets[i+1] = rowOffsets[i] + c.rowGroup.rowGroups[i].NumRows()
	}

	return &multiOffsetIndex{
		column:     c,
		indexes:    indexes,
		offsets:    offsets,
		rowOffsets: rowOffsets,
		numPages:   numPages,
	}, nil
}

func (c *multiColumnChunk) BloomFilter() BloomFilter {
	return multiBloomFilter{c}
}

type multiBloomFilter struct{ *multiColumnChunk }

func (f multiBloomFilter) ReadAt(b []byte, off int64) (int, error) {
	i := 0

	for i < len(f.chunks) {
		if r := f.chunks[i].BloomFilter(); r != nil {
			size := r.Size()
			if off < size {
				break
			}
			off -= size
		}
		i++
	}

	if i == len(f.chunks) {
		return 0, io.EOF
	}

	rn := int(0)
	for len(b) > 0 {
		if i >= len(f.chunks) {
			return rn, io.EOF
		}
		if r := f.chunks[i].BloomFilter(); r != nil {
			n, err := r.ReadAt(b, off)
			rn += n
			if err != nil && err != io.EOF {
				return rn, err
			}
			if b = b[n:]; len(b) == 0 {
				return rn, nil
			}
			// When moving to next chunk, reset offset to 0
			off = 0
		}
		i++
	}

	return rn, nil
}

func (f multiBloomFilter) Size() int64 {
	size := int64(0)
	for _, c := range f.chunks {
		if b := c.BloomFilter(); b != nil {
			size += b.Size()
		}
	}
	return size
}

func (f multiBloomFilter) Check(v Value) (bool, error) {
	for _, c := range f.chunks {
		if b := c.BloomFilter(); b != nil {
			if ok, err := b.Check(v); ok || err != nil {
				return ok, err
			}
		}
	}
	return false, nil
}

type multiColumnIndex struct {
	column   *multiColumnChunk
	indexes  []ColumnIndex
	offsets  []int
	numPages int
	typ      Type
}

func (m *multiColumnIndex) NumPages() int {
	return m.numPages
}

func (m *multiColumnIndex) mapPageIndex(pageIndex int) (chunkIndex, localIndex int) {
	for i := range len(m.offsets) - 1 {
		if pageIndex >= m.offsets[i] && pageIndex < m.offsets[i+1] {
			return i, pageIndex - m.offsets[i]
		}
	}
	// Out of bounds - return last valid position
	if len(m.indexes) > 0 {
		lastIndex := len(m.indexes) - 1
		lastNumPages := m.indexes[lastIndex].NumPages()
		if lastNumPages > 0 {
			return lastIndex, lastNumPages - 1
		}
	}
	return 0, 0
}

func (m *multiColumnIndex) NullCount(pageIndex int) int64 {
	chunkIndex, localIndex := m.mapPageIndex(pageIndex)
	return m.indexes[chunkIndex].NullCount(localIndex)
}

func (m *multiColumnIndex) NullPage(pageIndex int) bool {
	chunkIndex, localIndex := m.mapPageIndex(pageIndex)
	return m.indexes[chunkIndex].NullPage(localIndex)
}

func (m *multiColumnIndex) MinValue(pageIndex int) Value {
	chunkIndex, localIndex := m.mapPageIndex(pageIndex)
	return m.indexes[chunkIndex].MinValue(localIndex)
}

func (m *multiColumnIndex) MaxValue(pageIndex int) Value {
	chunkIndex, localIndex := m.mapPageIndex(pageIndex)
	return m.indexes[chunkIndex].MaxValue(localIndex)
}

func (m *multiColumnIndex) IsAscending() bool {
	if len(m.indexes) == 0 {
		return false
	}

	// All indexes must be ascending
	for _, index := range m.indexes {
		if !index.IsAscending() {
			return false
		}
	}

	// Additionally, check boundary between chunks:
	// max of chunk[i] must be <= min of chunk[i+1]
	if m.typ == nil {
		return true
	}
	cmp := m.typ.Compare
	for i := range len(m.indexes) - 1 {
		currIndex := m.indexes[i]
		nextIndex := m.indexes[i+1]

		// Find last non-null page in current chunk
		lastPage := currIndex.NumPages() - 1
		for lastPage >= 0 && currIndex.NullPage(lastPage) {
			lastPage--
		}

		// Find first non-null page in next chunk
		firstPage := 0
		numPages := nextIndex.NumPages()
		for firstPage < numPages && nextIndex.NullPage(firstPage) {
			firstPage++
		}

		// If both have valid pages, compare max of current to min of next
		if lastPage >= 0 && firstPage < numPages {
			currMax := currIndex.MaxValue(lastPage)
			nextMin := nextIndex.MinValue(firstPage)
			if cmp(currMax, nextMin) > 0 {
				return false // Not ascending across chunk boundary
			}
		}
	}

	return true
}

func (m *multiColumnIndex) IsDescending() bool {
	if len(m.indexes) == 0 {
		return false
	}

	// All indexes must be descending
	for _, index := range m.indexes {
		if !index.IsDescending() {
			return false
		}
	}

	// Additionally, check boundary between chunks:
	// min of chunk[i] must be >= max of chunk[i+1]
	if m.typ == nil {
		return true
	}
	cmp := m.typ.Compare
	for i := range len(m.indexes) - 1 {
		currIndex := m.indexes[i]
		nextIndex := m.indexes[i+1]

		// Find first non-null page in current chunk
		firstPage := 0
		numPages := currIndex.NumPages()
		for firstPage < numPages && currIndex.NullPage(firstPage) {
			firstPage++
		}

		// Find last non-null page in next chunk
		lastPage := nextIndex.NumPages() - 1
		for lastPage >= 0 && nextIndex.NullPage(lastPage) {
			lastPage--
		}

		// If both have valid pages, compare min of current to max of next
		if firstPage < numPages && lastPage >= 0 {
			currMin := currIndex.MinValue(firstPage)
			nextMax := nextIndex.MaxValue(lastPage)
			if cmp(currMin, nextMax) < 0 {
				return false // Not descending across chunk boundary
			}
		}
	}

	return true
}

type multiOffsetIndex struct {
	column     *multiColumnChunk
	indexes    []OffsetIndex
	offsets    []int
	rowOffsets []int64
	numPages   int
}

func (m *multiOffsetIndex) NumPages() int {
	return m.numPages
}

func (m *multiOffsetIndex) mapPageIndex(pageIndex int) (chunkIndex int, localIndex int) {
	for i := range len(m.offsets) - 1 {
		if pageIndex >= m.offsets[i] && pageIndex < m.offsets[i+1] {
			return i, pageIndex - m.offsets[i]
		}
	}
	// Out of bounds - return last valid position
	if len(m.indexes) > 0 {
		lastIndex := len(m.indexes) - 1
		lastNumPages := m.indexes[lastIndex].NumPages()
		if lastNumPages > 0 {
			return lastIndex, lastNumPages - 1
		}
	}
	return 0, 0
}

func (m *multiOffsetIndex) Offset(pageIndex int) int64 {
	chunkIndex, localIndex := m.mapPageIndex(pageIndex)
	return m.indexes[chunkIndex].Offset(localIndex)
}

func (m *multiOffsetIndex) CompressedPageSize(pageIndex int) int64 {
	chunkIndex, localIndex := m.mapPageIndex(pageIndex)
	return m.indexes[chunkIndex].CompressedPageSize(localIndex)
}

func (m *multiOffsetIndex) FirstRowIndex(pageIndex int) int64 {
	chunkIndex, localIndex := m.mapPageIndex(pageIndex)
	localRowIndex := m.indexes[chunkIndex].FirstRowIndex(localIndex)
	return m.rowOffsets[chunkIndex] + localRowIndex
}

type multiPages struct {
	pages  Pages
	index  int
	column *multiColumnChunk
}

func (m *multiPages) ReadPage() (Page, error) {
	for {
		if m.pages != nil {
			p, err := m.pages.ReadPage()
			if err == nil || err != io.EOF {
				return p, err
			}
			if err := m.pages.Close(); err != nil {
				return nil, err
			}
			m.pages = nil
		}

		if m.column == nil || m.index == len(m.column.chunks) {
			return nil, io.EOF
		}

		m.pages = m.column.chunks[m.index].Pages()
		m.index++
	}
}

func (m *multiPages) SeekToRow(rowIndex int64) error {
	if m.column == nil {
		return io.ErrClosedPipe
	}

	if m.pages != nil {
		if err := m.pages.Close(); err != nil {
			return err
		}
	}

	rowGroups := m.column.rowGroup.rowGroups
	numRows := int64(0)
	m.pages = nil
	m.index = 0

	for m.index < len(rowGroups) {
		numRows = rowGroups[m.index].NumRows()
		if rowIndex < numRows {
			break
		}
		rowIndex -= numRows
		m.index++
	}

	if m.index < len(rowGroups) {
		m.pages = m.column.chunks[m.index].Pages()
		m.index++
		return m.pages.SeekToRow(rowIndex)
	}
	return nil
}

func (m *multiPages) Close() (err error) {
	if m.pages != nil {
		err = m.pages.Close()
	}
	m.pages = nil
	m.index = 0
	m.column = nil
	return err
}
