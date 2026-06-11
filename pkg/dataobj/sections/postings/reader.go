package postings

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/prometheus/prometheus/model/labels"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/arrowconv"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	memoryv2 "github.com/grafana/loki/v3/pkg/memory"
	"github.com/grafana/loki/v3/pkg/xcap"
)

var tracer = otel.Tracer("pkg/dataobj/sections/postings")

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

// validate returns an error if opts is invalid. ReaderOptions are valid when
// Columns is non-empty and every column belongs to the same Section.
func (opts *ReaderOptions) validate() error {
	if len(opts.Columns) == 0 {
		return errors.New("ReaderOptions.Columns must be non-empty")
	}

	columnLookup := make(map[*Column]struct{}, len(opts.Columns))

	// Ensure all columns belong to the same section.
	var section *Section
	for i, col := range opts.Columns {
		if col == nil {
			return fmt.Errorf("ReaderOptions.Columns[%d] is nil", i)
		}
		if section != nil && col.Section != section {
			return fmt.Errorf("all columns must belong to the same section: got=%p want=%p", col.Section, section)
		} else if section == nil {
			section = col.Section
		}
		columnLookup[col] = struct{}{}
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

// A Reader reads batches of rows from postings [Section]. The returned
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

// ErrLabelLookupNotImplemented indicates label name/value lookup has not been
// implemented for postings.Reader yet.
var ErrLabelLookupNotImplemented = errors.New("postings label lookup not implemented")

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
		return fmt.Errorf("invalid reader options: %w", err)
	}
	if r.opts.Allocator == nil {
		r.opts.Allocator = memory.DefaultAllocator
	}

	ctx, span := xcap.StartSpan(ctx, tracer, "postings.Reader.Open")
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

// readPointersBatchSize bounds per-batch allocations while amortising fixed costs when draining the adapter.
const readPointersBatchSize = 4096

// readPointersOutputSchema returns the Arrow schema emitted by ReadPointersForStreams.
func readPointersOutputSchema() *arrow.Schema {
	// makeColumnName mirrors pointers/reader.go:504-515.
	makeColumnName := func(label, name string, dty arrow.DataType) string {
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
	mkLabelled := func(label, typeName string, dty arrow.DataType) arrow.Field {
		return arrow.Field{
			Name:     makeColumnName(label, typeName, dty),
			Type:     dty,
			Nullable: true,
		}
	}
	mkPlain := func(typeName string, dty arrow.DataType) arrow.Field {
		return arrow.Field{
			Name:     makeColumnName("", typeName, dty),
			Type:     dty,
			Nullable: true,
		}
	}
	fields := []arrow.Field{
		// path column carries Tag="path" on the pointers side; all others have empty Tag.
		mkLabelled("path", pointers.ColumnTypePath.String(), arrow.BinaryTypes.String),
		mkPlain(pointers.ColumnTypeSection.String(), arrow.PrimitiveTypes.Int64),
		mkPlain(pointers.ColumnTypePointerKind.String(), arrow.PrimitiveTypes.Int64),
		mkPlain(pointers.ColumnTypeStreamID.String(), arrow.PrimitiveTypes.Int64),
		mkPlain(pointers.ColumnTypeStreamIDRef.String(), arrow.PrimitiveTypes.Int64),
		mkPlain(pointers.ColumnTypeMinTimestamp.String(), arrow.FixedWidthTypes.Timestamp_ns),
		mkPlain(pointers.ColumnTypeMaxTimestamp.String(), arrow.FixedWidthTypes.Timestamp_ns),
		mkPlain(pointers.ColumnTypeRowCount.String(), arrow.PrimitiveTypes.Int64),
		mkPlain(pointers.ColumnTypeUncompressedSize.String(), arrow.PrimitiveTypes.Int64),
		// Internal label-names column pointers.Reader appends with ColumnTypeStreamID; kept for schema parity.
		{Name: pointers.InternalLabelsFieldName, Type: arrow.BinaryTypes.String, Nullable: true},
	}
	return arrow.NewSchema(fields, nil)
}

// ReadPointersForStreams returns stream-pointer rows scoped to the provided stream refs.
// Stream refs include object path context, which avoids accidental cross-object stream-ID
// collisions when the same numeric stream ID appears in multiple source objects.
func (r *Reader) ReadPointersForStreams(ctx context.Context, streamRefs map[StreamRef]struct{}, start, end time.Time) (arrow.RecordBatch, error) {
	if !r.ready {
		return nil, errReaderNotOpen
	}

	ctx, span := xcap.StartSpan(ctx, tracer, "postings.Reader.ReadPointersForStreams")
	defer span.End()
	startTime := time.Now()
	defer r.alloc.Reclaim()

	outSchema := readPointersOutputSchema()
	alloc := r.opts.Allocator
	if alloc == nil {
		alloc = memory.DefaultAllocator
	}

	postingsRows, err := r.collectPostingsRows(ctx, streamRefs, start, end)
	if err != nil {
		return nil, err
	}

	if len(postingsRows) == 0 {
		rb := buildEmptyRecord(alloc, outSchema)
		xcap.RegionFromContext(ctx).Record(xcap.StatPostingsPointersRead.Observe(int64(0)))
		xcap.RegionFromContext(ctx).Record(xcap.StatPostingsPointersReadTime.Observe(time.Since(startTime).Seconds()))
		return rb, nil
	}

	rb, err := buildReadPointersRecord(alloc, outSchema, postingsRows)
	if err != nil {
		return nil, fmt.Errorf("building ReadPointersForStreams output batch: %w", err)
	}

	xcap.RegionFromContext(ctx).Record(xcap.StatPostingsPointersRead.Observe(rb.NumRows()))
	xcap.RegionFromContext(ctx).Record(xcap.StatPostingsPointersReadTime.Observe(time.Since(startTime).Seconds()))
	return rb, nil
}

// pointerJoinRow is one output tuple assembled from a postings row.
type pointerJoinRow struct {
	objectPath   string
	sectionIndex int64
	streamID     int64
	minTimestamp int64 // ns since epoch
	maxTimestamp int64 // ns since epoch
	hasBounds    bool
}

type pointerJoinKey struct {
	objectPath   string
	sectionIndex int64
	streamID     int64
}

// collectPostingsRows reads KindLabel rows and emits one tuple per stream-id bit set in each
// row's bitmap, applying streamRefs and [start,end] filters row-side.
func (r *Reader) collectPostingsRows(
	ctx context.Context,
	streamRefs map[StreamRef]struct{},
	start, end time.Time,
) ([]pointerJoinRow, error) {
	postingsCols, err := findColumnsByType(r.opts.Columns,
		ColumnTypeKind,
		ColumnTypeObjectPath,
		ColumnTypeSectionIndex,
		ColumnTypeStreamIDBitmap,
		ColumnTypeMinTimestamp,
		ColumnTypeMaxTimestamp,
	)
	if err != nil {
		return nil, fmt.Errorf("finding postings columns for ReadPointersForStreams: %w", err)
	}
	colKind, colObjectPath, colSectionIndex, colStreamIDBitmap, colMinTs, colMaxTs :=
		postingsCols[0], postingsCols[1], postingsCols[2], postingsCols[3], postingsCols[4], postingsCols[5]

	innerSection := colKind.Section.inner
	innerColumns := []*columnar.Column{
		colKind.inner, colObjectPath.inner, colSectionIndex.inner,
		colStreamIDBitmap.inner, colMinTs.inner, colMaxTs.inner,
	}

	dset, err := columnar.MakeDataset(innerSection, innerColumns)
	if err != nil {
		return nil, fmt.Errorf("creating postings dataset: %w", err)
	} else if len(dset.Columns()) != len(innerColumns) {
		return nil, fmt.Errorf("postings dataset has %d columns, expected %d", len(dset.Columns()), len(innerColumns))
	}

	columnLookup := map[*Column]dataset.Column{
		colKind:           dset.Columns()[0],
		colObjectPath:     dset.Columns()[1],
		colSectionIndex:   dset.Columns()[2],
		colStreamIDBitmap: dset.Columns()[3],
		colMinTs:          dset.Columns()[4],
		colMaxTs:          dset.Columns()[5],
	}

	kindEq := EqualPredicate{
		Column: colKind,
		Value:  scalar.NewInt64Scalar(int64(KindLabel)),
	}
	preds, err := mapPredicates([]Predicate{kindEq}, columnLookup)
	if err != nil {
		return nil, fmt.Errorf("mapping postings predicates: %w", err)
	}

	innerOptions := dataset.RowReaderOptions{
		Dataset:    dset,
		Columns:    dset.Columns(),
		Predicates: preds,
		Prefetch:   true,
	}
	postingsAdapter := columnar.NewReaderAdapter(innerOptions)
	defer func() { _ = postingsAdapter.Close() }()
	if err := postingsAdapter.Open(ctx); err != nil {
		return nil, fmt.Errorf("opening postings adapter for ReadPointersForStreams: %w", err)
	}

	// Schema for the inner postings projection (used by arrowconv.ToRecordBatch
	// to convert columnar batches into arrow batches).
	innerSchema := arrow.NewSchema([]arrow.Field{
		columnToField(colKind),
		columnToField(colObjectPath),
		columnToField(colSectionIndex),
		columnToField(colStreamIDBitmap),
		columnToField(colMinTs),
		columnToField(colMaxTs),
	}, nil)

	var out []pointerJoinRow
	seen := make(map[pointerJoinKey]int)

	for {
		colBatch, readErr := postingsAdapter.Read(ctx, r.alloc, readPointersBatchSize)
		if colBatch != nil && colBatch.NumRows() > 0 {
			rb, err := arrowconv.ToRecordBatch(colBatch, innerSchema)
			if err != nil {
				return nil, fmt.Errorf("converting postings columnar batch to arrow: %w", err)
			}
			if err := appendPostingsJoinRows(rb, streamRefs, start.UnixNano(), end.UnixNano(), &out, seen); err != nil {
				return nil, err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("reading postings batch for ReadPointersForStreams: %w", readErr)
		}
	}

	return out, nil
}

// appendPostingsJoinRows decodes each row's LSB bitmap into one tuple per stream-id, skipping
// rows outside [startNanos, endNanos] and stream-ids absent from streamRefs.
func appendPostingsJoinRows(
	rb arrow.RecordBatch,
	streamRefs map[StreamRef]struct{},
	startNanos, endNanos int64,
	out *[]pointerJoinRow,
	seen map[pointerJoinKey]int,
) error {
	if rb.NumRows() == 0 {
		return nil
	}

	objectPathCol, ok := rb.Column(1).(*array.String)
	if !ok {
		return fmt.Errorf("postings object_path column has unexpected type %T", rb.Column(1))
	}
	sectionIndexCol, ok := rb.Column(2).(*array.Int64)
	if !ok {
		return fmt.Errorf("postings section_index column has unexpected type %T", rb.Column(2))
	}
	bitmapCol, ok := rb.Column(3).(*array.Binary)
	if !ok {
		return fmt.Errorf("postings stream_id_bitmap column has unexpected type %T", rb.Column(3))
	}
	minTsCol, ok := rb.Column(4).(*array.Timestamp)
	if !ok {
		return fmt.Errorf("postings min_timestamp column has unexpected type %T", rb.Column(4))
	}
	maxTsCol, ok := rb.Column(5).(*array.Timestamp)
	if !ok {
		return fmt.Errorf("postings max_timestamp column has unexpected type %T", rb.Column(5))
	}

	filterStreamRefs := len(streamRefs) > 0

	for i := 0; i < int(rb.NumRows()); i++ {
		if objectPathCol.IsNull(i) || sectionIndexCol.IsNull(i) || bitmapCol.IsNull(i) {
			continue
		}

		// Time-range filter on the posting's [min, max]. A null bound cannot be
		// evaluated, so don't prune on it; emit the bound as zero in that case.
		var minTs, maxTs int64
		hasBounds := false
		if !minTsCol.IsNull(i) && !maxTsCol.IsNull(i) {
			hasBounds = true
			minTs = int64(minTsCol.Value(i))
			maxTs = int64(maxTsCol.Value(i))
			if maxTs < startNanos || minTs > endNanos {
				continue // posting does not overlap the query window
			}
		}

		objectPath := objectPathCol.Value(i)
		sectionIndex := sectionIndexCol.Value(i)
		bitmapBytes := bitmapCol.Value(i)

		// LSB bitmap: byte b, bit p => streamID = b*8 + p (pkg/memory.Bitmap).
		for byteIdx, b := range bitmapBytes {
			if b == 0 {
				continue
			}
			for bitPos := 0; bitPos < 8; bitPos++ {
				if (b>>uint(bitPos))&1 == 0 {
					continue
				}
				streamID := int64(byteIdx*8 + bitPos)
				if filterStreamRefs {
					streamRef := StreamRef{ObjectPath: objectPath, StreamID: streamID}
					if _, keep := streamRefs[streamRef]; !keep {
						continue
					}
				}
				key := pointerJoinKey{
					objectPath:   objectPath,
					sectionIndex: sectionIndex,
					streamID:     streamID,
				}
				if idx, exists := seen[key]; exists {
					mergePointerJoinRowBounds(&(*out)[idx], minTs, maxTs, hasBounds)
					continue
				}

				row := pointerJoinRow{
					objectPath:   objectPath,
					sectionIndex: sectionIndex,
					streamID:     streamID,
				}
				mergePointerJoinRowBounds(&row, minTs, maxTs, hasBounds)

				*out = append(*out, row)
				seen[key] = len(*out) - 1
			}
		}
	}
	return nil
}

func mergePointerJoinRowBounds(row *pointerJoinRow, minTs, maxTs int64, hasBounds bool) {
	if !hasBounds {
		return
	}
	if !row.hasBounds {
		row.minTimestamp = minTs
		row.maxTimestamp = maxTs
		row.hasBounds = true
		return
	}
	if minTs < row.minTimestamp {
		row.minTimestamp = minTs
	}
	if maxTs > row.maxTimestamp {
		row.maxTimestamp = maxTs
	}
}

// buildReadPointersRecord builds the output batch from the assembled join rows in
// readPointersOutputSchema field order.
func buildReadPointersRecord(alloc memory.Allocator, schema *arrow.Schema, rows []pointerJoinRow) (arrow.RecordBatch, error) {
	rb := array.NewRecordBuilder(alloc, schema)

	for _, row := range rows {
		rb.Field(0).(*array.StringBuilder).Append(row.objectPath)
		rb.Field(1).(*array.Int64Builder).Append(row.sectionIndex)
		rb.Field(2).(*array.Int64Builder).Append(int64(pointers.PointerKindStreamIndex))
		rb.Field(3).(*array.Int64Builder).Append(row.streamID)
		// stream_id_ref: new-format objects carry no cross-index ref, so it equals stream_id.
		rb.Field(4).(*array.Int64Builder).Append(row.streamID)
		rb.Field(5).(*array.TimestampBuilder).Append(arrow.Timestamp(row.minTimestamp))
		rb.Field(6).(*array.TimestampBuilder).Append(arrow.Timestamp(row.maxTimestamp))
		// row_count and uncompressed_size: postings carries no per-stream count; emit zero.
		rb.Field(7).(*array.Int64Builder).Append(0)
		rb.Field(8).(*array.Int64Builder).Append(0)
		// __streamLabelNames__: null here, populated by upstream label resolution.
		rb.Field(9).(*array.StringBuilder).AppendNull()
	}

	return rb.NewRecordBatch(), nil
}

// buildEmptyRecord returns a zero-row batch built via the same RecordBuilder path so its
// schema is byte-for-byte equal to the populated case.
func buildEmptyRecord(alloc memory.Allocator, schema *arrow.Schema) arrow.RecordBatch {
	rb := array.NewRecordBuilder(alloc, schema)
	return rb.NewRecordBatch()
}

// readBloomRowsBatchSize is the inner read size used by [Reader.ReadBloomRows]
// when draining the columnar adapter. Mirrors readPointersBatchSize (4096) —
// keeps per-batch allocations bounded while amortising fixed costs.
const readBloomRowsBatchSize = 4096

// ReadBloomRows returns arrow RecordBatch for KindBloom rows
func (r *Reader) ReadBloomRows(ctx context.Context) (arrow.RecordBatch, error) {
	if !r.ready {
		return nil, errReaderNotOpen
	}

	ctx, span := xcap.StartSpan(ctx, tracer, "postings.Reader.ReadBloomRows")
	defer span.End()
	defer r.alloc.Reclaim()

	alloc := r.opts.Allocator
	if alloc == nil {
		alloc = memory.DefaultAllocator
	}

	// Project the 4 output columns + the kind column (predicate-only).
	outputCols, err := findColumnsByType(r.opts.Columns,
		ColumnTypeObjectPath,
		ColumnTypeSectionIndex,
		ColumnTypeColumnName,
		ColumnTypeBloomFilter,
	)
	if err != nil {
		return nil, fmt.Errorf("finding bloom-row columns: %w", err)
	}
	kindCols, err := findColumnsByType(r.opts.Columns, ColumnTypeKind)
	if err != nil {
		return nil, fmt.Errorf("finding bloom-row columns: %w", err)
	}
	colKind := kindCols[0]

	// Inner projection [output cols..., kind]; kind drives the pushdown and is stripped from the result.
	innerSection := outputCols[0].Section.inner
	innerColumns := []*columnar.Column{
		outputCols[0].inner,
		outputCols[1].inner,
		outputCols[2].inner,
		outputCols[3].inner,
		colKind.inner,
	}

	dset, err := columnar.MakeDataset(innerSection, innerColumns)
	if err != nil {
		return nil, fmt.Errorf("creating dataset: %w", err)
	} else if len(dset.Columns()) != len(innerColumns) {
		return nil, fmt.Errorf("dataset has %d columns, expected %d", len(dset.Columns()), len(innerColumns))
	}

	columnLookup := map[*Column]dataset.Column{
		outputCols[0]: dset.Columns()[0],
		outputCols[1]: dset.Columns()[1],
		outputCols[2]: dset.Columns()[2],
		outputCols[3]: dset.Columns()[3],
		colKind:       dset.Columns()[4],
	}

	kindEq := EqualPredicate{
		Column: colKind,
		Value:  scalar.NewInt64Scalar(int64(KindBloom)),
	}
	preds, err := mapPredicates([]Predicate{kindEq}, columnLookup)
	if err != nil {
		return nil, fmt.Errorf("mapping predicates: %w", err)
	}

	innerOptions := dataset.RowReaderOptions{
		Dataset:    dset,
		Columns:    dset.Columns(),
		Predicates: preds,
		Prefetch:   true,
	}
	adapter := columnar.NewReaderAdapter(innerOptions)
	defer func() { _ = adapter.Close() }()
	if err := adapter.Open(ctx); err != nil {
		return nil, fmt.Errorf("opening ReadBloomRows adapter: %w", err)
	}

	// Inner schema (5 fields) must match the columnar batch column count.
	innerSchema := arrow.NewSchema([]arrow.Field{
		columnToField(outputCols[0]),
		columnToField(outputCols[1]),
		columnToField(outputCols[2]),
		columnToField(outputCols[3]),
		columnToField(colKind),
	}, nil)

	// Output schema (4 fields) — kind stripped.
	outputSchema := columnsSchema(outputCols)

	collected, err := r.collectBloomRowBatches(ctx, adapter, alloc, innerSchema, outputSchema)
	if err != nil {
		return nil, err
	}

	xcap.RegionFromContext(ctx).Record(xcap.StatPostingsBloomRowsRead.Observe(collected.NumRows()))
	return collected, nil
}

// collectBloomRowBatches drains adapter into a single output batch
func (r *Reader) collectBloomRowBatches(
	ctx context.Context,
	adapter *columnar.ReaderAdapter,
	alloc memory.Allocator,
	innerSchema *arrow.Schema,
	outputSchema *arrow.Schema,
) (arrow.RecordBatch, error) {
	rb := array.NewRecordBuilder(alloc, outputSchema)

	for {
		colBatch, readErr := adapter.Read(ctx, r.alloc, readBloomRowsBatchSize)
		if colBatch != nil && colBatch.NumRows() > 0 {
			innerRB, err := arrowconv.ToRecordBatch(colBatch, innerSchema)
			if err != nil {
				return nil, fmt.Errorf("converting bloom-row columnar batch to arrow: %w", err)
			}
			if err := appendBloomRowBatch(innerRB, rb); err != nil {
				return nil, err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("reading bloom rows: %w", readErr)
		}
	}
	return rb.NewRecordBatch(), nil
}

func appendBloomRowBatch(innerRB arrow.RecordBatch, rb *array.RecordBuilder) error {
	if innerRB.NumRows() == 0 {
		return nil
	}
	pathCol, ok := innerRB.Column(0).(*array.String)
	if !ok {
		return fmt.Errorf("bloom-row object_path column has unexpected type %T", innerRB.Column(0))
	}
	sectionCol, ok := innerRB.Column(1).(*array.Int64)
	if !ok {
		return fmt.Errorf("bloom-row section_index column has unexpected type %T", innerRB.Column(1))
	}
	columnNameCol, ok := innerRB.Column(2).(*array.String)
	if !ok {
		return fmt.Errorf("bloom-row column_name column has unexpected type %T", innerRB.Column(2))
	}
	bloomCol, ok := innerRB.Column(3).(*array.Binary)
	if !ok {
		return fmt.Errorf("bloom-row bloom_filter column has unexpected type %T", innerRB.Column(3))
	}

	pathB := rb.Field(0).(*array.StringBuilder)
	sectionB := rb.Field(1).(*array.Int64Builder)
	columnNameB := rb.Field(2).(*array.StringBuilder)
	bloomB := rb.Field(3).(*array.BinaryBuilder)

	for i := 0; i < int(innerRB.NumRows()); i++ {
		if pathCol.IsNull(i) {
			pathB.AppendNull()
		} else {
			pathB.Append(pathCol.Value(i))
		}
		if sectionCol.IsNull(i) {
			sectionB.AppendNull()
		} else {
			sectionB.Append(sectionCol.Value(i))
		}
		if columnNameCol.IsNull(i) {
			columnNameB.AppendNull()
		} else {
			columnNameB.Append(columnNameCol.Value(i))
		}
		if bloomCol.IsNull(i) {
			bloomB.AppendNull()
		} else {
			bloomB.Append(bloomCol.Value(i))
		}
	}
	return nil
}

// readResolveMatchingStreamRefsBatchSize bounds per-batch allocations while amortising fixed costs.
const readResolveMatchingStreamRefsBatchSize = 4096

type matcherIndex int

// ResolveMatchingStreamRefs returns the object-scoped streams matching every matcher against KindLabel rows.
func (r *Reader) ResolveMatchingStreamRefs(ctx context.Context, matchers []*labels.Matcher) (map[StreamRef]struct{}, map[StreamRef][]string, error) {
	if !r.ready {
		return nil, nil, errReaderNotOpen
	}

	ctx, span := xcap.StartSpan(ctx, tracer, "postings.Reader.ResolveMatchingStreamRefs")
	defer span.End()

	defer r.alloc.Reclaim()

	if len(matchers) == 0 {
		return nil, nil, nil
	}

	equalMatchers, otherMatchers := splitByMatchType(matchers)

	cols, err := findColumnsByType(r.opts.Columns,
		ColumnTypeObjectPath,
		ColumnTypeColumnName,
		ColumnTypeLabelValue,
		ColumnTypeStreamIDBitmap,
		ColumnTypeKind,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("finding ResolveMatchingStreamRefs columns: %w", err)
	}
	colObjectPath, colColumnName, colLabelValue, colStreamIDBitmap, colKind := cols[0], cols[1], cols[2], cols[3], cols[4]

	innerSection := cols[0].Section.inner
	innerColumns := []*columnar.Column{
		colObjectPath.inner,
		colColumnName.inner,
		colLabelValue.inner,
		colStreamIDBitmap.inner,
		colKind.inner,
	}

	dset, err := columnar.MakeDataset(innerSection, innerColumns)
	if err != nil {
		return nil, nil, fmt.Errorf("creating dataset: %w", err)
	} else if len(dset.Columns()) != len(innerColumns) {
		return nil, nil, fmt.Errorf("dataset has %d columns, expected %d", len(dset.Columns()), len(innerColumns))
	}

	columnLookup := map[*Column]dataset.Column{
		colObjectPath:     dset.Columns()[0],
		colColumnName:     dset.Columns()[1],
		colLabelValue:     dset.Columns()[2],
		colStreamIDBitmap: dset.Columns()[3],
		colKind:           dset.Columns()[4],
	}

	kindEq := EqualPredicate{
		Column: colKind,
		Value:  scalar.NewInt64Scalar(int64(KindLabel)),
	}
	var builtPredicate Predicate = kindEq
	// matchers is guaranteed non-empty here (early return above).

	// Names targeted by a non-Equal matcher; they need a broad column_name=Name branch.
	nonEqualNames := make(map[string]struct{})
	for _, m := range otherMatchers {
		nonEqualNames[m.Name] = struct{}{}
	}

	// addedBroad dedups the broad branch to at most one per Name.
	addedBroad := make(map[string]struct{})

	var branches []Predicate
	// Branch (a): Equal-only Names get the precise AND(name, value) pushdown.
	for _, m := range equalMatchers {
		if _, hasNonEqual := nonEqualNames[m.Name]; hasNonEqual {
			continue // handled by the broad branch below
		}
		pair := AndPredicate{
			Left: EqualPredicate{
				Column: colColumnName,
				Value:  scalar.NewStringScalar(m.Name),
			},
			Right: EqualPredicate{
				Column: colLabelValue,
				Value:  scalar.NewStringScalar(m.Value),
			},
		}
		branches = append(branches, pair)
	}
	// Branch (b): one broad column_name=Name per non-Equal Name; supersedes any
	// shared Equal pair (skipped above), whose value is re-checked row-side.
	for _, m := range otherMatchers {
		if _, exists := addedBroad[m.Name]; exists {
			continue
		}
		addedBroad[m.Name] = struct{}{}
		branches = append(branches, EqualPredicate{
			Column: colColumnName,
			Value:  scalar.NewStringScalar(m.Name),
		})
	}
	if len(branches) > 0 {
		combinedOr := branches[0]
		for i := 1; i < len(branches); i++ {
			combinedOr = OrPredicate{Left: combinedOr, Right: branches[i]}
		}
		builtPredicate = AndPredicate{Left: kindEq, Right: combinedOr}
	}

	preds, err := mapPredicates([]Predicate{builtPredicate}, columnLookup)
	if err != nil {
		return nil, nil, fmt.Errorf("mapping predicates: %w", err)
	}

	innerOptions := dataset.RowReaderOptions{
		Dataset:    dset,
		Columns:    dset.Columns(),
		Predicates: preds,
		Prefetch:   true,
	}
	adapter := columnar.NewReaderAdapter(innerOptions)
	defer func() { _ = adapter.Close() }()
	if err := adapter.Open(ctx); err != nil {
		return nil, nil, fmt.Errorf("opening ResolveMatchingStreamRefs adapter: %w", err)
	}

	// Inner schema (5 fields) used by arrowconv.ToRecordBatch — must match
	// the columnar batch column count.
	innerSchema := arrow.NewSchema([]arrow.Field{
		columnToField(colObjectPath),
		columnToField(colColumnName),
		columnToField(colLabelValue),
		columnToField(colStreamIDBitmap),
		columnToField(colKind),
	}, nil)

	perMatcherStreams := make(map[matcherIndex]map[StreamRef]struct{})
	labelNamesByStreamSet := make(map[StreamRef]map[string]struct{})
	activeMatchers := make([]*labels.Matcher, 0, len(matchers))

	for _, m := range matchers {
		if m == nil {
			continue
		}
		activeMatchers = append(activeMatchers, m)
	}

	// Drain the adapter, accumulating each row's stream IDs into the set of every matcher it satisfies.
	for {
		colBatch, readErr := adapter.Read(ctx, r.alloc, readResolveMatchingStreamRefsBatchSize)
		if colBatch != nil && colBatch.NumRows() > 0 {
			innerRB, convErr := arrowconv.ToRecordBatch(colBatch, innerSchema)
			if convErr != nil {
				return nil, nil, fmt.Errorf("converting ResolveMatchingStreamRefs columnar batch to arrow: %w", convErr)
			}
			if err := accumulateLabelRows(
				innerRB,
				activeMatchers,
				perMatcherStreams,
				labelNamesByStreamSet,
			); err != nil {
				return nil, nil, err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, nil, fmt.Errorf("reading ResolveMatchingStreamRefs batch: %w", readErr)
		}
	}

	// AND across matchers: a stream ref must appear in every matcher's set.
	var matchingStreams map[StreamRef]struct{}
	if len(activeMatchers) == 0 {
		// Defensive: short-circuited at the top of ResolveMatchingStreamRefs, but if
		// every matcher was nil we land here.
		matchingStreams = make(map[StreamRef]struct{})
	} else {
		first := perMatcherStreams[matcherIndex(0)]
		matchingStreams = make(map[StreamRef]struct{}, len(first))
		for streamRef := range first {
			matchingStreams[streamRef] = struct{}{}
		}
		for i := 1; i < len(activeMatchers) && len(matchingStreams) > 0; i++ {
			next := perMatcherStreams[matcherIndex(i)]
			for streamRef := range matchingStreams {
				if _, ok := next[streamRef]; !ok {
					delete(matchingStreams, streamRef)
				}
			}
		}
	}

	// Flatten the inversion to slices, scoped to the surviving matching stream refs.
	var labelNamesByStream map[StreamRef][]string
	if len(matchingStreams) > 0 {
		labelNamesByStream = make(map[StreamRef][]string, len(matchingStreams))
		for streamRef := range matchingStreams {
			nameSet := labelNamesByStreamSet[streamRef]
			if len(nameSet) == 0 {
				continue
			}
			names := make([]string, 0, len(nameSet))
			for n := range nameSet {
				names = append(names, n)
			}
			labelNamesByStream[streamRef] = names
		}
	}

	xcap.RegionFromContext(ctx).Record(xcap.StatPostingsLabelsResolved.Observe(int64(len(matchingStreams))))
	return matchingStreams, labelNamesByStream, nil
}

func splitByMatchType(ms []*labels.Matcher) (equal, other []*labels.Matcher) {
	for _, m := range ms {
		if m == nil {
			continue
		}
		if m.Type == labels.MatchEqual {
			equal = append(equal, m)
		} else {
			other = append(other, m)
		}
	}
	return equal, other
}

// accumulateLabelRows adds each row's stream IDs to perMatcherStreams[i] for every matcher
func accumulateLabelRows(
	rb arrow.RecordBatch,
	matchers []*labels.Matcher,
	perMatcherStreams map[matcherIndex]map[StreamRef]struct{},
	labelNamesByStreamSet map[StreamRef]map[string]struct{},
) error {
	if rb.NumRows() == 0 {
		return nil
	}
	objectPathCol, ok := rb.Column(0).(*array.String)
	if !ok {
		return fmt.Errorf("ResolveMatchingStreamRefs object_path has unexpected type %T", rb.Column(0))
	}
	columnNameCol, ok := rb.Column(1).(*array.String)
	if !ok {
		return fmt.Errorf("ResolveMatchingStreamRefs column_name has unexpected type %T", rb.Column(1))
	}
	labelValueCol, ok := rb.Column(2).(*array.String)
	if !ok {
		return fmt.Errorf("ResolveMatchingStreamRefs label_value has unexpected type %T", rb.Column(2))
	}
	bitmapCol, ok := rb.Column(3).(*array.Binary)
	if !ok {
		return fmt.Errorf("ResolveMatchingStreamRefs stream_id_bitmap has unexpected type %T", rb.Column(3))
	}

	// Group matcher indexes by Name for O(1) per-row dispatch.
	matchersByName := make(map[string][]matcherIndex, len(matchers))
	for i, m := range matchers {
		matchersByName[m.Name] = append(matchersByName[m.Name], matcherIndex(i))
	}

	for i := 0; i < int(rb.NumRows()); i++ {
		if objectPathCol.IsNull(i) || columnNameCol.IsNull(i) || labelValueCol.IsNull(i) || bitmapCol.IsNull(i) {
			continue
		}
		objectPath := objectPathCol.Value(i)
		name := columnNameCol.Value(i)
		value := labelValueCol.Value(i)
		bitmapBytes := bitmapCol.Value(i)

		// Matchers targeting this column; each contributes iff Matches(value).
		applicable := matchersByName[name]
		if len(applicable) == 0 {
			continue // defensive: pushdown should already exclude untargeted columns
		}
		matched := make([]matcherIndex, 0, len(applicable))
		for _, idx := range applicable {
			if matchers[idx].Matches(value) {
				matched = append(matched, idx)
			}
		}
		if len(matched) == 0 {
			continue
		}

		for byteIdx, b := range bitmapBytes {
			if b == 0 {
				continue
			}
			for bitPos := 0; bitPos < 8; bitPos++ {
				if (b>>uint(bitPos))&1 == 0 {
					continue
				}
				streamRef := StreamRef{
					ObjectPath: objectPath,
					StreamID:   int64(byteIdx*8 + bitPos),
				}

				for _, idx := range matched {
					set, exists := perMatcherStreams[idx]
					if !exists {
						set = make(map[StreamRef]struct{})
						perMatcherStreams[idx] = set
					}
					set[streamRef] = struct{}{}
				}

				// Record column_name for the inversion (scoped later to matching streams).
				nameSet, exists := labelNamesByStreamSet[streamRef]
				if !exists {
					nameSet = make(map[string]struct{})
					labelNamesByStreamSet[streamRef] = nameSet
				}
				nameSet[name] = struct{}{}
			}
		}
	}
	return nil
}

// mapPredicates translates a slice of postings [Predicate] values into the
// equivalent slice of [dataset.Predicate] values, using columnLookup to
// resolve each [Column] to its corresponding [dataset.Column]
func mapPredicates(ps []Predicate, columnLookup map[*Column]dataset.Column) (predicates []dataset.Predicate, err error) {
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

// mapPredicate translates a single postings [Predicate] into the equivalent [dataset.Predicate]
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

// mustConvertType returns the [datasetmd.PhysicalType] corresponding to arrowType
func mustConvertType(arrowType arrow.DataType) datasetmd.PhysicalType {
	toType, ok := arrowconv.DatasetType(arrowType)
	if !ok {
		panic(fmt.Sprintf("unsupported arrow type %s", arrowType))
	}
	return toType
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

// ResolveLabelNames returns distinct label names for the provided stream refs.
func (r *Reader) ResolveLabelNames(ctx context.Context, streamRefs map[StreamRef]struct{}) ([]string, error) {
	if !r.ready {
		return nil, errReaderNotOpen
	}
	return r.resolveDistinctLabelStrings(ctx, streamRefs, ColumnTypeColumnName)
}

// ResolveLabelValues returns distinct label values for the provided stream refs.
func (r *Reader) ResolveLabelValues(ctx context.Context, streamRefs map[StreamRef]struct{}) ([]string, error) {
	if !r.ready {
		return nil, errReaderNotOpen
	}
	return r.resolveDistinctLabelStrings(ctx, streamRefs, ColumnTypeLabelValue)
}

func (r *Reader) resolveDistinctLabelStrings(
	ctx context.Context,
	streamRefs map[StreamRef]struct{},
	labelColumnType ColumnType,
) ([]string, error) {
	defer r.alloc.Reclaim()

	labelCols, err := findColumnsByType(r.opts.Columns, labelColumnType)
	if err != nil {
		return nil, fmt.Errorf("finding %s column: %w", labelColumnType, err)
	}
	kindCols, err := findColumnsByType(r.opts.Columns, ColumnTypeKind)
	if err != nil {
		return nil, fmt.Errorf("finding kind column: %w", err)
	}
	colLabel, colKind := labelCols[0], kindCols[0]

	filterByStreamRefs := streamRefs != nil
	if filterByStreamRefs && len(streamRefs) == 0 {
		return nil, nil
	}

	innerColumns := []*columnar.Column{colLabel.inner, colKind.inner}
	var (
		colObjectPath, colStreamIDBitmap *Column
	)
	if filterByStreamRefs {
		streamFilterCols, err := findColumnsByType(r.opts.Columns, ColumnTypeObjectPath, ColumnTypeStreamIDBitmap)
		if err != nil {
			return nil, fmt.Errorf("finding stream filter columns: %w", err)
		}
		colObjectPath, colStreamIDBitmap = streamFilterCols[0], streamFilterCols[1]
		innerColumns = append(innerColumns, colObjectPath.inner, colStreamIDBitmap.inner)
	}

	innerSection := colLabel.Section.inner
	dset, err := columnar.MakeDataset(innerSection, innerColumns)
	if err != nil {
		return nil, fmt.Errorf("creating dataset: %w", err)
	}
	if len(dset.Columns()) != len(innerColumns) {
		return nil, fmt.Errorf("dataset has %d columns, expected %d", len(dset.Columns()), len(innerColumns))
	}

	columnLookup := map[*Column]dataset.Column{
		colLabel: dset.Columns()[0],
		colKind:  dset.Columns()[1],
	}
	if filterByStreamRefs {
		columnLookup[colObjectPath] = dset.Columns()[2]
		columnLookup[colStreamIDBitmap] = dset.Columns()[3]
	}

	kindEq := EqualPredicate{
		Column: colKind,
		Value:  scalar.NewInt64Scalar(int64(KindLabel)),
	}
	preds, err := mapPredicates([]Predicate{kindEq}, columnLookup)
	if err != nil {
		return nil, fmt.Errorf("mapping predicates: %w", err)
	}

	innerOptions := dataset.RowReaderOptions{
		Dataset:    dset,
		Columns:    dset.Columns(),
		Predicates: preds,
		Prefetch:   true,
	}
	adapter := columnar.NewReaderAdapter(innerOptions)
	defer func() { _ = adapter.Close() }()
	if err := adapter.Open(ctx); err != nil {
		return nil, fmt.Errorf("opening adapter: %w", err)
	}

	fields := []arrow.Field{
		columnToField(colLabel),
		columnToField(colKind),
	}
	if filterByStreamRefs {
		fields = append(fields, columnToField(colObjectPath), columnToField(colStreamIDBitmap))
	}
	innerSchema := arrow.NewSchema(fields, nil)

	seen := make(map[string]struct{})
	var values []string
	for {
		colBatch, readErr := adapter.Read(ctx, r.alloc, readResolveMatchingStreamRefsBatchSize)
		if colBatch != nil && colBatch.NumRows() > 0 {
			rb, convErr := arrowconv.ToRecordBatch(colBatch, innerSchema)
			if convErr != nil {
				return nil, fmt.Errorf("converting columnar batch to arrow: %w", convErr)
			}

			valueCol, ok := rb.Column(0).(*array.String)
			if !ok {
				return nil, fmt.Errorf("label column has unexpected type %T", rb.Column(0))
			}

			var (
				objectPathCol *array.String
				bitmapCol     *array.Binary
			)
			if filterByStreamRefs {
				objectPathCol, ok = rb.Column(2).(*array.String)
				if !ok {
					return nil, fmt.Errorf("object_path column has unexpected type %T", rb.Column(2))
				}
				bitmapCol, ok = rb.Column(3).(*array.Binary)
				if !ok {
					return nil, fmt.Errorf("stream_id_bitmap column has unexpected type %T", rb.Column(3))
				}
			}

			for i := range int(rb.NumRows()) {
				if valueCol.IsNull(i) {
					continue
				}

				if filterByStreamRefs {
					if objectPathCol.IsNull(i) || bitmapCol.IsNull(i) {
						continue
					}
					if !bitmapMatchesAnyStreamRef(objectPathCol.Value(i), bitmapCol.Value(i), streamRefs) {
						continue
					}
				}

				value := valueCol.Value(i)
				if _, exists := seen[value]; exists {
					continue
				}
				seen[value] = struct{}{}
				values = append(values, value)
			}
		}

		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("reading label rows: %w", readErr)
		}
	}

	return values, nil
}

func bitmapMatchesAnyStreamRef(objectPath string, bitmapBytes []byte, streamRefs map[StreamRef]struct{}) bool {
	for byteIdx, bitmapByte := range bitmapBytes {
		if bitmapByte == 0 {
			continue
		}
		for bitPos := 0; bitPos < 8; bitPos++ {
			if (bitmapByte>>uint(bitPos))&1 == 0 {
				continue
			}
			streamRef := StreamRef{
				ObjectPath: objectPath,
				StreamID:   int64(byteIdx*8 + bitPos),
			}
			if _, found := streamRefs[streamRef]; found {
				return true
			}
		}
	}
	return false
}

// StreamLabelColumnNames returns the distinct column_name values across KindLabel rows
func (r *Reader) StreamLabelColumnNames(ctx context.Context) ([]string, error) {
	if !r.ready {
		return nil, errReaderNotOpen
	}

	ctx, span := xcap.StartSpan(ctx, tracer, "postings.Reader.StreamLabelColumnNames")
	defer span.End()
	defer r.alloc.Reclaim()

	// Project column_name (output) + kind (predicate-only, gates to KindLabel).
	nameCols, err := findColumnsByType(r.opts.Columns, ColumnTypeColumnName)
	if err != nil {
		return nil, fmt.Errorf("finding column_name column: %w", err)
	}
	kindCols, err := findColumnsByType(r.opts.Columns, ColumnTypeKind)
	if err != nil {
		return nil, fmt.Errorf("finding kind column: %w", err)
	}
	colColumnName, colKind := nameCols[0], kindCols[0]

	innerSection := colColumnName.Section.inner
	innerColumns := []*columnar.Column{colColumnName.inner, colKind.inner}

	dset, err := columnar.MakeDataset(innerSection, innerColumns)
	if err != nil {
		return nil, fmt.Errorf("creating dataset: %w", err)
	} else if len(dset.Columns()) != len(innerColumns) {
		return nil, fmt.Errorf("dataset has %d columns, expected %d", len(dset.Columns()), len(innerColumns))
	}

	columnLookup := map[*Column]dataset.Column{
		colColumnName: dset.Columns()[0],
		colKind:       dset.Columns()[1],
	}

	kindEq := EqualPredicate{
		Column: colKind,
		Value:  scalar.NewInt64Scalar(int64(KindLabel)),
	}
	preds, err := mapPredicates([]Predicate{kindEq}, columnLookup)
	if err != nil {
		return nil, fmt.Errorf("mapping predicates: %w", err)
	}

	innerOptions := dataset.RowReaderOptions{
		Dataset:    dset,
		Columns:    dset.Columns(),
		Predicates: preds,
		Prefetch:   true,
	}
	adapter := columnar.NewReaderAdapter(innerOptions)
	defer func() { _ = adapter.Close() }()
	if err := adapter.Open(ctx); err != nil {
		return nil, fmt.Errorf("opening StreamLabelColumnNames adapter: %w", err)
	}

	innerSchema := arrow.NewSchema([]arrow.Field{
		columnToField(colColumnName),
		columnToField(colKind),
	}, nil)

	seen := make(map[string]struct{})
	var names []string
	for {
		colBatch, readErr := adapter.Read(ctx, r.alloc, readBloomRowsBatchSize)
		if colBatch != nil && colBatch.NumRows() > 0 {
			rb, err := arrowconv.ToRecordBatch(colBatch, innerSchema)
			if err != nil {
				return nil, fmt.Errorf("converting columnar batch to arrow: %w", err)
			}
			nameCol, ok := rb.Column(0).(*array.String)
			if !ok {
				return nil, fmt.Errorf("column_name column has unexpected type %T", rb.Column(0))
			}
			for i := 0; i < int(rb.NumRows()); i++ {
				if nameCol.IsNull(i) {
					continue
				}
				name := nameCol.Value(i)
				if name == "" {
					continue
				}
				if _, dup := seen[name]; dup {
					continue
				}
				seen[name] = struct{}{}
				names = append(names, name)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("reading column_name rows: %w", readErr)
		}
	}
	return names, nil
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
