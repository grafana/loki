package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
)

// compareStatsRow returns the lexicographic order of two stats.Stat records
// under the merge sort, which is:
//
//	(Labels in SortSchema order, MinTimestamp, MaxTimestamp, ObjectPath, SectionIndex)
//
// Assumes all input rows share the same SortSchema; this is validated
// upstream in classifyRuns.
func compareStatsRow(a, b stats.Stat) int {
	// The first three components (labels, minT, maxT) match the stats builder sort
	// (pkg/dataobj/sections/stats/builder.go:compareStats)
	//
	// Compare label values in the order defined by SortSchema.
	for labelName := range strings.SplitSeq(a.SortSchema, ",") {
		va := a.Labels[labelName]
		vb := b.Labels[labelName]
		if va != vb {
			if va < vb {
				return -1
			}
			return 1
		}
	}

	// Compare MinTimestamp.
	if a.MinTimestamp != b.MinTimestamp {
		if a.MinTimestamp < b.MinTimestamp {
			return -1
		}
		return 1
	}

	// Compare MaxTimestamp.
	if a.MaxTimestamp != b.MaxTimestamp {
		if a.MaxTimestamp < b.MaxTimestamp {
			return -1
		}
		return 1
	}

	// ObjectPath and SectionIndex are appended as final tiebreakers so that
	// distinct source sections can never compare equal.
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

	return 0
}

// statsPileReader reads stats.Stat records from a stats section in order.
type statsPileReader struct {
	ctx     context.Context
	pileIdx int
	reader  *stats.Reader
	batch   arrow.RecordBatch
	index   int
	columns stats.ColumnIndex
	opened  bool

	cur       stats.Stat // current value, valid between Next() returning true and the next Next() call
	err       error      // captured if iteration ends with anything other than io.EOF
	exhausted bool       // set when Next has returned false; further calls return false without work
}

// newStatsPileReader creates a new statsPileReader from a stats section.
func newStatsPileReader(ctx context.Context, sec *stats.Section, pileIdx int) *statsPileReader {
	reader := stats.NewReader(stats.ReaderOptions{
		Columns:   sec.Columns(),
		Allocator: memory.DefaultAllocator,
	})
	return &statsPileReader{
		ctx:     ctx,
		pileIdx: pileIdx,
		reader:  reader,
	}
}

// Next advances the cursor. Returns false on exhaustion (natural EOF or any error).
// Subsequent calls to Next continue to return false.
func (r *statsPileReader) Next() bool {
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

// next reads the next stats.Stat from the section. Returns io.EOF when exhausted.
// Uses r.ctx instead of accepting a context parameter.
func (r *statsPileReader) next() (stats.Stat, error) {
	// Open the reader on first access
	if !r.opened {
		if err := r.reader.Open(r.ctx); err != nil {
			return stats.Stat{}, fmt.Errorf("opening reader: %w", err)
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
			return stats.Stat{}, io.EOF
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return stats.Stat{}, fmt.Errorf("reading batch: %w", err)
		}

		// If we got a batch with rows, use it
		if batch != nil && batch.NumRows() > 0 {
			r.batch = batch
			r.index = 0
			// Build column index on first batch
			if r.columns == nil {
				r.columns = stats.BuildColumnIndex(batch.Schema())
			}
		} else if batch != nil {
			batch.Release()
			return stats.Stat{}, io.EOF
		} else if errors.Is(err, io.EOF) {
			return stats.Stat{}, io.EOF
		}
	}

	// Decode the row at r.index from r.batch
	row := stats.DecodeRow(r.batch, r.columns, r.index)
	r.index++
	return row, nil
}

// Value returns the current record. Undefined if Next has not been called
// or if the last Next call returned false.
func (r *statsPileReader) Value() stats.Stat {
	return r.cur
}

// Err returns any error that caused iteration to end. nil on natural EOF.
func (r *statsPileReader) Err() error {
	return r.err
}

// PileIdx returns the pile's index in the merge.
func (r *statsPileReader) PileIdx() int {
	return r.pileIdx
}

// Verify that statsPileReader implements pileSequence[stats.Stat].
var _ pileSequence[stats.Stat] = (*statsPileReader)(nil)

// Close closes the reader and releases resources.
func (r *statsPileReader) Close() error {
	if r.batch != nil {
		r.batch.Release()
		r.batch = nil
	}
	if r.reader != nil {
		return r.reader.Close()
	}
	return nil
}
