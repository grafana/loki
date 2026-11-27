package stats

import (
	"context"
	"errors"
	"fmt"
	"io"

	iter "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/memory"
)

const rowReaderBatchSize = 8192

// RowReader reads [Stat] records from a stats [Section] one row at a time, in
// section order. It is a row-level cursor over the batch-level [Reader].
// It implements [iter.CloseIterator] over [Stat].
//
// A [Stat] that At returns owns its strings and its Labels map. It stays valid
// after the next call to Next and after Close.
//
// A field whose column the section lacks, and a null element, keep the zero
// value. A null label is not in the Labels map.
//
// A RowReader is not safe for concurrent use.
type RowReader struct {
	ctx     context.Context
	reader  *Reader
	alloc   *memory.Allocator
	batch   *RecordBatch
	index   int
	decoder rowDecoder
	opened  bool

	cur       Stat  // current row, replaced by each call to Next that returns true
	err       error // captured if iteration ends with anything other than io.EOF
	exhausted bool  // set when Next has returned false; further calls return false without work
}

// NewRowReader creates a RowReader over all of sec's columns. The underlying
// reader is opened lazily on the first call to Next. The provided ctx governs
// all subsequent I/O. A RowReader of an empty section reads no rows.
//
// statsTracker is optional. The underlying reader reports its stats to it.
func NewRowReader(ctx context.Context, sec *Section, statsTracker RowReaderStatsTracker) *RowReader {
	if sec.Empty() {
		return &RowReader{ctx: ctx, exhausted: true}
	}
	return &RowReader{
		ctx:    ctx,
		reader: NewReader(ReaderOptions{Columns: sec.Columns(), StatsTracker: statsTracker}),
		alloc:  memory.NewAllocator(nil),
	}
}

var _ iter.CloseIterator[Stat] = (*RowReader)(nil)

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

// next reads the next Stat from the section. Returns io.EOF when exhausted.
func (r *RowReader) next() (Stat, error) {
	if !r.opened {
		if err := r.reader.Open(r.ctx); err != nil {
			return Stat{}, fmt.Errorf("opening reader: %w", err)
		}
		r.opened = true
	}

	if r.batch == nil || r.index >= r.batch.Rows {
		// The decoded rows own their strings, so the allocator can reuse the
		// memory of the previous batch.
		r.batch = nil
		r.alloc.Reclaim()

		batch, err := r.reader.ReadRecordBatch(r.ctx, r.alloc, rowReaderBatchSize)
		if errors.Is(err, io.EOF) {
			return Stat{}, io.EOF
		} else if err != nil {
			return Stat{}, fmt.Errorf("reading batch: %w", err)
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
func (r *RowReader) At() Stat { return r.cur }

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

// rowDecoder decodes the rows of a [RecordBatch] into [Stat] values. It copies
// every string out of the batch, so a Stat does not point into the memory of the
// batch. The zero rowDecoder is ready to use.
//
// Consecutive rows often share the sort schema and the label values, because
// the section sorts by them. The decoder keeps the last string of each and
// allocates only when the value changes.
type rowDecoder struct {
	internedObjectPath lastString
	internedSortSchema lastString
	internedLabels     []lastString // One for each label column of the batch.
}

// decode returns row i of batch. A null element, or a column that the batch does
// not hold, leaves the field at its zero value. A null label is not in the
// Labels map. When two label columns have the same name, the last non-null value
// wins.
func (d *rowDecoder) decode(batch *RecordBatch, i int) Stat {
	result := Stat{Labels: make(map[string]string, len(batch.Labels))}

	if col := batch.ObjectPath; col != nil && !col.IsNull(i) {
		result.ObjectPath = d.internedObjectPath.get(col.Get(i))
	}
	if col := batch.SectionIndex; col != nil && !col.IsNull(i) {
		result.SectionIndex = col.Get(i)
	}
	if col := batch.SortSchema; col != nil && !col.IsNull(i) {
		result.SortSchema = d.internedSortSchema.get(col.Get(i))
	}
	if col := batch.MinTimestamp; col != nil && !col.IsNull(i) {
		result.MinTimestamp = col.Get(i)
	}
	if col := batch.MaxTimestamp; col != nil && !col.IsNull(i) {
		result.MaxTimestamp = col.Get(i)
	}
	if col := batch.RowCount; col != nil && !col.IsNull(i) {
		result.RowCount = col.Get(i)
	}
	if col := batch.UncompressedSize; col != nil && !col.IsNull(i) {
		result.UncompressedSize = col.Get(i)
	}
	if col := batch.ShardBucket; col != nil && !col.IsNull(i) {
		result.ShardBucket = uint32(col.Get(i))
	}

	if len(d.internedLabels) != len(batch.Labels) {
		d.internedLabels = make([]lastString, len(batch.Labels))
	}
	for j, label := range batch.Labels {
		if !label.Values.IsNull(i) {
			result.Labels[label.Name] = d.internedLabels[j].get(label.Values.Get(i))
		}
	}
	return result
}

// lastString converts bytes to a string. It returns the previous string when
// the bytes are equal.
type lastString struct{ last string }

func (l *lastString) get(b []byte) string {
	// The compiler does not copy b to compare it. Only the assignment copies.
	if l.last != string(b) {
		l.last = string(b)
	}
	return l.last
}
