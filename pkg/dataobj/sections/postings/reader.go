package postings

import (
	"context"
	"errors"
	"fmt"

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
	inner *columnar.ReaderAdapter

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

// Reset reuses the Reader with new options. Schema is rebuilt here, matching
// streams.Reader so Schema() is valid before Open.
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

// Read reads up to batchSize rows from the section. At end of section returns
// (nil, io.EOF). May return a non-nil batch with io.EOF — callers should
// process the batch before checking the error. Returned batches must be
// released by the caller after use.
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

	rb, readErr := r.inner.Read(ctx, r.alloc, batchSize)
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
	cols := appendMissingColumns(r.opts.Columns, predicateColumns(r.opts.Predicates))
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

	innerOptions := dataset.RowReaderOptions{
		Dataset:    dset,
		Columns:    dset.Columns(),
		Predicates: preds,
		Prefetch:   true,
	}
	if r.inner == nil {
		r.inner = columnar.NewReaderAdapter(innerOptions)
	} else {
		r.inner.Reset(innerOptions)
	}
	if err := r.inner.Open(ctx); err != nil {
		return fmt.Errorf("opening reader: %w", err)
	}

	r.ready = true
	return nil
}

// appendMissingColumns appends columns from src to dst that are not already
// present, de-duplicating by pointer identity.
func appendMissingColumns(dst, src []*Column) []*Column {
	exists := make(map[*Column]struct{}, len(dst))
	for _, col := range dst {
		exists[col] = struct{}{}
	}
	for _, col := range src {
		if _, ok := exists[col]; !ok {
			dst = append(dst, col)
		}
	}
	return dst
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
	ColumnTypeSectionIndex:     arrow.PrimitiveTypes.Int64,
	ColumnTypeColumnName:       arrow.BinaryTypes.String,
	ColumnTypeLabelValue:       arrow.BinaryTypes.String,
	ColumnTypeBloomFilter:      arrow.BinaryTypes.Binary,
	ColumnTypeStreamIDBitmap:   arrow.BinaryTypes.Binary,
	ColumnTypeUncompressedSize: arrow.PrimitiveTypes.Int64,
	ColumnTypeMinTimestamp:     arrow.FixedWidthTypes.Timestamp_ns,
	ColumnTypeMaxTimestamp:     arrow.FixedWidthTypes.Timestamp_ns,
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
