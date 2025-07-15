package parquet

import (
	"io"
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

	rowGroupsCopy := make([]RowGroup, len(rowGroups))
	copy(rowGroupsCopy, rowGroups)

	c := new(multiRowGroup)
	c.init(schema, rowGroupsCopy)
	return c
}

func (c *multiRowGroup) init(schema *Schema, rowGroups []RowGroup) error {
	columns := make([]multiColumnChunk, len(schema.Columns()))

	rowGroupColumnChunks := make([][]ColumnChunk, len(rowGroups))
	for i, rowGroup := range rowGroups {
		rowGroupColumnChunks[i] = rowGroup.ColumnChunks()
	}

	for i := range columns {
		columns[i].rowGroup = c
		columns[i].column = i
		columns[i].chunks = make([]ColumnChunk, len(rowGroupColumnChunks))

		for j, columnChunks := range rowGroupColumnChunks {
			columns[i].chunks[j] = columnChunks[i]
		}
	}

	c.schema = schema
	c.rowGroups = rowGroups
	c.columns = make([]ColumnChunk, len(columns))

	for i := range columns {
		c.columns[i] = &columns[i]
	}

	return nil
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
}

func (c *multiRowGroup) NumRows() (numRows int64) {
	for _, rowGroup := range c.rowGroups {
		numRows += rowGroup.NumRows()
	}
	return numRows
}

func (c *multiRowGroup) ColumnChunks() []ColumnChunk { return c.columns }

func (c *multiRowGroup) SortingColumns() []SortingColumn { return nil }

func (c *multiRowGroup) Schema() *Schema { return c.schema }

func (c *multiRowGroup) Rows() Rows { return NewRowGroupRowReader(c) }

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
	// TODO: implement
	return nil, nil
}

func (c *multiColumnChunk) OffsetIndex() (OffsetIndex, error) {
	// TODO: implement
	return nil, nil
}

func (c *multiColumnChunk) BloomFilter() BloomFilter {
	return multiBloomFilter{c}
}

type multiBloomFilter struct{ *multiColumnChunk }

func (f multiBloomFilter) ReadAt(b []byte, off int64) (int, error) {
	// TODO: add a test for this function
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
		if r := f.chunks[i].BloomFilter(); r != nil {
			n, err := r.ReadAt(b, off)
			rn += n
			if err != nil {
				return rn, err
			}
			if b = b[n:]; len(b) == 0 {
				return rn, nil
			}
			off += int64(n)
		}
		i++
	}

	if i == len(f.chunks) {
		return rn, io.EOF
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
