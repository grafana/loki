package postings

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	iter "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/memory"
)

// RowReader reads [Row] records from a postings [Section] one row at a time, in
// section order. It is a row-level cursor over the batch-level [Reader].
// It implements [iter.CloseIterator] over [Row].
//
// A Row that At returns owns its strings and bytes. It stays valid after the
// next call to Next and after Close.
//
// A field of a column that the Reader did not project, and a null element, keep
// the zero value. A caller must project every field that it reads. For example,
// the zero Kind is [KindBloom].
//
// A RowReader is not safe for concurrent use.
type RowReader struct {
	ctx     context.Context
	reader  *Reader
	alloc   *memory.Allocator
	batch   *RecordBatch
	index   int
	decoder rowDecoder

	cur       Row   // current row, replaced by each call to Next that returns true
	err       error // captured if iteration ends with anything other than io.EOF
	exhausted bool  // set when Next has returned false; further calls return false without work
}

// NewRowReader creates a RowReader over an already-opened reader. The provided
// ctx governs all subsequent reads. The RowReader closes reader in
// [RowReader.Close].
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

// rowReaderBatchSize is the number of rows that a RowReader reads at a time.
//
// Bloom rows can hold tens of kilobytes each, so a batch can be large. An index
// merge keeps one batch open per source index, so a larger batch multiplies the
// memory of the merge.
const rowReaderBatchSize = 1024

// next reads the next Row from the section. Returns io.EOF when exhausted.
func (r *RowReader) next() (Row, error) {
	if r.batch == nil || r.index >= r.batch.Rows {
		r.batch = nil

		// The decoded rows own their strings and bytes, so the allocator can reuse
		// the memory of the previous batch.
		r.alloc.Reclaim()

		batch, err := r.reader.ReadRecordBatch(r.ctx, r.alloc, rowReaderBatchSize)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return Row{}, io.EOF
			}
			return Row{}, fmt.Errorf("reading batch: %w", err)
		}
		r.batch = batch
		r.index = 0
	}

	row := r.decoder.decode(r.batch, r.index)
	r.index++
	return row, nil
}

// At returns the current record. Undefined if Next has not been called or if
// the last Next call returned false.
func (r *RowReader) At() Row { return r.cur }

// Err returns any error that caused iteration to end. nil on natural EOF.
func (r *RowReader) Err() error { return r.err }

// Close releases the memory of the RowReader and closes the underlying reader.
// Repeat calls return nil without closing again.
func (r *RowReader) Close() error {
	// Mark exhausted so a stray Next() after Close() returns false instead of
	// dereferencing the now-nil reader.
	r.exhausted = true
	r.batch = nil
	if r.alloc != nil {
		r.alloc.Free()
	}
	if r.reader != nil {
		err := r.reader.Close()
		r.reader = nil
		return err
	}
	return nil
}

// rowDecoder decodes the rows of a [RecordBatch] into [Row] values. It copies
// every string and byte slice out of the batch, so a Row does not point into the
// memory of the batch.
//
// Consecutive rows often share the column name and the label value. The decoder
// keeps the last string of each and allocates only when the value changes. The
// zero rowDecoder is ready to use.
type rowDecoder struct {
	// internedObjectPaths holds one string for each object path that the
	// decoder has seen. The object path often differs between consecutive
	// rows, so the previous string alone saves few allocations.
	internedObjectPaths map[string]string
	internedColumnName  lastString
	internedLabelValue  lastString
}

// decode returns row i of batch. A null element, or a column that the batch does
// not hold, decodes to the zero value.
func (d *rowDecoder) decode(batch *RecordBatch, i int) Row {
	var row Row

	if col := batch.Kind; col != nil && !col.IsNull(i) {
		row.Kind = PostingKind(col.Get(i))
	}
	if col := batch.ObjectPath; col != nil && !col.IsNull(i) {
		row.ObjectPath = d.objectPath(col.Get(i))
	}
	if col := batch.ShardBuckets; col != nil && !col.IsNull(i) {
		row.ShardBuckets = col.Get(i)
	}
	if col := batch.SectionIndex; col != nil && !col.IsNull(i) {
		row.SectionIndex = col.Get(i)
	}
	if col := batch.ColumnName; col != nil && !col.IsNull(i) {
		row.ColumnName = d.internedColumnName.get(col.Get(i))
	}
	if col := batch.LabelValue; col != nil && !col.IsNull(i) {
		row.LabelValue = d.internedLabelValue.get(col.Get(i))
	}
	if col := batch.StreamIDBitmap; col != nil && !col.IsNull(i) {
		row.StreamIDBitmap = bytes.Clone(col.Get(i))
	}
	if col := batch.BloomFilter; col != nil && !col.IsNull(i) {
		row.BloomFilter = bytes.Clone(col.Get(i))
	}
	if col := batch.UncompressedSize; col != nil && !col.IsNull(i) {
		row.UncompressedSize = col.Get(i)
	}
	if col := batch.MinTimestamp; col != nil && !col.IsNull(i) {
		row.MinTimestamp = col.Get(i)
	}
	if col := batch.MaxTimestamp; col != nil && !col.IsNull(i) {
		row.MaxTimestamp = col.Get(i)
	}
	if col := batch.MinShardBucket; col != nil && !col.IsNull(i) {
		row.MinShardBucket = uint32(col.Get(i))
	}
	if col := batch.MaxShardBucket; col != nil && !col.IsNull(i) {
		row.MaxShardBucket = uint32(col.Get(i))
	}
	return row
}

// objectPath returns b as a string. It copies b only the first time that it sees
// the path.
func (d *rowDecoder) objectPath(b []byte) string {
	if path, ok := d.internedObjectPaths[string(b)]; ok {
		return path
	}
	if d.internedObjectPaths == nil {
		d.internedObjectPaths = make(map[string]string)
	}
	path := string(b)
	d.internedObjectPaths[path] = path
	return path
}

// lastString converts bytes to a string. It returns the previous string when the
// bytes are equal.
type lastString struct{ last string }

func (l *lastString) get(b []byte) string {
	// The compiler does not copy b to compare it. Only the assignment copies.
	if l.last != string(b) {
		l.last = string(b)
	}
	return l.last
}
