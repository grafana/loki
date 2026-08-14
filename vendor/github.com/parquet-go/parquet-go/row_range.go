package parquet

import (
	"fmt"
	"io"
)

// This file implements row-range views of row groups: a rowRangeRowGroup
// presents rows [off, off+length) of a base row group, with column chunks that
// seek to the range start and stop after the range's row count. Range views are
// produced by the merge planner (see merge_refine.go) to slice partially
// overlapping row groups into disjoint stretches — which stream through the
// writer's column-oriented fast paths — and truly overlapping stretches, which
// go through the loser-tree merge.

// rowRangeRowGroup is a view of rows [off, off+length) of a base row group.
type rowRangeRowGroup struct {
	base   RowGroup
	off    int64
	length int64
	chunks []ColumnChunk
}

// newRowRangeRowGroup returns a view of rows [off, off+length) of base. The
// caller is responsible for bounds: 0 <= off, off+length <= base.NumRows(),
// length > 0, and for not constructing views that span the whole base row
// group (use the base row group directly instead, which preserves its
// eligibility for the verbatim copy fast path).
func newRowRangeRowGroup(base RowGroup, off, length int64) *rowRangeRowGroup {
	baseChunks := base.ColumnChunks()
	rg := &rowRangeRowGroup{
		base:   base,
		off:    off,
		length: length,
		chunks: make([]ColumnChunk, len(baseChunks)),
	}

	// The value count of a row range is only knowable from metadata when each
	// row holds exactly one value, i.e. for non-repeated columns.
	repeated := make([]bool, len(baseChunks))
	if schema := base.Schema(); schema != nil {
		forEachLeafColumnOf(schema, func(leaf leafColumn) {
			if int(leaf.columnIndex) < len(repeated) {
				repeated[leaf.columnIndex] = leaf.maxRepetitionLevel > 0
			}
		})
	}

	for i, chunk := range baseChunks {
		rg.chunks[i] = &rangeColumnChunk{
			base:   chunk,
			off:    off,
			length: length,
			exact:  !repeated[i],
		}
	}
	return rg
}

func (rg *rowRangeRowGroup) NumRows() int64                  { return rg.length }
func (rg *rowRangeRowGroup) ColumnChunks() []ColumnChunk     { return rg.chunks }
func (rg *rowRangeRowGroup) Schema() *Schema                 { return rg.base.Schema() }
func (rg *rowRangeRowGroup) SortingColumns() []SortingColumn { return rg.base.SortingColumns() }
func (rg *rowRangeRowGroup) Rows() Rows                      { return NewRowGroupRowReader(rg) }

// chunkTransparentRowGroup marks range views as safe for the chunk-level write
// fast paths: Rows() is NewRowGroupRowReader over the range chunks, so reading
// the chunks in order is the definition of its row semantics.
func (rg *rowRangeRowGroup) chunkTransparentRowGroup() {}

// rangeColumnChunk presents rows [off, off+length) of a base column chunk.
type rangeColumnChunk struct {
	base   ColumnChunk
	off    int64
	length int64
	exact  bool // NumValues is exact (non-repeated column)
}

func (c *rangeColumnChunk) Type() Type  { return c.base.Type() }
func (c *rangeColumnChunk) Column() int { return c.base.Column() }

func (c *rangeColumnChunk) Pages() Pages {
	pages := c.base.Pages()
	if err := pages.SeekToRow(c.off); err != nil {
		pages.Close()
		return &errorPages{err: fmt.Errorf("seeking to start of row range at %d: %w", c.off, err)}
	}
	return &rangePages{base: pages, off: c.off, length: c.length, remaining: c.length}
}

// The page index and bloom filter of the base chunk describe all of its rows;
// exposing them for a sub-range would let consumers draw wrong conclusions
// (e.g. bounds that rows in the range never reach), so range views report them
// as absent.
func (c *rangeColumnChunk) ColumnIndex() (ColumnIndex, error) { return nil, ErrMissingColumnIndex }
func (c *rangeColumnChunk) OffsetIndex() (OffsetIndex, error) { return nil, ErrMissingOffsetIndex }
func (c *rangeColumnChunk) BloomFilter() BloomFilter          { return nil }

// NumValues returns the number of values in the row range. For non-repeated
// columns this is exactly the row count; for repeated columns the count is not
// knowable from metadata and the base chunk's total is returned as an upper
// bound (see exactNumValues).
func (c *rangeColumnChunk) NumValues() int64 {
	if c.exact {
		return c.length
	}
	return c.base.NumValues()
}

// exactNumValues reports whether NumValues is exact rather than an upper
// bound. Consumers that size resources from NumValues (bloom filters) must
// check this and fall back when the count is inexact.
func (c *rangeColumnChunk) exactNumValues() bool { return c.exact }

// rangePages limits a Pages stream positioned inside its base column chunk to
// the rows [off, off+length) of the base, slicing the final page when the
// range ends mid-page.
type rangePages struct {
	base      Pages
	off       int64 // start of the range, in base row indexes
	length    int64
	remaining int64
}

func (p *rangePages) ReadPage() (Page, error) {
	if p.remaining <= 0 {
		return nil, io.EOF
	}
	page, err := p.base.ReadPage()
	if err != nil {
		return nil, err
	}
	numRows := page.NumRows()
	if numRows <= p.remaining {
		p.remaining -= numRows
		return page, nil
	}
	// The range ends inside this page. The slice shares the page's underlying
	// buffers without incrementing their reference counts, so ownership of the
	// page transfers to the caller through the slice: the caller's Release of
	// the slice releases the shared buffers.
	tail := page.Slice(0, p.remaining)
	p.remaining = 0
	return tail, nil
}

// SeekToRow positions the stream on the row at index rowIndex within the
// range (i.e. relative to the start of the range, not of the base chunk).
func (p *rangePages) SeekToRow(rowIndex int64) error {
	if rowIndex < 0 || rowIndex > p.length {
		return fmt.Errorf("SeekToRow: row index %d out of range of row range view of %d rows", rowIndex, p.length)
	}
	if err := p.base.SeekToRow(p.off + rowIndex); err != nil {
		return err
	}
	p.remaining = p.length - rowIndex
	return nil
}

func (p *rangePages) Close() error {
	p.remaining = 0
	return p.base.Close()
}

// errorPages is a Pages implementation which returns an error on every read.
type errorPages struct{ err error }

func (p *errorPages) ReadPage() (Page, error) { return nil, p.err }
func (p *errorPages) SeekToRow(int64) error   { return p.err }
func (p *errorPages) Close() error            { return nil }

var (
	_ RowGroup    = (*rowRangeRowGroup)(nil)
	_ ColumnChunk = (*rangeColumnChunk)(nil)
	_ Pages       = (*rangePages)(nil)
)
