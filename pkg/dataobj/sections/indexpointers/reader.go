package indexpointers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/arrowconv"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
	"github.com/grafana/loki/v3/pkg/xcap"
)

var tracer = otel.Tracer("pkg/dataobj/sections/indexpointers")

// ReaderOptions customizes the behavior of a [Reader].
type ReaderOptions struct {
	// Columns to read. Each column must belong to the same [Section]. Each
	// column type can appear once. A repeat makes the first ReadRecordBatch
	// fail.
	Columns []*Column

	// Predicates holds a set of predicates to apply when reading the section.
	// Columns referenced in Predicates must be in the set of Columns.
	Predicates []Predicate
}

// Validate returns an error if the opts is not valid. ReaderOptions are only
// valid when:
//
//   - Each [Column] in Columns belongs to the same [Section].
//   - Each [Predicate] in Predicates references a [Column] from Columns.
//   - Scalar values used in predicates are of a supported type: an int64,
//     uint64, timestamp, or a byte array.
func (opts *ReaderOptions) Validate() error {
	columnLookup := make(map[*Column]struct{}, len(opts.Columns))

	if len(opts.Columns) > 0 {
		// Ensure all columns belong to the same section.
		var checkSection *Section

		for _, col := range opts.Columns {
			if checkSection != nil && col.Section != checkSection {
				return fmt.Errorf("all columns must belong to the same section: got=%p want=%p", col.Section, checkSection)
			} else if checkSection == nil {
				checkSection = col.Section
			}
			columnLookup[col] = struct{}{}
		}
	}

	var errs []error

	validateColumn := func(col *Column) {
		if col == nil {
			errs = append(errs, fmt.Errorf("column is nil"))
		} else if _, found := columnLookup[col]; !found {
			errs = append(errs, fmt.Errorf("column %p not in Columns", col))
		}
	}

	validateScalar := func(s scalar.Scalar) {
		_, ok := arrowconv.DatasetType(s.DataType())
		if !ok {
			errs = append(errs, fmt.Errorf("unsupported scalar type %s", s.DataType()))
		}
	}

	for _, p := range opts.Predicates {
		walkPredicate(p, func(p Predicate) bool {
			// Validate that predicates reference valid columns and use valid
			// scalars.
			switch p := p.(type) {
			case nil: // End of walk; nothing to do.

			case AndPredicate: // Nothing to do.
			case OrPredicate: // Nothing to do.
			case NotPredicate: // Nothing to do.
			case TruePredicate: // Nothing to do.
			case FalsePredicate: // Nothing to do.

			case EqualPredicate:
				validateColumn(p.Column)
				validateScalar(p.Value)

			case InPredicate:
				validateColumn(p.Column)
				for _, val := range p.Values {
					validateScalar(val)
				}

			case GreaterThanPredicate:
				validateColumn(p.Column)
				validateScalar(p.Value)

			case LessThanPredicate:
				validateColumn(p.Column)
				validateScalar(p.Value)

			case FuncPredicate:
				validateColumn(p.Column)

			default:
				errs = append(errs, fmt.Errorf("unrecognized predicate type %T", p))
			}

			return true
		})
	}

	return errors.Join(errs...)
}

// A Reader reads batches of rows from an index pointers [Section].
// [Reader.ReadRecordBatch] returns each batch as a [RecordBatch] with one array
// for each entry in [ReaderOptions.Columns].
//
// A Reader is not safe for concurrent use.
type Reader struct {
	opts ReaderOptions

	ready bool
	inner *dataset.RowReader

	// readSpan for recording observations, it is created once during init
	// and is passed down via context so that inner readers can record
	// observations to it.
	readSpan trace.Span
}

var errReaderNotOpen = errors.New("reader not opened")

// NewReader creates a new Reader from the provided options. Options are not
// validated until the first call to [Reader.Open].
func NewReader(opts ReaderOptions) *Reader {
	var r Reader
	r.Reset(opts)
	return &r
}

// Open initializes Reader resources.
//
// Open must be called before [Reader.ReadRecordBatch]. Open is safe to call
// multiple times.
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
		ctx, r.readSpan = xcap.StartSpan(ctx, tracer, "indexpointers.Reader.Read")
	} else {
		// inject span into context for inner readers to record observations.
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
	if err := r.opts.Validate(); err != nil {
		return fmt.Errorf("invalid options: %w", err)
	}

	ctx, span := xcap.StartSpan(ctx, tracer, "indexpointers.Reader.Open")
	defer span.End()

	var innerSection *columnar.Section
	innerColumns := make([]*columnar.Column, len(r.opts.Columns))
	for i, column := range r.opts.Columns {
		if innerSection == nil {
			innerSection = column.Section.inner
		}
		innerColumns[i] = column.inner
	}

	dset, err := columnar.MakeDataset(innerSection, innerColumns)
	if err != nil {
		return fmt.Errorf("creating dataset: %w", err)
	} else if len(dset.Columns()) != len(r.opts.Columns) {
		return fmt.Errorf("dataset has %d columns, expected %d", len(dset.Columns()), len(r.opts.Columns))
	}

	columnLookup := make(map[*Column]dataset.Column, len(r.opts.Columns))
	for i, col := range dset.Columns() {
		columnLookup[r.opts.Columns[i]] = col
	}

	preds, err := mapPredicates(r.opts.Predicates, columnLookup)
	if err != nil {
		return fmt.Errorf("mapping predicates: %w", err)
	}

	innerOptions := dataset.RowReaderOptions{
		Dataset:           dset,
		ProjectedColumns:  dset.Columns(),
		Predicates:        preds,
		PrefetchAllOnOpen: true,
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

func mapPredicates(ps []Predicate, columnLookup map[*Column]dataset.Column) (predicates []dataset.Predicate, err error) {
	// For simplicity, [mapPredicate] and the functions it calls panic if they
	// encounter an unsupported conversion.
	//
	// These should normally be handled by [ReaderOptions.Validate], but we catch
	// any panics here to gracefully return an error to the caller instead of
	// potentially crashing the goroutine.
	defer func() {
		if r := recover(); r == nil {
			return
		} else if recoveredErr, ok := r.(error); ok {
			err = recoveredErr
		} else {
			err = fmt.Errorf("error while mapping: %v", r)
		}
	}()

	for _, p := range ps {
		predicates = append(predicates, mapPredicate(p, columnLookup))
	}
	return
}

func mapPredicate(p Predicate, columnLookup map[*Column]dataset.Column) dataset.Predicate {
	switch p := p.(type) {
	case AndPredicate:
		return dataset.AndPredicate{
			Left:  mapPredicate(p.Left, columnLookup),
			Right: mapPredicate(p.Right, columnLookup),
		}

	case OrPredicate:
		return dataset.OrPredicate{
			Left:  mapPredicate(p.Left, columnLookup),
			Right: mapPredicate(p.Right, columnLookup),
		}

	case NotPredicate:
		return dataset.NotPredicate{
			Inner: mapPredicate(p.Inner, columnLookup),
		}

	case TruePredicate:
		return dataset.TruePredicate{}

	case FalsePredicate:
		return dataset.FalsePredicate{}

	case EqualPredicate:
		col, ok := columnLookup[p.Column]
		if !ok {
			panic(fmt.Sprintf("column %p not found in column lookup", p.Column))
		}
		return dataset.EqualPredicate{
			Column: col,
			Value:  arrowconv.FromScalar(p.Value, mustConvertType(p.Value.DataType())),
		}

	case InPredicate:
		col, ok := columnLookup[p.Column]
		if !ok {
			panic(fmt.Sprintf("column %p not found in column lookup", p.Column))
		}

		vals := make([]dataset.Value, len(p.Values))
		for i := range p.Values {
			vals[i] = arrowconv.FromScalar(p.Values[i], mustConvertType(p.Values[i].DataType()))
		}

		var valueSet dataset.ValueSet
		switch col.ColumnDesc().Type.Physical {
		case datasetmd.PHYSICAL_TYPE_INT64:
			valueSet = dataset.NewInt64ValueSet(vals)
		case datasetmd.PHYSICAL_TYPE_UINT64:
			valueSet = dataset.NewUint64ValueSet(vals)
		case datasetmd.PHYSICAL_TYPE_BINARY:
			valueSet = dataset.NewBinaryValueSet(vals)
		default:
			panic("InPredicate not implemented for datatype")
		}

		return dataset.InPredicate{
			Column: col,
			Values: valueSet,
		}

	case GreaterThanPredicate:
		col, ok := columnLookup[p.Column]
		if !ok {
			panic(fmt.Sprintf("column %p not found in column lookup", p.Column))
		}
		return dataset.GreaterThanPredicate{
			Column: col,
			Value:  arrowconv.FromScalar(p.Value, mustConvertType(p.Value.DataType())),
		}

	case LessThanPredicate:
		col, ok := columnLookup[p.Column]
		if !ok {
			panic(fmt.Sprintf("column %p not found in column lookup", p.Column))
		}
		return dataset.LessThanPredicate{
			Column: col,
			Value:  arrowconv.FromScalar(p.Value, mustConvertType(p.Value.DataType())),
		}

	case FuncPredicate:
		col, ok := columnLookup[p.Column]
		if !ok {
			panic(fmt.Sprintf("column %p not found in column lookup", p.Column))
		}

		fieldType, ok := columnDatatypes[p.Column.Type]
		if !ok {
			fieldType = arrow.Null
		}

		return dataset.FuncPredicate{
			Column: col,
			Keep: func(_ dataset.Column, value dataset.Value) bool {
				return p.Keep(p.Column, arrowconv.ToScalar(value, fieldType))
			},
		}

	default:
		panic(fmt.Sprintf("unsupported predicate type %T", p))
	}
}

func mustConvertType(dtype arrow.DataType) datasetmd.PhysicalType {
	toType, ok := arrowconv.DatasetType(dtype)
	if !ok {
		panic(fmt.Sprintf("unsupported dataset type %s", dtype))
	}
	return toType
}

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
		// Close our inner reader so it releases resources immediately. It'll be
		// fully reset on the next call to [Reader.Open].
		_ = r.inner.Close()
	}
}

// Close closes the Reader and releases any resources it holds. Closed Readers
// can be reused by calling [Reader.Reset].
func (r *Reader) Close() error {
	if r.readSpan != nil {
		r.readSpan.End()
	}

	if r.inner != nil {
		return r.inner.Close()
	}

	return nil
}

// columnDatatypes maps a column type to the Arrow type of the scalars that a
// [FuncPredicate] on a column of that type receives.
var columnDatatypes = map[ColumnType]arrow.DataType{
	ColumnTypeInvalid:       arrow.Null,
	ColumnTypePath:          arrow.BinaryTypes.String,
	ColumnTypeMinTimestamp:  arrow.FixedWidthTypes.Timestamp_ns,
	ColumnTypeMaxTimestamp:  arrow.FixedWidthTypes.Timestamp_ns,
	ColumnTypeRunID:         arrow.PrimitiveTypes.Int64,
	ColumnTypeMinPostingKey: arrow.BinaryTypes.Binary,
	ColumnTypeMaxPostingKey: arrow.BinaryTypes.Binary,
	ColumnTypeShardBuckets:  arrow.PrimitiveTypes.Int64,
	ColumnTypeRunLength:     arrow.PrimitiveTypes.Int64,
}
