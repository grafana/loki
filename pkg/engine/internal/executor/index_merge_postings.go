package executor

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// comparePostingsRow compares two postings rows using the sort order:
// (Kind, ObjectPath, SectionIndex, ColumnName, LabelValue).
func comparePostingsRow(a, b postings.Row) int {
	if a.Kind != b.Kind {
		if a.Kind < b.Kind {
			return -1
		}
		return 1
	}
	if a.ObjectPath != b.ObjectPath {
		if a.ObjectPath < b.ObjectPath {
			return -1
		}
		return 1
	}
	if a.SectionIndex != b.SectionIndex {
		if a.SectionIndex < b.SectionIndex {
			return -1
		}
		return 1
	}
	if a.ColumnName != b.ColumnName {
		if a.ColumnName < b.ColumnName {
			return -1
		}
		return 1
	}
	if a.LabelValue != b.LabelValue {
		if a.LabelValue < b.LabelValue {
			return -1
		}
		return 1
	}
	return 0
}

// postingsPileReader reads postings.Row records from a postings section in order.
type postingsPileReader struct {
	ctx     context.Context
	pileIdx int
	reader  *postings.Reader
	batch   arrow.RecordBatch
	index   int
	columns postings.ColumnIndex
	opened  bool

	cur       postings.Row // current value, valid between Next() returning true and the next Next() call
	err       error        // captured if iteration ends with anything other than io.EOF
	exhausted bool         // set when Next has returned false; further calls return false without work
}

// newPostingsPileReader creates a new postingsPileReader from a postings section.
func newPostingsPileReader(ctx context.Context, sec *postings.Section, pileIdx int) *postingsPileReader {
	reader := postings.NewReader(postings.ReaderOptions{
		Columns:   sec.Columns(),
		Allocator: memory.DefaultAllocator,
	})
	return &postingsPileReader{
		ctx:     ctx,
		pileIdx: pileIdx,
		reader:  reader,
	}
}

// Next advances the cursor. Returns false on exhaustion (natural EOF or any error).
// Subsequent calls to Next continue to return false.
func (r *postingsPileReader) Next() bool {
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

// next reads the next postings.Row from the section. Returns io.EOF when exhausted.
// Uses r.ctx instead of accepting a context parameter.
func (r *postingsPileReader) next() (postings.Row, error) {
	// Open the reader on first access
	if !r.opened {
		if err := r.reader.Open(r.ctx); err != nil {
			return postings.Row{}, fmt.Errorf("opening reader: %w", err)
		}
		r.opened = true
	}

	// If we don't have a batch or we've consumed all rows in the current batch,
	// read the next batch.
	if r.batch == nil || r.index >= int(r.batch.NumRows()) {
		if r.batch != nil {
			r.batch.Release()
			r.batch = nil
		}

		// Read next batch
		batch, err := r.reader.Read(r.ctx, 8192)
		if errors.Is(err, io.EOF) && batch == nil {
			return postings.Row{}, io.EOF
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return postings.Row{}, fmt.Errorf("reading batch: %w", err)
		}

		// If we got a batch with rows, use it
		if batch != nil && batch.NumRows() > 0 {
			r.batch = batch
			r.index = 0
			// Build column index on first batch
			if r.columns == nil {
				r.columns = postings.BuildColumnIndex(batch.Schema())
			}
		} else if batch != nil {
			batch.Release()
			return postings.Row{}, io.EOF
		} else if errors.Is(err, io.EOF) {
			return postings.Row{}, io.EOF
		}
	}

	// Decode the row at r.index from r.batch
	row := postings.DecodeRow(r.batch, r.columns, r.index)
	r.index++
	return row, nil
}

// Value returns the current record. Undefined if Next has not been called
// or if the last Next call returned false.
func (r *postingsPileReader) Value() postings.Row {
	return r.cur
}

// Err returns any error that caused iteration to end. nil on natural EOF.
func (r *postingsPileReader) Err() error {
	return r.err
}

// PileIdx returns the pile's index in the merge.
func (r *postingsPileReader) PileIdx() int {
	return r.pileIdx
}

// Verify that postingsPileReader implements pileSequence[postings.Row].
var _ pileSequence[postings.Row] = (*postingsPileReader)(nil)

// Close closes the reader and releases resources.
func (r *postingsPileReader) Close() error {
	if r.batch != nil {
		r.batch.Release()
		r.batch = nil
	}
	if r.reader != nil {
		return r.reader.Close()
	}
	return nil
}
