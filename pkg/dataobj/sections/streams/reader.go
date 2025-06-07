package streams

import (
	"context"
	"errors"
	"fmt"
	_ "io" // Used for documenting io.EOF.

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/arrowconv"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
)

// ReaderOptions customizes the behavior of a [Reader].
type ReaderOptions struct {
	// Columns to read. Each column must belong to the same [Section].
	Columns []*Column

	// Predicates holds a set of predicates to apply when reading the section.
	// Columns referenced in Predicates must be in the set of Columns.
	Predicates []Predicate

	// Allocator to use for allocating Arrow records. If nil,
	// [memory.DefaultAllocator] is used.
	Allocator memory.Allocator
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

// A Reader reads batches of rows from a [Section].
type Reader struct {
	opts   ReaderOptions
	schema *arrow.Schema // Set on [Reader.Reset].

	ready bool
	inner *dataset.Reader
	buf   []dataset.Row
}

// NewReader creates a new Reader from the provided options. Options are not
// validated until the first call to [Reader.Read].
func NewReader(opts ReaderOptions) *Reader {
	var r Reader
	r.Reset(opts)
	return &r
}

// Schema returns the [arrow.Schema] used by the Reader. Fields in the schema
// match the order of columns listed in [ReaderOptions].
//
// Names of fields in the schema are guaranteed to be unique per column but are
// not guaranteed to be stable.
//
// The returned Schema must not be modified.
func (r *Reader) Schema() *arrow.Schema { return r.schema }

// Read reads the batch of rows from the section, returning them as an Arrow
// record.
//
// If [ReaderOptions] has predicates, only rows that match the predicates are
// returned. If none of the next batchSize rows matched the predicate, Read
// returns a nil record with a nil error.
//
// Read will return an error if the next batch of rows could not be read due to
// invalid options or I/O errors. At the end of the section, Read returns nil,
// [io.EOF].
//
// Read may return a non-nil record with a non-nil error, including if the
// error is [io.EOF]. Callers should always process the record before
// processing the error value.
//
// When a record is returned, it will match the schema specified by
// [Reader.Schema]. These records must always be released after use.
func (r *Reader) Read(ctx context.Context, batchSize int) (arrow.Record, error) {
	if !r.ready {
		err := r.init()
		if err != nil {
			return nil, fmt.Errorf("initializing Reader: %w", err)
		}
	}

	r.buf = slicegrow.GrowToCap(r.buf, batchSize)
	r.buf = r.buf[:batchSize]

	builder := array.NewRecordBuilder(r.opts.Allocator, r.schema)
	defer builder.Release()

	n, readErr := r.inner.Read(ctx, r.buf)
	for rowIndex := range n {
		row := r.buf[rowIndex]

		for columnIndex, val := range row.Values {
			columnBuilder := builder.Field(columnIndex)

			if val.IsNil() {
				columnBuilder.AppendNull()
				continue
			}

			// Append non-null values. We switch on [ColumnType] here so it's easier
			// to follow the mapping of ColumnType to Arrow type. The mappings here
			// should align with both [columnToField] (for Arrow type) and
			// [Builder.encodeTo] (for dataset type).
			//
			// Passing our byte slices to [array.BinaryBuilder.Append] are safe; it
			// will copy the contents of the value and we can reuse the buffer on the
			// next call to [dataset.Reader.Read].
			columnType := r.opts.Columns[columnIndex].Type
			switch columnType {
			case ColumnTypeInvalid:
				columnBuilder.AppendNull() // Unsupported column
			case ColumnTypeStreamID: // Appends IDs as int64
				columnBuilder.(*array.Int64Builder).Append(val.Int64())
			case ColumnTypeMinTimestamp, ColumnTypeMaxTimestamp: // Values are nanosecond timestamps as int64
				columnBuilder.(*array.TimestampBuilder).Append(arrow.Timestamp(val.Int64()))
			case ColumnTypeLabel: // Appends labels as byte arrays
				columnBuilder.(*array.BinaryBuilder).Append(val.ByteArray())
			case ColumnTypeRows: // Appends rows as int64
				columnBuilder.(*array.Int64Builder).Append(val.Int64())
			case ColumnTypeUncompressedSize: // Appends uncompressed size as int64
				columnBuilder.(*array.Int64Builder).Append(val.Int64())
			default:
				// We'll only hit this if we added a new column type but forgot to
				// support reading it.
				return nil, fmt.Errorf("unsupported column type %s for column %d", columnType, columnIndex)
			}
		}
	}

	// We only return readErr after processing n so that we properly handle n>0
	// while also getting an error such as io.EOF.
	return builder.NewRecord(), readErr
}

func (r *Reader) init() error {
	if err := r.opts.Validate(); err != nil {
		return fmt.Errorf("invalid options: %w", err)
	} else if r.opts.Allocator == nil {
		r.opts.Allocator = memory.DefaultAllocator
	}

	dset, err := newColumnsDataset(r.opts.Columns)
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

	innerOptions := dataset.ReaderOptions{
		Dataset:         dset,
		Columns:         dset.Columns(),
		Predicates:      preds,
		TargetCacheSize: 16_000_000, // Permit up to 16MB of cache pages.
	}
	if r.inner == nil {
		r.inner = dataset.NewReader(innerOptions)
	} else {
		r.inner.Reset(innerOptions)
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

		return dataset.InPredicate{
			Column: col,
			Values: vals,
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

		fieldType := columnToField(p.Column).Type

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

func mustConvertType(dtype arrow.DataType) datasetmd.ValueType {
	toType, ok := arrowconv.DatasetType(dtype)
	if !ok {
		panic(fmt.Sprintf("unsupported dataset type %s", dtype))
	}
	return toType
}

// Reset discards any state and resets r with a new set of optiosn. This
// permits reusing a Reader rather than allocating a new one.
func (r *Reader) Reset(opts ReaderOptions) {
	r.opts = opts
	r.schema = columnsSchema(opts.Columns)

	r.ready = false

	if r.inner != nil {
		// Close our inner reader so it releases resources immediately. It'll be
		// fully reset on the next call to [Reader.init].
		_ = r.inner.Close()
	}
}

func columnsSchema(cols []*Column) *arrow.Schema {
	fields := make([]arrow.Field, 0, len(cols))
	for _, col := range cols {
		fields = append(fields, columnToField(col))
	}
	return arrow.NewSchema(fields, nil)
}

var columnDatatypes = map[ColumnType]arrow.DataType{
	ColumnTypeInvalid:          arrow.Null,
	ColumnTypeStreamID:         arrow.PrimitiveTypes.Int64,
	ColumnTypeMinTimestamp:     arrow.FixedWidthTypes.Timestamp_ns,
	ColumnTypeMaxTimestamp:     arrow.FixedWidthTypes.Timestamp_ns,
	ColumnTypeLabel:            arrow.BinaryTypes.Binary,
	ColumnTypeRows:             arrow.PrimitiveTypes.Int64,
	ColumnTypeUncompressedSize: arrow.PrimitiveTypes.Int64,
}

func columnToField(col *Column) arrow.Field {
	dtype, ok := columnDatatypes[col.Type]
	if !ok {
		dtype = arrow.Null
	}

	return arrow.Field{
		Name:     makeColumnName(col.Name, col.Type.String(), dtype),
		Type:     dtype,
		Nullable: true, // All columns are nullable.
	}
}

// makeColumnName returns a unique name for a [Column] and its expected data
// type.
//
// Unique names are used by unit tests to be able to produce expected rows.
func makeColumnName(label string, name string, dty arrow.DataType) string {
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
