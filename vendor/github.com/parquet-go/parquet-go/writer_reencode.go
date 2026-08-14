package parquet

import (
	"io"
	"sync/atomic"
)

// This file implements the "L3" re-encode path for Writer.WriteRowGroup: when a
// single file-backed source row group's schema matches the writer but it cannot
// be copied verbatim (the configured codec/encoding/etc. differ), re-encode it
// column-by-column instead of assembling rows and then deconstructing them.
//
// The regular fallback reads the source columns, interleaves their values into
// Row objects, then immediately tears those rows back apart into columns to
// re-encode. L3 skips that round-trip: it reads each column's values directly
// and writes them straight through the column writer (which still re-encodes,
// re-compresses, computes statistics, and populates bloom filters exactly as the
// row path would).
//
// L3 still decodes and re-encodes values, so it is far less of a win than the
// verbatim copy (L0); its benefit is avoiding the row round-trip (measured
// ~1.4–1.8x faster with notably less garbage). See
// docs/merge-copy-optimization.md.

// reencodePathCounter counts row groups written via the L3 column-oriented
// re-encode path. Test/benchmark instrumentation only.
var reencodePathCounter atomic.Int64

// disableWriteReencode forces WriteRowGroup onto the row-oriented fallback. It
// exists only so benchmarks can measure L3 against an otherwise identical
// baseline.
var disableWriteReencode bool

// columnOrientedRowGroup reports whether every column of rowGroup can be read
// column-wise while preserving row order, and the row count fits the configured
// row group size. This holds for file-backed chunks (a single row group of a
// parquet file) and for in-memory column buffers (parquet.Buffer /
// GenericBuffer, whose columns are reordered together when sorted); it excludes
// overlapping heap merges, whose row order depends on cross-column
// interleaving. The size check ensures column-wise writing does not produce a
// row group larger than the row path would. Row groups whose Rows() adds
// semantics on top of the chunks (deduplication, value conversion) are rejected
// by chunkTransparentRowGroup — reading their chunks directly would bypass
// those semantics.
func (w *Writer) columnOrientedRowGroup(rowGroup RowGroup) ([]ColumnChunk, bool) {
	if !chunkTransparentRowGroup(rowGroup) {
		return nil, false
	}
	dst := w.writer.currentRowGroup.columns
	columns := rowGroup.ColumnChunks()
	if len(columns) == 0 || len(columns) != len(dst) {
		return nil, false
	}
	for _, col := range columns {
		if !columnOrientedChunk(col) {
			return nil, false
		}
	}
	if rowGroup.NumRows() > w.writer.currentRowGroup.maxRows {
		return nil, false
	}
	return columns, true
}

// columnOrientedChunk reports whether a column chunk can be read column-wise
// while preserving row order: file-backed chunks, in-memory column buffers,
// and row-range views over either.
func columnOrientedChunk(col ColumnChunk) bool {
	switch c := col.(type) {
	case *FileColumnChunk:
		return true
	case ColumnBuffer:
		return true
	case *rangeColumnChunk:
		return columnOrientedChunk(c.base)
	default:
		return false
	}
}

// reencodableRowGroup reports whether rowGroup can be written via the L3
// column-oriented re-encode path.
func (w *Writer) reencodableRowGroup(rowGroup RowGroup) ([]ColumnChunk, bool) {
	if disableWriteReencode {
		return nil, false
	}
	return w.columnOrientedRowGroup(rowGroup)
}

// writeSegmentsPacked writes the segments of a split merge, packing consecutive
// file-backed (non-overlapping) segments into output row groups up to the
// configured MaxRowsPerRowGroup. A single-segment batch is written through the
// normal path (preserving the verbatim L0 fast path for the 1:1 case); a
// multi-segment batch is consolidated column-by-column via L3 into one output
// row group, undershooting MaxRowsPerRowGroup as needed. Non-file-backed
// segments (overlapping heap merges) or segments larger than the limit flush the
// pending batch and are written individually through the row path.
func (w *Writer) writeSegmentsPacked(segments []RowGroup, schema *Schema, sortingColumns []SortingColumn) (int64, error) {
	maxRows := w.writer.currentRowGroup.maxRows

	var (
		total       int64
		pending     []RowGroup
		pendingRows int64
	)

	flushPending := func() error {
		var (
			n   int64
			err error
		)
		switch len(pending) {
		case 0:
			return nil
		case 1:
			n, err = w.WriteRowGroup(pending[0])
		default:
			n, err = w.packSegmentsByColumn(pending, schema, sortingColumns)
		}
		total += n
		pending = pending[:0]
		pendingRows = 0
		return err
	}

	for _, seg := range segments {
		if _, ok := w.columnOrientedRowGroup(seg); ok {
			if pendingRows > 0 && pendingRows+seg.NumRows() > maxRows {
				if err := flushPending(); err != nil {
					return total, err
				}
			}
			pending = append(pending, seg)
			pendingRows += seg.NumRows()
			continue
		}
		if err := flushPending(); err != nil {
			return total, err
		}
		n, err := w.WriteRowGroup(seg)
		total += n
		if err != nil {
			return total, err
		}
	}
	if err := flushPending(); err != nil {
		return total, err
	}
	return total, nil
}

// packSegmentsByColumn consolidates several file-backed segments into a single
// output row group by re-encoding each column across all segments in order.
func (w *Writer) packSegmentsByColumn(segments []RowGroup, schema *Schema, sortingColumns []SortingColumn) (int64, error) {
	if err := w.writer.flush(); err != nil {
		return 0, err
	}
	dst := w.writer.currentRowGroup.columns
	w.configureBloomFiltersForSegments(segments)
	for _, seg := range segments {
		columns := seg.ColumnChunks()
		for i := range columns {
			if err := copyColumnValues(dst[i], columns[i]); err != nil {
				return 0, err
			}
		}
	}
	reencodePathCounter.Add(1)
	return w.writer.writeRowGroup(w.writer.currentRowGroup, schema, sortingColumns)
}

// configureBloomFiltersForSegments sizes each column's bloom filter for the
// total number of values across all segments being packed together. Columns
// whose value count is only an upper bound (row-range views of repeated
// columns) are left unallocated so flushFilterPages sizes the filter from the
// number of values actually written.
func (w *Writer) configureBloomFiltersForSegments(segments []RowGroup) {
	for i, c := range w.writer.currentRowGroup.columns {
		if c.columnFilter == nil {
			continue
		}
		total := int64(0)
		exact := true
		for _, seg := range segments {
			chunk := seg.ColumnChunks()[i]
			exact = exact && chunkNumValuesIsExact(chunk)
			total += chunk.NumValues()
		}
		if exact {
			c.resizeBloomFilter(total)
		}
	}
}

// writeRowGroupByColumn re-encodes each source column chunk into its destination
// column writer, reading values column-wise instead of materializing rows.
func (w *Writer) writeRowGroupByColumn(columns []ColumnChunk) error {
	dst := w.writer.currentRowGroup.columns
	for i, col := range columns {
		if err := copyColumnValues(dst[i], col); err != nil {
			return err
		}
	}
	reencodePathCounter.Add(1)
	return nil
}

// reencodeValueBufferSize is the batch size for column-wise value copies. Larger
// than the default value buffer to amortize per-call overhead.
const reencodeValueBufferSize = 1024

// copyColumnValues reads every value of src in order and writes it to dst,
// without materializing rows.
func copyColumnValues(dst *ColumnWriter, src ColumnChunk) error {
	reader := NewColumnChunkValueReader(src)
	defer reader.Close()

	buf := make([]Value, reencodeValueBufferSize)
	for {
		n, err := reader.ReadValues(buf)
		if n > 0 {
			if _, werr := dst.WriteRowValues(buf[:n]); werr != nil {
				return werr
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}
