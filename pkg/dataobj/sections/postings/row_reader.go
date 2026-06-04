package postings

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	iter "github.com/grafana/loki/v3/pkg/iter/v2"
)

// RowReader reads [Row] records from a postings [Section] one row at a time, in
// section order. It is a thin row-level cursor over the batch-level [Reader].
// It implements [iter.CloseIterator] over [Row].
//
// A RowReader is not safe for concurrent use.
type RowReader struct {
	ctx     context.Context
	reader  *Reader
	batch   arrow.RecordBatch
	index   int
	columns ColumnIndex
	opened  bool

	cur       Row   // current value, valid between Next() returning true and the next Next() call
	err       error // captured if iteration ends with anything other than io.EOF
	exhausted bool  // set when Next has returned false; further calls return false without work
}

// NewRowReader creates a RowReader over all of sec's columns. The underlying
// reader is opened lazily on the first call to Next. The provided ctx governs
// all subsequent I/O (Open and Read).
func NewRowReader(ctx context.Context, sec *Section) *RowReader {
	return &RowReader{
		ctx: ctx,
		reader: NewReader(ReaderOptions{
			Columns:   sec.Columns(),
			Allocator: memory.DefaultAllocator,
		}),
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

// next reads the next Row from the section. Returns io.EOF when exhausted.
func (r *RowReader) next() (Row, error) {
	if !r.opened {
		if err := r.reader.Open(r.ctx); err != nil {
			return Row{}, fmt.Errorf("opening reader: %w", err)
		}
		r.opened = true
	}

	if r.batch == nil || r.index >= int(r.batch.NumRows()) {
		r.batch = nil

		batch, err := r.reader.Read(r.ctx, 8192)
		if errors.Is(err, io.EOF) && batch == nil {
			return Row{}, io.EOF
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return Row{}, fmt.Errorf("reading batch: %w", err)
		}

		if batch != nil && batch.NumRows() > 0 {
			r.batch = batch
			r.index = 0
			if r.columns == nil {
				r.columns = BuildColumnIndex(batch.Schema())
			}
		} else {
			// Empty or nil batch: treat as end of section.
			return Row{}, io.EOF
		}
	}

	row := DecodeRow(r.batch, r.columns, r.index)
	r.index++
	return row, nil
}

// At returns the current record. Undefined if Next has not been called or if
// the last Next call returned false.
func (r *RowReader) At() Row { return r.cur }

// Err returns any error that caused iteration to end. nil on natural EOF.
func (r *RowReader) Err() error { return r.err }

// Close releases the underlying reader. Idempotent: repeat calls return nil
// without re-closing. Marks the reader exhausted so a stray Next() after
// Close() returns false instead of dereferencing the now-nil reader.
func (r *RowReader) Close() error {
	r.exhausted = true
	r.batch = nil
	if r.reader != nil {
		err := r.reader.Close()
		r.reader = nil
		return err
	}
	return nil
}
