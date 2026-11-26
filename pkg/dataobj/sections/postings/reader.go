package postings

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/arrowconv"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
	memoryv2 "github.com/grafana/loki/v3/pkg/memory"
	"github.com/grafana/loki/v3/pkg/xcap"
)

var tracer = otel.Tracer("pkg/dataobj/sections/postings")

// ReaderOptions customizes the behavior of a [Reader].
type ReaderOptions struct {
	// Columns to read. Each column must belong to the same [Section].
	Columns []*Column

	// Predicates filter the rows returned by the Reader. Optional; when empty
	// all rows from Columns are returned.
	Predicates []Predicate

	// Allocator to use for allocating Arrow records. If nil,
	// [memory.DefaultAllocator] is used.
	Allocator memory.Allocator

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

// A Reader reads batches of rows from a postings [Section]. The returned
// [arrow.RecordBatch] values carry one column per entry in
// [ReaderOptions.Columns], named per [Reader.Schema].
type Reader struct {
	opts   ReaderOptions
	schema *arrow.Schema

	ready bool
	inner *dataset.RowReader

	alloc *memoryv2.Allocator

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

// Columns returns the [Column]s the Reader will read.
func (r *Reader) Columns() []*Column { return r.opts.Columns }

// Schema returns the [arrow.Schema] used by the Reader. Set on construction
// (via [Reader.Reset]) so it is valid before [Reader.Open] is called.
func (r *Reader) Schema() *arrow.Schema { return r.schema }

// Reset reuses the Reader with new options. Reset rebuilds Schema so that
// Schema() is valid before Open.
func (r *Reader) Reset(opts ReaderOptions) {
	if r.alloc == nil {
		r.alloc = memoryv2.NewAllocator(nil)
	} else {
		r.alloc.Reset()
	}
	r.opts = opts
	r.schema = columnsSchema(opts.Columns)
	r.readSpan = nil
	r.ready = false
	if r.inner != nil {
		_ = r.inner.Close()
	}
}

// Open initializes Reader resources. Must be called before [Reader.Read].
// Safe to call multiple times.
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

// Read reads up to batchSize rows from the section. Read returns an error if
// batchSize is not positive.
//
// Read skips the rows that fail the predicates. A batch with a nil error has at
// least one row. A batch with fewer than batchSize rows does not mean that the
// section ended. At the end of the section, Read returns an empty batch and
// io.EOF.
//
// A batch that comes with an error has no rows. Read never returns a nil batch
// with io.EOF. It can return a nil batch with another error. The caller must
// release every returned batch.
func (r *Reader) Read(ctx context.Context, batchSize int) (arrow.RecordBatch, error) {
	if !r.ready {
		return nil, errReaderNotOpen
	}

	if r.readSpan == nil {
		ctx, r.readSpan = xcap.StartSpan(ctx, tracer, "postings.Reader.Read")
	} else {
		ctx = xcap.ContextWithSpan(ctx, r.readSpan)
	}

	defer r.alloc.Reclaim()

	rb, readErr := r.inner.ReadRecordBatch(ctx, r.alloc, batchSize)
	result, err := arrowconv.ToRecordBatch(rb, r.schema)
	if err != nil {
		return nil, fmt.Errorf("convert columnar.RecordBatch to arrow.RecordBatch: %w", err)
	}

	return result, readErr
}

func (r *Reader) init(ctx context.Context) error {
	if err := r.opts.validate(); err != nil {
		return fmt.Errorf("invalid options: %w", err)
	}
	if r.opts.Allocator == nil {
		r.opts.Allocator = memory.DefaultAllocator
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
	// batch has one array for each field of the schema. [dataset.RowReader] reads
	// the other columns only for the predicates.
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

// columnsSchema builds the arrow schema for the given projected columns.
func columnsSchema(cols []*Column) *arrow.Schema {
	fields := make([]arrow.Field, 0, len(cols))
	for _, col := range cols {
		fields = append(fields, columnToField(col))
	}
	return arrow.NewSchema(fields, nil)
}

var columnDatatypes = map[ColumnType]arrow.DataType{
	ColumnTypeInvalid:          arrow.Null,
	ColumnTypeKind:             arrow.PrimitiveTypes.Int64,
	ColumnTypeObjectPath:       arrow.BinaryTypes.String,
	ColumnTypeShardBuckets:     arrow.PrimitiveTypes.Int64,
	ColumnTypeSectionIndex:     arrow.PrimitiveTypes.Int64,
	ColumnTypeColumnName:       arrow.BinaryTypes.String,
	ColumnTypeLabelValue:       arrow.BinaryTypes.String,
	ColumnTypeBloomFilter:      arrow.BinaryTypes.Binary,
	ColumnTypeStreamIDBitmap:   arrow.BinaryTypes.Binary,
	ColumnTypeUncompressedSize: arrow.PrimitiveTypes.Int64,
	ColumnTypeMinTimestamp:     arrow.FixedWidthTypes.Timestamp_ns,
	ColumnTypeMaxTimestamp:     arrow.FixedWidthTypes.Timestamp_ns,
	ColumnTypeMinShardBucket:   arrow.PrimitiveTypes.Int64,
	ColumnTypeMaxShardBucket:   arrow.PrimitiveTypes.Int64,
}

func columnToField(col *Column) arrow.Field {
	dtype, ok := columnDatatypes[col.Type]
	if !ok {
		dtype = arrow.Null
	}
	return arrow.Field{
		Name:     makeColumnName(col.Name, col.Type.String(), dtype),
		Type:     dtype,
		Nullable: true,
	}
}

// makeColumnName produces a unique field name "<name>.<type>.<dtype>" or
// "<type>.<dtype>" when the column has no name. Mirrors streams/reader.go.
func makeColumnName(label, name string, dty arrow.DataType) string {
	switch {
	case label == "" && name == "":
		return dty.Name()
	case label == "" && name != "":
		return name + "." + dty.Name()
	default:
		if name == "" {
			name = "<invalid>"
		}
		return label + "." + name + "." + dty.Name()
	}
}
