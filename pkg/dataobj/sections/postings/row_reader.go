package postings

import (
	"context"
	"errors"
	"fmt"
	"io"

	iter "github.com/grafana/loki/v3/pkg/iter/v2"
)

// RowReader reads [Row] records from a postings [Section] one row at a time, in
// section order. It is a row-level cursor over the batch-level [Reader].
// It implements [iter.CloseIterator] over [Row].
//
// A RowReader is not safe for concurrent use.
type RowReader struct {
	ctx    context.Context
	reader *Reader
	batch  rowBatch
	index  int // next row of batch to decode

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
// Bloom rows can hold tens of kilobytes each. Each Reader keeps the memory of
// its largest batch for reuse. An index merge keeps one Reader open per source
// index, so a larger batch multiplies the memory of the merge.
const rowReaderBatchSize = 128

// next reads the next Row from the section. Returns io.EOF when exhausted.
func (r *RowReader) next() (Row, error) {
	for r.index >= r.batch.rows {
		// Drop the arrays first, because the read reuses their memory.
		r.batch = rowBatch{}

		rb, err := r.reader.readRecordBatch(r.ctx, rowReaderBatchSize)
		if errors.Is(err, io.EOF) {
			return Row{}, io.EOF
		} else if err != nil {
			return Row{}, fmt.Errorf("reading batch: %w", err)
		}

		batch, err := newRowBatch(r.reader.Columns(), rb)
		if err != nil {
			return Row{}, fmt.Errorf("decoding batch: %w", err)
		}
		r.batch, r.index = batch, 0
	}

	row := r.batch.row(r.index)
	r.index++
	return row, nil
}

// At returns the current record. Undefined if Next has not been called or if
// the last Next call returned false.
//
// The record does not use the memory of the Reader, so it stays valid after
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
	r.batch = rowBatch{}
	if r.reader != nil {
		err := r.reader.Close()
		r.reader = nil
		return err
	}
	return nil
}
