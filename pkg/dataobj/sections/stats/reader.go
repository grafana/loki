package stats

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/memory"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// Section represents an opened stats section.
type Section struct {
	inner   *columnar.Section
	columns []*Column
}

// Open opens a [Section] from an underlying [dataobj.Section]. Open returns an
// error if the section metadata could not be read or if the provided ctx is
// canceled.
func Open(ctx context.Context, section *dataobj.Section) (*Section, error) {
	if !CheckSection(section) {
		return nil, fmt.Errorf("section type mismatch: got=%s want=%s", section.Type, sectionType)
	} else if !columnar.IsSupportedVersion(section.Type.Version) {
		return nil, fmt.Errorf("unsupported section version: got=%d", section.Type.Version)
	}

	dec, err := columnar.NewDecoder(section.Reader, section.Type.Version)
	if err != nil {
		return nil, fmt.Errorf("creating decoder: %w", err)
	}

	columnarSection, err := columnar.Open(ctx, section.Tenant, dec)
	if err != nil {
		return nil, fmt.Errorf("opening columnar section: %w", err)
	}

	sec := &Section{inner: columnarSection}
	if err := sec.init(); err != nil {
		return nil, fmt.Errorf("initializing section: %w", err)
	}
	return sec, nil
}

func (s *Section) init() error {
	for _, col := range s.inner.Columns() {
		colType, err := ParseColumnType(col.Type.Logical)
		if err != nil {
			// Skip over unrecognized columns; probably come from a newer
			// version of the code.
			continue
		}

		s.columns = append(s.columns, &Column{
			Section: s,
			Name:    col.Tag,
			Type:    colType,

			inner: col,
		})
	}

	return nil
}

// Layout returns the stream layout that the section records. ok is false if
// the section records none, as sections written before stats sections
// recorded a layout do.
//
// Layout returns an error if the recorded layout is not one that
// [streams.NewLayout] can produce.
func (s *Section) Layout() (layout streams.Layout, ok bool, err error) {
	return streams.LayoutFromSortInfo(s.inner.SortInfo())
}

// Columns returns the set of Columns in the section. The slice of returned
// columns must not be mutated.
//
// Unrecognized columns (e.g., when running older code against newer stats
// sections) are skipped.
func (s *Section) Columns() []*Column { return s.columns }

// Empty reports whether the section has no physical columns, as a section
// from a [LayoutSectionBuilder] has. An empty section has no rows. A section
// whose columns are all of types that this code does not know is not empty.
func (s *Section) Empty() bool { return len(s.inner.Columns()) == 0 }

// ObjectPathColumn returns the object_path column of the section. ok is false
// for an empty section, which has no rows.
//
// It returns an error if a section that is not empty has no object_path column
// or more than one. A reader that needs every object path must not skip such a
// section, because it could miss object paths.
func (s *Section) ObjectPathColumn() (column *Column, ok bool, err error) {
	if s.Empty() {
		return nil, false, nil
	}
	for _, c := range s.columns {
		if c.Type != ColumnTypeObjectPath {
			continue
		}
		if column != nil {
			return nil, false, errors.New("stats section has more than one object_path column")
		}
		column = c
	}
	if column == nil {
		return nil, false, errors.New("stats section has no object_path column")
	}
	return column, true, nil
}

// A Column represents one of the columns in the stats section. Valid columns
// can only be retrieved by calling [Section.Columns].
//
// Data in columns can be read by using a [Reader].
type Column struct {
	Section *Section   // Section that contains the column.
	Name    string     // Optional name of the column (label name for dynamic columns).
	Type    ColumnType // Type of data in the column.

	inner *columnar.Column
}

var tracer = otel.Tracer("pkg/dataobj/sections/stats")

// RowReaderStatsTracker is the type of [ReaderOptions.StatsTracker] and of the
// tracker argument of [NewRowReader].
type RowReaderStatsTracker = dataset.RowReaderStatsTracker

// ReaderOptions customizes the behavior of a [Reader].
type ReaderOptions struct {
	// Columns to read. Each column must belong to the same [Section]. Each
	// column type can appear once, except the label columns, which can repeat.
	// A repeat of another type makes the first ReadRecordBatch fail.
	Columns []*Column

	// StatsTracker keeps track of the various reader internal stats.
	StatsTracker dataset.RowReaderStatsTracker
}

// validate returns an error if opts is invalid. ReaderOptions are valid when
// Columns is non-empty and every column belongs to the same Section.
//
// Package-private — not exposed publicly; called from init() to keep the
// public surface minimal per spec.
func (opts *ReaderOptions) validate() error {
	if len(opts.Columns) == 0 {
		return errors.New("ReaderOptions.Columns must be non-empty")
	}
	var section *Section
	for i, col := range opts.Columns {
		if col == nil {
			return fmt.Errorf("ReaderOptions.Columns[%d] is nil", i)
		}
		if section == nil {
			section = col.Section
		} else if col.Section != section {
			return fmt.Errorf("ReaderOptions.Columns[%d] belongs to a different Section", i)
		}
	}
	return nil
}

// A Reader reads batches of rows from a stats [Section].
// [Reader.ReadRecordBatch] returns each batch as a [RecordBatch] with one array
// for each entry in [ReaderOptions.Columns].
//
// A Reader is not safe for concurrent use.
type Reader struct {
	opts     ReaderOptions
	ready    bool
	inner    *dataset.RowReader
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

// Columns returns the [Column]s the Reader will read. The caller must not modify
// the returned slice.
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

// ReadRecordBatch reads the next rows of the section, at most batchSize of them.
// A nil error means that the batch has between one and batchSize rows. A batch
// with fewer rows does not mean that the section ended.
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
		ctx, r.readSpan = xcap.StartSpan(ctx, tracer, "stats.Reader.Read")
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

	ctx, span := xcap.StartSpan(ctx, tracer, "stats.Reader.Open")
	defer span.End()

	cols := r.opts.Columns
	innerSection := cols[0].Section.inner
	innerColumns := make([]*columnar.Column, len(cols))
	for i, c := range cols {
		innerColumns[i] = c.inner
	}

	dset, err := columnar.MakeDataset(innerSection, innerColumns)
	if err != nil {
		return fmt.Errorf("creating dataset: %w", err)
	}

	innerOptions := dataset.RowReaderOptions{
		Dataset:           dset,
		ProjectedColumns:  dset.Columns(),
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
