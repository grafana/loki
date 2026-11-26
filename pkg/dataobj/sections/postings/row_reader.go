package postings

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"unsafe"

	"github.com/grafana/loki/v3/pkg/columnar"
	iter "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/memory"
)

// RowReader reads [Row] records from a postings [Section] one row at a time, in
// section order. It is a row-level cursor over the batch-level [Reader].
// It implements [iter.CloseIterator] over [Row].
//
// A RowReader is not safe for concurrent use.
type RowReader struct {
	ctx    context.Context
	reader *Reader

	// alloc holds the memory of batch. next reclaims it before each read.
	alloc  *memory.Allocator
	batch  *columnar.RecordBatch
	index  int // next row of batch to decode
	values rowValues

	cur       Row   // row that the last successful Next decoded
	err       error // captured if iteration ends with anything other than io.EOF
	exhausted bool  // set when Next has returned false; further calls return false without work
}

// NewRowReader creates a RowReader over an already-opened reader. The provided
// ctx governs all subsequent reads.
func NewRowReader(ctx context.Context, reader *Reader) *RowReader {
	return &RowReader{
		ctx:    ctx,
		reader: reader,
		alloc:  memory.NewAllocator(nil),
	}
}

var _ iter.CloseIterator[Row] = (*RowReader)(nil)

// Next advances the cursor. Returns false on exhaustion (natural EOF or any
// error). Subsequent calls continue to return false.
func (r *RowReader) Next() bool {
	if r.exhausted {
		return false
	}
	rec, err := r.next()
	if errors.Is(err, io.EOF) {
		r.exhausted = true
		return false
	}
	if err != nil {
		r.err = err
		r.exhausted = true
		return false
	}
	r.cur = rec
	return true
}

// rowReaderBatchSize is the maximum number of rows that a RowReader reads at a
// time.
//
// Bloom rows can hold tens of kilobytes each. Each RowReader keeps the memory
// of its largest batch for reuse. An index merge keeps one RowReader open per
// source index, so a larger batch multiplies the memory of the merge.
const rowReaderBatchSize = 128

// next reads the next Row from the section. Returns io.EOF when exhausted.
func (r *RowReader) next() (Row, error) {
	for r.batch == nil || r.index >= int(r.batch.NumRows()) {
		// Drop the batch first, because the read reuses its memory.
		r.batch = nil
		r.alloc.Reclaim()

		rb, err := r.reader.readRecordBatch(r.ctx, r.alloc, rowReaderBatchSize)
		if errors.Is(err, io.EOF) {
			return Row{}, io.EOF
		} else if err != nil {
			return Row{}, fmt.Errorf("reading batch: %w", err)
		}
		if err := checkRecordBatch(r.reader.Columns(), rb); err != nil {
			return Row{}, fmt.Errorf("decoding batch: %w", err)
		}
		r.batch, r.index = rb, 0
		r.values.reset(r.reader.Columns(), rb)
	}

	row := decodeRow(r.reader.Columns(), r.batch, r.index, &r.values)
	r.index++
	return row, nil
}

// At returns the current record. Undefined if Next has not been called or if
// the last Next call returned false.
//
// The record does not use the memory of the batch, so it stays valid after
// later calls to Next. A retained record keeps the strings and stream ID
// bitmaps of its batch alive.
func (r *RowReader) At() Row { return r.cur }

// Err returns any error that caused iteration to end. nil on natural EOF.
func (r *RowReader) Err() error { return r.err }

// Close releases the underlying reader. Repeat calls return nil without
// re-closing.
func (r *RowReader) Close() error {
	// Mark exhausted so a stray Next() after Close() returns false instead of
	// dereferencing the now-nil reader.
	r.exhausted = true
	r.batch = nil
	if r.reader != nil {
		err := r.reader.Close()
		r.reader = nil
		return err
	}
	return nil
}

// checkRecordBatch returns an error if rb does not hold, for each column, an
// array of rb.NumRows elements of the type that [decodeRow] reads. It ignores a
// column of a type that it does not know, because [Row] has no field for it.
//
// A check once per batch lets decodeRow read the arrays without a type check
// for each row.
func checkRecordBatch(columns []*Column, rb *columnar.RecordBatch) error {
	if int(rb.NumCols()) < len(columns) {
		return fmt.Errorf("record batch has %d columns, want at least %d", rb.NumCols(), len(columns))
	}

	rows := int(rb.NumRows())
	for i, col := range columns {
		arr := rb.Column(int64(i))
		if arr.Len() != rows {
			return fmt.Errorf("array of %s has %d elements, want %d", col.Type, arr.Len(), rows)
		}

		ok := true
		switch col.Type {
		case ColumnTypeObjectPath, ColumnTypeColumnName, ColumnTypeLabelValue, ColumnTypeStreamIDBitmap, ColumnTypeBloomFilter:
			_, ok = arr.(*columnar.UTF8)
		case ColumnTypeKind, ColumnTypeShardBuckets, ColumnTypeSectionIndex, ColumnTypeUncompressedSize,
			ColumnTypeMinTimestamp, ColumnTypeMaxTimestamp, ColumnTypeMinShardBucket, ColumnTypeMaxShardBucket:
			_, ok = arr.(*columnar.Number[int64])
		}
		if !ok {
			return fmt.Errorf("invalid array type %T for %s", arr, col.Type)
		}
	}
	return nil
}

// decodeRow decodes row i of rb, whose first arrays hold columns. A null value
// or a column that rb does not hold decodes as the zero value. rb must pass
// [checkRecordBatch], and i must be in the range [0, rb.NumRows()).
//
// decodeRow copies strings and stream ID bitmaps into values, so the Row stays
// valid after the memory of rb is reused.
func decodeRow(columns []*Column, rb *columnar.RecordBatch, i int, values *rowValues) Row {
	var row Row
	for c, col := range columns {
		arr := rb.Column(int64(c))
		if arr.IsNull(i) {
			continue
		}

		switch col.Type {
		case ColumnTypeKind:
			row.Kind = PostingKind(int64At(arr, i))
		case ColumnTypeObjectPath:
			row.ObjectPath = values.string(binaryAt(arr, i))
		case ColumnTypeShardBuckets:
			row.ShardBuckets = int64At(arr, i)
		case ColumnTypeSectionIndex:
			row.SectionIndex = int64At(arr, i)
		case ColumnTypeColumnName:
			row.ColumnName = values.string(binaryAt(arr, i))
		case ColumnTypeLabelValue:
			row.LabelValue = values.string(binaryAt(arr, i))
		case ColumnTypeStreamIDBitmap:
			row.StreamIDBitmap = values.bytes(binaryAt(arr, i))
		case ColumnTypeBloomFilter:
			// Copy each bloom filter on its own. They can be tens of kilobytes
			// each, and a value in rowValues keeps every value of its batch alive.
			row.BloomFilter = bytes.Clone(binaryAt(arr, i))
		case ColumnTypeUncompressedSize:
			row.UncompressedSize = int64At(arr, i)
		case ColumnTypeMinTimestamp:
			row.MinTimestamp = int64At(arr, i)
		case ColumnTypeMaxTimestamp:
			row.MaxTimestamp = int64At(arr, i)
		case ColumnTypeMinShardBucket:
			row.MinShardBucket = uint32(int64At(arr, i))
		case ColumnTypeMaxShardBucket:
			row.MaxShardBucket = uint32(int64At(arr, i))
		}
	}
	return row
}

// int64At returns value i of arr. arr must be a [columnar.Number] of int64,
// as [checkRecordBatch] ensures.
func int64At(arr columnar.Array, i int) int64 {
	return arr.(*columnar.Number[int64]).Get(i)
}

// binaryAt returns value i of arr. arr must be a [columnar.UTF8], as
// [checkRecordBatch] ensures. The value uses the memory of arr.
func binaryAt(arr columnar.Array, i int) []byte {
	return arr.(*columnar.UTF8).Get(i)
}

// rowValues copies strings and byte slices out of the memory of a batch, which
// the next read reuses. It packs the values of one batch into one buffer, so a
// row takes no allocation of its own.
//
// rowValues never writes bytes that it returned. So a value stays valid after
// later copies and after reset. A retained value keeps the buffer of its batch
// alive.
type rowValues struct {
	buf []byte
}

// reset starts a new buffer with room for the string and stream ID bitmap
// values of rb. It does not reuse the previous buffer, because rows can still
// hold values in it.
func (v *rowValues) reset(columns []*Column, rb *columnar.RecordBatch) {
	var size int
	for i, col := range columns {
		if col.Type == ColumnTypeBloomFilter {
			continue
		}
		if arr, ok := rb.Column(int64(i)).(*columnar.UTF8); ok {
			size += arr.DataLen()
		}
	}
	v.buf = make([]byte, 0, size)
}

// string returns a copy of b as a string.
func (v *rowValues) string(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	value := v.copy(b)
	return unsafe.String(unsafe.SliceData(value), len(value))
}

// bytes returns a copy of b. The capacity of the copy ends at its length, so
// an append by the caller cannot overwrite the next value.
func (v *rowValues) bytes(b []byte) []byte {
	return v.copy(b)
}

func (v *rowValues) copy(b []byte) []byte {
	// If append grows the buffer, the values returned before stay in the old
	// buffer. They stay valid, because nothing writes to it again.
	start := len(v.buf)
	v.buf = append(v.buf, b...)
	return v.buf[start:len(v.buf):len(v.buf)]
}
