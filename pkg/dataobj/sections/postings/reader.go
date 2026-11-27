package postings

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
	"github.com/grafana/loki/v3/pkg/xcap"
)

var tracer = otel.Tracer("pkg/dataobj/sections/postings")

// RowReaderStatsTracker is the type of [ReaderOptions.StatsTracker].
type RowReaderStatsTracker = dataset.RowReaderStatsTracker

// NewOnPagesDownloadedFunc returns a stats tracker for
// [ReaderOptions.StatsTracker]. It calls fn with the compressed and the
// uncompressed size of each batch of pages that the reader downloads. It
// returns nil if fn is nil.
func NewOnPagesDownloadedFunc(fn func(compressedBytes, uncompressedBytes int64)) RowReaderStatsTracker {
	return dataset.NewOnPagesDownloadedFunc(fn)
}

// ReaderOptions customizes the behavior of a [Reader].
type ReaderOptions struct {
	// Columns to read. Each column must belong to the same [Section]. Each
	// column type can appear once. A repeat makes the first ReadRecordBatch
	// fail.
	Columns []*Column

	// Predicates filter the rows returned by the Reader. Optional; when empty
	// all rows from Columns are returned. A predicate can reference a column
	// that is not in Columns. The Reader reads that column without returning it.
	Predicates []Predicate

	// StatsTracker keeps track of the various reader internal stats.
	StatsTracker dataset.RowReaderStatsTracker
}

// validate returns an error if opts is invalid. ReaderOptions are valid when
// Columns is non-empty and every column (projected or referenced by a
// predicate) belongs to the same Section.
func (opts *ReaderOptions) validate() error {
	if len(opts.Columns) == 0 {
		return errors.New("ReaderOptions.Columns must be non-empty")
	}
	var section *Section
	checkSection := func(col *Column) error {
		if col == nil {
			return errors.New("column is nil")
		}
		if section == nil {
			section = col.Section
		} else if col.Section != section {
			return errors.New("column belongs to a different Section")
		}
		return nil
	}

	for i, col := range opts.Columns {
		if err := checkSection(col); err != nil {
			return fmt.Errorf("ReaderOptions.Columns[%d]: %w", i, err)
		}
	}

	var perr error
	for _, p := range opts.Predicates {
		walkPredicate(p, func(p Predicate) bool {
			if perr != nil {
				return false
			}
			if col, ok := predicateColumn(p); ok {
				perr = checkSection(col)
			}
			return true
		})
	}
	return perr
}

// A Reader reads batches of rows from a postings [Section].
// [Reader.ReadRecordBatch] returns each batch as a [RecordBatch] with one array
// for each entry in [ReaderOptions.Columns].
//
// A Reader is not safe for concurrent use.
type Reader struct {
	opts ReaderOptions

	ready bool
	inner *dataset.RowReader

	readSpan trace.Span
}

var errReaderNotOpen = errors.New("reader not opened")

// NewReader creates a new Reader. Options are not validated until the first
// call to [Reader.Open].
func NewReader(opts ReaderOptions) *Reader {
	var r Reader
	r.Reset(opts)
	return &r
}

// Columns returns the [Column]s the Reader will read. The caller must not
// modify the returned slice.
func (r *Reader) Columns() []*Column { return r.opts.Columns }

// Reset reuses the Reader with new options.
func (r *Reader) Reset(opts ReaderOptions) {
	r.opts = opts

	// Clone the columns to protect against the caller changing the slice.
	// ReadRecordBatch pairs each array of a batch with the column at the same
	// position, so a changed slice would mislabel the arrays.
	r.opts.Columns = slices.Clone(opts.Columns)
	r.readSpan = nil
	r.ready = false
	if r.inner != nil {
		_ = r.inner.Close()
	}
}

// Open initializes Reader resources. Must be called before
// [Reader.ReadRecordBatch]. Safe to call multiple times.
func (r *Reader) Open(ctx context.Context) error {
	if r.ready {
		return nil
	}
	if err := r.init(ctx); err != nil {
		_ = r.Close()
		return fmt.Errorf("initializing Reader: %w", err)
	}
	return nil
}

// ReadRecordBatch reads the next rows of the section that pass the predicates,
// at most batchSize of them. A nil error means that the batch has between one
// and batchSize rows. A batch with fewer rows does not mean that the section
// ended.
//
// At the end of the section, and on every call after it, ReadRecordBatch
// returns nil and [io.EOF]. On any other error it returns nil and the error.
// Treat any error other than [io.EOF] as final. It returns an error when
// batchSize is not positive, alloc is nil, or the Reader is not open.
//
// The arrays of the batch use memory from alloc. They stay valid until the
// caller reclaims alloc. A call can leave temporary memory in alloc, also when
// it fails. The caller should reclaim alloc after each batch.
//
// The call must have the allocator to itself: do not use alloc in another
// goroutine during the call.
func (r *Reader) ReadRecordBatch(ctx context.Context, alloc *memory.Allocator, batchSize int) (*RecordBatch, error) {
	if !r.ready {
		return nil, errReaderNotOpen
	}

	if r.readSpan == nil {
		ctx, r.readSpan = xcap.StartSpan(ctx, tracer, "postings.Reader.Read")
	} else {
		ctx = xcap.ContextWithSpan(ctx, r.readSpan)
	}

	rb, err := r.inner.ReadRecordBatch(ctx, alloc, batchSize)
	if errors.Is(err, io.EOF) {
		return nil, io.EOF
	} else if err != nil {
		return nil, fmt.Errorf("reading record batch: %w", err)
	}

	batch, err := newRecordBatch(r.opts.Columns, rb)
	if err != nil {
		return nil, fmt.Errorf("decoding record batch: %w", err)
	}
	return batch, nil
}

func (r *Reader) init(ctx context.Context) error {
	if err := r.opts.validate(); err != nil {
		return fmt.Errorf("invalid options: %w", err)
	}

	ctx, span := xcap.StartSpan(ctx, tracer, "postings.Reader.Open")
	defer span.End()

	// Compose the dataset from the projected columns plus any additional
	// columns referenced only by predicates.
	cols := withMissingColumns(r.opts.Columns, predicateColumns(r.opts.Predicates))
	innerSection := cols[0].Section.inner
	innerColumns := make([]*columnar.Column, len(cols))
	for i, c := range cols {
		innerColumns[i] = c.inner
	}

	dset, err := columnar.MakeDataset(innerSection, innerColumns)
	if err != nil {
		return fmt.Errorf("creating dataset: %w", err)
	} else if len(dset.Columns()) != len(cols) {
		return fmt.Errorf("dataset has %d columns, expected %d", len(dset.Columns()), len(cols))
	}

	columnLookup := make(map[*Column]dataset.Column, len(cols))
	for i, col := range dset.Columns() {
		columnLookup[cols[i]] = col
	}

	preds, err := mapPredicates(r.opts.Predicates, columnLookup)
	if err != nil {
		return fmt.Errorf("mapping predicates: %w", err)
	}

	// Project only the first len(r.opts.Columns) columns of the dataset, so a
	// batch has one array for each entry in [ReaderOptions.Columns].
	// [dataset.RowReader] reads the other columns only for the predicates.
	innerOptions := dataset.RowReaderOptions{
		Dataset:           dset,
		ProjectedColumns:  dset.Columns()[:len(r.opts.Columns)],
		Predicates:        preds,
		PrefetchAllOnOpen: true,
		StatsTracker:      r.opts.StatsTracker,
	}
	if r.inner == nil {
		r.inner = dataset.NewRowReader(innerOptions)
	} else {
		r.inner.Reset(innerOptions)
	}
	if err := r.inner.Open(ctx); err != nil {
		return fmt.Errorf("opening reader: %w", err)
	}

	r.ready = true
	return nil
}

// withMissingColumns returns the columns of base followed by the columns of src
// that base does not hold. It compares columns by pointer. It does not modify
// base.
func withMissingColumns(base, src []*Column) []*Column {
	exists := make(map[*Column]struct{}, len(base))
	for _, col := range base {
		exists[col] = struct{}{}
	}

	// A caller can pass a prefix of [Section.Columns]. The array of that slice
	// belongs to the section. An append in place would overwrite the columns
	// after the prefix.
	out := slices.Clone(base)
	for _, col := range src {
		if _, ok := exists[col]; !ok {
			out = append(out, col)
		}
	}
	return out
}

// Close closes the Reader and releases any resources it holds.
func (r *Reader) Close() error {
	if r.readSpan != nil {
		r.readSpan.End()
	}
	if r.inner != nil {
		return r.inner.Close()
	}
	return nil
}
