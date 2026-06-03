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
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
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

// Validate returns an error if the opts is not valid. ReaderOptions are only
// valid when:
//
// - Columns is non-empty.
// - Each [Column] in Columns belongs to the same [Section].
// - Each [Predicate] in Predicates references a [Column] from Columns.
// - Scalar values used in predicates are of a supported type: an int64,
// uint64, timestamp, or a byte array.
func (opts *ReaderOptions) Validate() error {
	if len(opts.Columns) == 0 {
		return errors.New("ReaderOptions.Columns must be non-empty")
	}

	columnLookup := make(map[*Column]struct{}, len(opts.Columns))

	// Ensure all columns belong to the same section.
	var checkSection *Section
	for i, col := range opts.Columns {
		if col == nil {
			return fmt.Errorf("ReaderOptions.Columns[%d] is nil", i)
		}
		if checkSection != nil && col.Section != checkSection {
			return fmt.Errorf("all columns must belong to the same section: got=%p want=%p", col.Section, checkSection)
		} else if checkSection == nil {
			checkSection = col.Section
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

	// streamsSec is the sibling streams.Section discovered at Open time on
	// the same parent dataobj.Object. It is nil when the postings section
	// was opened via [Open] (no parent back-pointer) or when the parent
	// object does not contain a streams section. [Reader.ReadPointers] (per
	// ) requires this field to be non-nil; other methods do not.
	streamsSec *streams.Section
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
	// Clear the cached sibling streams.Section so a Reset that swaps to a
	// section opened via the plain [Open] (no parent back-pointer) does not
	// silently retain the streams section from the previous configuration.
	// init() will re-discover it when the new section's parent is non-nil.
	r.streamsSec = nil
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
	if err := r.opts.Validate(); err != nil {
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

	// Locate the sibling streams.Section if the postings.Section was opened
	// via OpenWithObject (i.e. retains a back-pointer to its parent
	// dataobj.Object). When no parent reference is available — the legacy
	// Open path, used by fixtures that don't need ReadPointers — leave
	// streamsSec nil. ReadPointers reports an explicit error in
	// that case so other methods (Read, future ReadBloomRows, ResolveLabels)
	// remain usable on postings-only fixtures.
	if parent := cols[0].Section.parent; parent != nil {
		for _, sec := range parent.Sections() {
			if !streams.CheckSection(sec) {
				continue
			}
			sSec, err := streams.Open(ctx, sec)
			if err != nil {
				return fmt.Errorf("opening sibling streams section: %w", err)
			}
			r.streamsSec = sSec
			break
		}
	}

	r.ready = true
	return nil
}

// readPointersBatchSize is the row batch size used internally by
// [Reader.ReadPointers] when draining the postings-side and streams-side
// columnar adapters. It mirrors the metastore default at
// indexSectionsReader.batchSize, which falls back to 8192 when unset. We use
// 4096 here to keep per-batch allocations bounded while still amortising
// per-batch fixed costs; tune in a follow-up if profiling indicates a better
// value.
const readPointersBatchSize = 4096

// streamRowMetadata is the per-stream join lookup record built from the
// streams-section read.
type streamRowMetadata struct {
	minTimestamp     int64 // ns since epoch
	maxTimestamp     int64 // ns since epoch
	rowCount         int64
	uncompressedSize int64
}

// readPointersOutputSchema produces the byte-for-byte Arrow schema that
// today's pointers.Reader (configured like
// metastore.indexSectionsReader.openStreamPointersReader) emits for the
// 9-column default pointer-scan projection. It is the schema-compatibility / schema
// invariant 's reader-dispatch boundary depends on.
//
// Schema fields (in order):
//
// 1. path → utf8
// 2. section → int64
// 3. pointer_kind → int64
// 4. stream_id → int64
// 5. stream_id_ref → int64
// 6. min_timestamp → timestamp[ns]
// 7. max_timestamp → timestamp[ns]
// 8. row_count → int64
// 9. uncompressed_size → int64
// 10. __streamLabelNames__ → utf8 (internal field appended by
// pointers.Reader whenever ColumnTypeStreamID is in the projection —
// pointers/reader.go:461-465; this stub-projection is populated with
// nulls because postings has no inline label-name info at this layer)
//
// Field naming follows the pointers.makeColumnName(label, name, dtype)
// convention. The pointers section's path column is built with
// dataset.NewColumnBuilder("path", ...) (pointers/builder.go:248), so its
// Tag is "path" — the resulting Arrow field name is "path.path.utf8". All
// other pointers columns are built via numberColumnBuilder("", ...) with
// an empty Tag, yielding "<type>.<dtype>" names.
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
		// path column carries Tag="path" on the pointers side — see
		// pointers/builder.go:248 (dataset.NewColumnBuilder("path", ...)).
		// All other pointers columns have empty Tag.
		mkLabelled("path", pointers.ColumnTypePath.String(), arrow.BinaryTypes.String),
		mkPlain(pointers.ColumnTypeSection.String(), arrow.PrimitiveTypes.Int64),
		mkPlain(pointers.ColumnTypePointerKind.String(), arrow.PrimitiveTypes.Int64),
		mkPlain(pointers.ColumnTypeStreamID.String(), arrow.PrimitiveTypes.Int64),
		mkPlain(pointers.ColumnTypeStreamIDRef.String(), arrow.PrimitiveTypes.Int64),
		mkPlain(pointers.ColumnTypeMinTimestamp.String(), arrow.FixedWidthTypes.Timestamp_ns),
		mkPlain(pointers.ColumnTypeMaxTimestamp.String(), arrow.FixedWidthTypes.Timestamp_ns),
		mkPlain(pointers.ColumnTypeRowCount.String(), arrow.PrimitiveTypes.Int64),
		mkPlain(pointers.ColumnTypeUncompressedSize.String(), arrow.PrimitiveTypes.Int64),
		// pointers.Reader appends this internal label-names column whenever
		// ColumnTypeStreamID is part of the projection — preserved here for
		// byte-for-byte schema parity (Schema().Equal()).
		{Name: pointers.InternalLabelsFieldName, Type: arrow.BinaryTypes.String, Nullable: true},
	}
	return arrow.NewSchema(fields, nil)
}

// streamsTimeRangePredicate builds the streams-section predicate equivalent
// to pointers.WhereTimeRangeOverlapsWith — a stream's [minTs, maxTs] overlaps
// [start, end] iff maxTs >= start AND minTs <= end. The streams package
// does not (currently) export a WhereTimeRangeOverlapsWith helper, so we
// construct it here using the same NotPredicate{LessThanPredicate} /
// NotPredicate{GreaterThanPredicate} idiom that
// metastore.buildTimeRangePredicate uses.
func streamsTimeRangePredicate(colMinTs, colMaxTs *streams.Column, sStart, sEnd *scalar.Timestamp) streams.Predicate {
	maxCheck := streams.NotPredicate{Inner: streams.LessThanPredicate{Column: colMaxTs, Value: sStart}}
	minCheck := streams.NotPredicate{Inner: streams.GreaterThanPredicate{Column: colMinTs, Value: sEnd}}
	return streams.AndPredicate{Left: maxCheck, Right: minCheck}
}

// ReadPointers returns an Arrow RecordBatch of stream-pointer rows matching
// the provided streamIDs and time range. The returned batch's Schema is
// byte-for-byte identical to today's pointers.Reader-fed openStreamPointersReader
// output projecting the FULL 9-column default pointer-scan column set
// (path, section, pointer_kind, stream_id, stream_id_ref, min_timestamp,
// max_timestamp, row_count, uncompressed_size) — Success Criterion #4,
// . Internally performs a join between the postings section and the
// sibling streams section of the same dataobj.
//
// streamIDs filters the result to streams in the provided set. When
// streamIDs is empty (nil or len==0), no stream-ID filter is applied — all
// streams whose [min_timestamp, max_timestamp] range overlaps [start, end]
// are returned.
//
// ReadPointers requires the postings.Section to have been opened via
// [OpenWithObject] (so the parent dataobj.Object is reachable and the
// sibling streams section can be located at Open time). When the section
// was opened via the legacy [Open], or when the parent object does not
// contain a streams section, ReadPointers returns an error.
func (r *Reader) ReadPointers(ctx context.Context, streamIDs map[int64]struct{}, start, end time.Time) (arrow.RecordBatch, error) {
	if !r.ready {
		return nil, errReaderNotOpen
	}
	if r.streamsSec == nil {
		return nil, fmt.Errorf("ReadPointers requires a sibling streams section in the same dataobj; none was found at Open")
	}

	ctx, span := xcap.StartSpan(ctx, tracer, "postings.Reader.ReadPointers")
	defer span.End()
	startTime := time.Now()
	defer r.alloc.Reclaim()

	outSchema := readPointersOutputSchema()
	alloc := r.opts.Allocator
	if alloc == nil {
		alloc = memory.DefaultAllocator
	}

	// (1) STREAMS-SIDE READ — apply time-range and stream-ID predicates where
	// the timestamp columns physically live. The streams-side projection is
	// (stream_id, min_timestamp, max_timestamp, rows, uncompressed_size).
	streamMeta, err := r.collectStreamRowMetadata(ctx, alloc, streamIDs, start, end)
	if err != nil {
		return nil, err
	}

	// If no streams match the predicates, short-circuit with an empty
	// 9-column batch. Building the empty record via the same RecordBuilder
	// path guarantees the schema is byte-for-byte identical to the populated
	// case (Schema().Equal() invariant — schema-compatibility).
	if len(streamMeta) == 0 {
		rb := buildEmptyRecord(alloc, outSchema)
		xcap.RegionFromContext(ctx).Record(xcap.StatPostingsPointersRead.Observe(int64(0)))
		xcap.RegionFromContext(ctx).Record(xcap.StatPostingsPointersReadTime.Observe(time.Since(startTime).Seconds()))
		return rb, nil
	}

	// (2) POSTINGS-SIDE READ — apply kind=KindLabel predicate where the kind
	// column physically lives. Project the four columns the join needs:
	// (kind, object_path, section_index, stream_id_bitmap).
	postingsRows, err := r.collectPostingsRows(ctx, streamMeta)
	if err != nil {
		return nil, err
	}

	// (3) ASSEMBLE 9-COLUMN OUTPUT — emit one join tuple per (postings row,
	// matching stream id).
	rb, err := buildReadPointersRecord(alloc, outSchema, postingsRows)
	if err != nil {
		return nil, fmt.Errorf("building ReadPointers output batch: %w", err)
	}

	xcap.RegionFromContext(ctx).Record(xcap.StatPostingsPointersRead.Observe(rb.NumRows()))
	xcap.RegionFromContext(ctx).Record(xcap.StatPostingsPointersReadTime.Observe(time.Since(startTime).Seconds()))
	return rb, nil
}

// collectStreamRowMetadata reads the sibling streams.Section using the
// provided time-range and (optional) stream-ID predicates and returns a Go
// map keyed by stream_id with the per-stream join metadata
// (minTimestamp, maxTimestamp, rowCount, uncompressedSize).
//
// Predicate pushdown: streamIDs (when non-empty) is pushed as an
// InPredicate on the stream_id column; the time-range is pushed as the
// AndPredicate(NotLT(maxTs, start), NotGT(minTs, end)) shape mirroring
// metastore.buildTimeRangePredicate. Pages whose statistics fall outside
// the predicate are skipped by the dataset layer.
func (r *Reader) collectStreamRowMetadata(
	ctx context.Context,
	alloc memory.Allocator,
	streamIDs map[int64]struct{},
	start, end time.Time,
) (map[int64]streamRowMetadata, error) {
	streamsCols, err := findStreamsColumnsByType(r.streamsSec.Columns(),
		streams.ColumnTypeStreamID,
		streams.ColumnTypeMinTimestamp,
		streams.ColumnTypeMaxTimestamp,
		streams.ColumnTypeRows,
		streams.ColumnTypeUncompressedSize,
	)
	if err != nil {
		return nil, fmt.Errorf("finding streams columns: %w", err)
	}
	colStreamID, colMinTs, colMaxTs, colRows, colUncompressed := streamsCols[0], streamsCols[1], streamsCols[2], streamsCols[3], streamsCols[4]

	sStart := scalar.NewTimestampScalar(arrow.Timestamp(start.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)
	sEnd := scalar.NewTimestampScalar(arrow.Timestamp(end.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)

	var streamsPreds []streams.Predicate
	streamsPreds = append(streamsPreds, streamsTimeRangePredicate(colMinTs, colMaxTs, sStart, sEnd))
	if len(streamIDs) > 0 {
		vals := make([]scalar.Scalar, 0, len(streamIDs))
		for id := range streamIDs {
			vals = append(vals, scalar.NewInt64Scalar(id))
		}
		streamsPreds = append(streamsPreds, streams.InPredicate{Column: colStreamID, Values: vals})
	}

	streamsReader := streams.NewReader(streams.ReaderOptions{
		Columns:    []*streams.Column{colStreamID, colMinTs, colMaxTs, colRows, colUncompressed},
		Predicates: streamsPreds,
		Allocator:  alloc,
	})
	if err := streamsReader.Open(ctx); err != nil {
		return nil, fmt.Errorf("opening streams reader for ReadPointers: %w", err)
	}
	defer func() { _ = streamsReader.Close() }()

	out := make(map[int64]streamRowMetadata)
	for {
		rb, readErr := streamsReader.Read(ctx, readPointersBatchSize)
		if rb != nil {
			if err := accumulateStreamMeta(rb, out); err != nil {
				return nil, err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("reading streams batch for ReadPointers: %w", readErr)
		}
	}
	return out, nil
}

// accumulateStreamMeta extracts (stream_id, minTs, maxTs, rows,
// uncompressed_size) tuples from rb and populates out. Column order matches
// the projection in collectStreamRowMetadata: (stream_id, min_ts, max_ts,
// rows, uncompressed_size). Rows with a null stream_id are skipped (cannot
// participate in the join).
//
// Stream IDs must be unique across the streams section read. If two rows
// surface the same stream_id (multi-page sections, future format quirks,
// or builder bugs) accumulateStreamMeta returns an error rather than
// silently overwriting earlier metadata — `rowCount` / `uncompressedSize`
// would otherwise be silently truncated to the LAST occurrence. Surfacing
// the duplicate as an error preserves the postings+streams join invariant
// (one streams row per stream_id) and makes upstream bugs observable.
func accumulateStreamMeta(rb arrow.RecordBatch, out map[int64]streamRowMetadata) error {
	if rb.NumRows() == 0 {
		return nil
	}
	streamIDCol, ok := rb.Column(0).(*array.Int64)
	if !ok {
		return fmt.Errorf("streams stream_id column has unexpected type %T", rb.Column(0))
	}
	minTsCol, ok := rb.Column(1).(*array.Timestamp)
	if !ok {
		return fmt.Errorf("streams min_timestamp column has unexpected type %T", rb.Column(1))
	}
	maxTsCol, ok := rb.Column(2).(*array.Timestamp)
	if !ok {
		return fmt.Errorf("streams max_timestamp column has unexpected type %T", rb.Column(2))
	}
	rowsCol, ok := rb.Column(3).(*array.Int64)
	if !ok {
		return fmt.Errorf("streams rows column has unexpected type %T", rb.Column(3))
	}
	uncompressedCol, ok := rb.Column(4).(*array.Int64)
	if !ok {
		return fmt.Errorf("streams uncompressed_size column has unexpected type %T", rb.Column(4))
	}

	for i := 0; i < int(rb.NumRows()); i++ {
		if streamIDCol.IsNull(i) {
			continue
		}
		id := streamIDCol.Value(i)
		meta := streamRowMetadata{}
		if !minTsCol.IsNull(i) {
			meta.minTimestamp = int64(minTsCol.Value(i))
		}
		if !maxTsCol.IsNull(i) {
			meta.maxTimestamp = int64(maxTsCol.Value(i))
		}
		if !rowsCol.IsNull(i) {
			meta.rowCount = rowsCol.Value(i)
		}
		if !uncompressedCol.IsNull(i) {
			meta.uncompressedSize = uncompressedCol.Value(i)
		}
		if _, exists := out[id]; exists {
			return fmt.Errorf("duplicate stream_id %d in streams section", id)
		}
		out[id] = meta
	}
	return nil
}

// pointerJoinRow is a fully-assembled 9-column output tuple produced by the
// postings+streams join. Field order matches the output schema; the
// __streamLabelNames__ column is left implicit (always null).
type pointerJoinRow struct {
	objectPath       string
	sectionIndex     int64
	streamID         int64
	streamIDRef      int64
	minTimestamp     int64 // ns since epoch
	maxTimestamp     int64 // ns since epoch
	rowCount         int64
	uncompressedSize int64
}

// collectPostingsRows reads the postings section projecting
// (kind, object_path, section_index, stream_id_bitmap) with a kind=KindLabel
// EqualPredicate pushdown, decodes each row's stream_id_bitmap (an LSB byte
// bitmap, NOT a roaring bitmap — see pkg/memory.Bitmap and
// pkg/dataobj/sections/postings/label_aggregator.go), and joins each set bit
// against streamMeta to produce 9-column output tuples.
func (r *Reader) collectPostingsRows(
	ctx context.Context,
	streamMeta map[int64]streamRowMetadata,
) ([]pointerJoinRow, error) {
	postingsCols, err := findColumnsByType(r.opts.Columns,
		ColumnTypeKind,
		ColumnTypeObjectPath,
		ColumnTypeSectionIndex,
		ColumnTypeStreamIDBitmap,
	)
	if err != nil {
		return nil, fmt.Errorf("finding postings columns for ReadPointers: %w", err)
	}
	colKind, colObjectPath, colSectionIndex, colStreamIDBitmap := postingsCols[0], postingsCols[1], postingsCols[2], postingsCols[3]

	innerSection := colKind.Section.inner
	innerColumns := []*columnar.Column{colKind.inner, colObjectPath.inner, colSectionIndex.inner, colStreamIDBitmap.inner}

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
		return nil, fmt.Errorf("opening postings adapter for ReadPointers: %w", err)
	}

	// Schema for the inner postings projection (used by arrowconv.ToRecordBatch
	// to convert columnar batches into arrow batches).
	innerSchema := arrow.NewSchema([]arrow.Field{
		columnToField(colKind),
		columnToField(colObjectPath),
		columnToField(colSectionIndex),
		columnToField(colStreamIDBitmap),
	}, nil)

	var out []pointerJoinRow

	for {
		colBatch, readErr := postingsAdapter.Read(ctx, r.alloc, readPointersBatchSize)
		if colBatch != nil && colBatch.NumRows() > 0 {
			rb, err := arrowconv.ToRecordBatch(colBatch, innerSchema)
			if err != nil {
				return nil, fmt.Errorf("converting postings columnar batch to arrow: %w", err)
			}
			if err := appendPostingsJoinRows(rb, streamMeta, &out); err != nil {
				return nil, err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("reading postings batch for ReadPointers: %w", readErr)
		}
	}

	return out, nil
}

// appendPostingsJoinRows iterates rb's (kind, object_path, section_index,
// stream_id_bitmap) rows, decodes each LSB bitmap, and emits a join tuple
// for every (object_path, section_index, stream_id) where stream_id is in
// streamMeta. Rows with a null bitmap or null section_index/object_path are
// skipped — they cannot participate in the join.
func appendPostingsJoinRows(rb arrow.RecordBatch, streamMeta map[int64]streamRowMetadata, out *[]pointerJoinRow) error {
	if rb.NumRows() == 0 {
		return nil
	}
	// Postings projection: column 0 = kind (filtered by predicate), 1 =
	// object_path (utf8), 2 = section_index (int64), 3 = stream_id_bitmap
	// (binary).
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

	for i := 0; i < int(rb.NumRows()); i++ {
		if objectPathCol.IsNull(i) || sectionIndexCol.IsNull(i) || bitmapCol.IsNull(i) {
			continue
		}
		objectPath := objectPathCol.Value(i)
		sectionIndex := sectionIndexCol.Value(i)
		bitmapBytes := bitmapCol.Value(i)

		// Iterate the LSB bitmap (each byte holds 8 stream IDs starting
		// from streamID = byteIdx*8 + bitPos). See
		// pkg/dataobj/sections/postings/builder_test.go:21-28 (checkBit
		// reference) and pkg/memory/bitmap.go.
		for byteIdx, b := range bitmapBytes {
			if b == 0 {
				continue
			}
			for bitPos := 0; bitPos < 8; bitPos++ {
				if (b>>uint(bitPos))&1 == 0 {
					continue
				}
				streamID := int64(byteIdx*8 + bitPos)
				meta, ok := streamMeta[streamID]
				if !ok {
					continue
				}
				*out = append(*out, pointerJoinRow{
					objectPath:   objectPath,
					sectionIndex: sectionIndex,
					streamID:     streamID,
					// streamIDRef: in new-format index objects the postings
					// section does not carry a distinct stream_id_ref
					// (legacy cross-index concept). For new-format the
					// stream's identity within the source object IS its
					// stream_id, so streamIDRef == streamID. This preserves
					// schema-compatibility schema parity while emitting a meaningful value.
					streamIDRef:      streamID,
					minTimestamp:     meta.minTimestamp,
					maxTimestamp:     meta.maxTimestamp,
					rowCount:         meta.rowCount,
					uncompressedSize: meta.uncompressedSize,
				})
			}
		}
	}
	return nil
}

// buildReadPointersRecord builds a 9-column arrow.RecordBatch (plus the
// internal __streamLabelNames__ field, kept null) from the assembled join
// rows. The output schema's field order matches readPointersOutputSchema().
func buildReadPointersRecord(alloc memory.Allocator, schema *arrow.Schema, rows []pointerJoinRow) (arrow.RecordBatch, error) {
	rb := array.NewRecordBuilder(alloc, schema)

	for _, row := range rows {
		rb.Field(0).(*array.StringBuilder).Append(row.objectPath)
		rb.Field(1).(*array.Int64Builder).Append(row.sectionIndex)
		rb.Field(2).(*array.Int64Builder).Append(int64(pointers.PointerKindStreamIndex))
		rb.Field(3).(*array.Int64Builder).Append(row.streamID)
		rb.Field(4).(*array.Int64Builder).Append(row.streamIDRef)
		rb.Field(5).(*array.TimestampBuilder).Append(arrow.Timestamp(row.minTimestamp))
		rb.Field(6).(*array.TimestampBuilder).Append(arrow.Timestamp(row.maxTimestamp))
		rb.Field(7).(*array.Int64Builder).Append(row.rowCount)
		rb.Field(8).(*array.Int64Builder).Append(row.uncompressedSize)
		// __streamLabelNames__ field — append null; populated by
		// upstream label-resolution decorators ( concern).
		rb.Field(9).(*array.StringBuilder).AppendNull()
	}

	return rb.NewRecordBatch(), nil
}

// buildEmptyRecord produces a zero-row arrow.RecordBatch for schema. Used
// by ReadPointers when the join produces no rows — guarantees the returned
// batch's Schema is byte-for-byte equal to the populated case (schema-compatibility
// invariant).
func buildEmptyRecord(alloc memory.Allocator, schema *arrow.Schema) arrow.RecordBatch {
	rb := array.NewRecordBuilder(alloc, schema)
	return rb.NewRecordBatch()
}

// readBloomRowsBatchSize is the inner read size used by [Reader.ReadBloomRows]
// when draining the columnar adapter. Mirrors readPointersBatchSize (4096) —
// keeps per-batch allocations bounded while amortising fixed costs.
const readBloomRowsBatchSize = 4096

// ReadBloomRows returns an Arrow RecordBatch projecting
// (object_path, section_index, column_name, bloom_filter) for rows whose
// kind=KindBloom. Use [MatchSections] to test bloom-filter membership against
// label matchers — bloom membership testing is intentionally a helper, not a
// [Predicate], because page-level stats cannot skip rows that require a
// bytes-deserialize + bloom.TestString.
//
// The 4-column output projection deliberately excludes the
// ColumnTypeStreamIDBitmap column — bitmap-based stream filtering is a
// future optimization. The kind column is used for predicate-pushdown only
// and is NOT part of
// the output schema.
//
// Column requirement: ReadBloomRows projects ObjectPath, SectionIndex,
// ColumnName, BloomFilter, AND Kind from [Reader.opts.Columns]. All five
// MUST be present — a narrower [ReaderOptions.Columns] projection that
// omits any of them yields "finding bloom-row columns: ... not found" at
// call time. Callers that construct [ReaderOptions] manually should pass
// the section's full column set ([Section.Columns]); this is the same
// shape the fixture tests use.
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
	// Doing two separate findColumnsByType calls keeps the helper simple and
	// makes the predicate-vs-output role explicit; this is the same pattern
	// used by collectPostingsRows for ReadPointers (kind is filtered, not
	// projected).
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

	// Build the inner 5-column projection: [output cols..., kind]. The kind
	// column is needed by the predicate (kind = KindBloom pushdown) but is
	// stripped from the returned arrow.RecordBatch before the caller sees it.
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

	// Inner schema (5 fields) used by arrowconv.ToRecordBatch — must match
	// the columnar batch column count.
	innerSchema := arrow.NewSchema([]arrow.Field{
		columnToField(outputCols[0]),
		columnToField(outputCols[1]),
		columnToField(outputCols[2]),
		columnToField(outputCols[3]),
		columnToField(colKind),
	}, nil)

	// Output schema (4 fields) — the kind column is stripped so the helper's
	// projection contract (positions 0-3 = path, section, column_name, bloom)
	// is honored.
	outputSchema := columnsSchema(outputCols)

	// single-batch decision (mirrors ReadPointers): drain the inner
	// adapter and accumulate the result as one arrow.RecordBatch. The loop
	// shape leaves room for a future streaming variant without changing the
	// caller contract.
	collected, err := r.collectBloomRowBatches(ctx, adapter, alloc, innerSchema, outputSchema)
	if err != nil {
		return nil, err
	}

	xcap.RegionFromContext(ctx).Record(xcap.StatPostingsBloomRowsRead.Observe(collected.NumRows()))
	return collected, nil
}

// collectBloomRowBatches drains adapter, converts each inner columnar batch
// to a 5-column arrow batch via arrowconv.ToRecordBatch, and copies the four
// kept columns (object_path, section_index, column_name, bloom_filter) into a
// single output batch matching outputSchema, dropping the trailing kind column.
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

// appendBloomRowBatch copies the 4 output columns (object_path, section_index,
// column_name, bloom_filter) from innerRB into rb. The 5th inner column (kind)
// is intentionally dropped — it served the predicate only.
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

// readResolveLabelsBatchSize is the inner read size used by
// [Reader.ResolveLabels] when draining the columnar adapter. Mirrors
// readPointersBatchSize / readBloomRowsBatchSize (4096) — keeps per-batch
// allocations bounded while amortising fixed costs.
const readResolveLabelsBatchSize = 4096

// matcherIndex identifies a single label matcher by its position in the
// caller-supplied matchers slice. It is used inside ResolveLabels to
// accumulate per-matcher stream-id sets so the AND-across-matchers
// intersection can be computed after the row read.
//
// Indexing on position (rather than (name, value) keys as in the
// pre- design) supports every MatchType uniformly:
// - Equal matchers no longer collide if two distinct matchers happen
// to share (name, value); each gets its own set.
// - Regex / NotEqual / NotRegex matchers cannot be keyed on
// (name, value) at all (value is a pattern), so position is the
// only stable key.
type matcherIndex int

// ResolveLabels resolves the conjunction of label matchers against KindLabel
// rows in the postings section. Returns the set of stream IDs matching all
// matchers (intersection across matchers) and a map from each matching
// stream ID to the label-column names that contributed it.
//
// Predicate pushdown (per , post- + fix):
//
// - Equal matchers are pushed down as EqualPredicate(column_name=Name)
// AND EqualPredicate(label_value=Value) pairs combined via OrPredicate
// — a row matches one such pair only when BOTH columns equal.
// - Regex / NotEqual / NotRegex matchers are pushed down ONLY on
// column_name (their Name). This ensures rows of every matcher's
// targeted column are READ; the value-side filter is then re-applied
// row-side via labels.Matcher.Matches (mirroring today's
// streams.Reader behaviour at index_sections_reader.go:454-521).
// - When an Equal matcher and a non-Equal matcher share a Name, the
// broader column_name=Name pushdown supersedes the narrow Equal
// AND-pair; the Equal value check is reapplied row-side. This avoids
// filtering out rows that the non-Equal matcher might match in that
// column.
//
// AND across matchers is enforced in Go after the row read via
// per-matcher stream-id set intersection. Both Equal and non-Equal
// matchers participate uniformly in the per-matcher accumulation — the
// fix replaces the pre-existing regex-only UNION map with a
// per-matcher set so multi-regex queries return the intersection.
//
// Pre-fix bugs (, ) and their resolutions:
//
// - : predicate pushdown previously included only Equal-matcher
// Names. A mixed query like {env="prod", app=~"foo.*"} read only
// env-column rows, so the app regex never saw any rows and was
// silently dropped. Fix: pushdown now includes column_name=Name
// for every non-Equal matcher's Name.
// - : regex-only resolution previously accumulated a single
// UNION map across all non-Equal matchers, returning streams that
// satisfied AT LEAST ONE regex. Fix: per-matcher accumulation +
// post-scan intersection produces the documented AND.
//
// Empty matcher slice returns (nil, nil, nil) — no resolution to perform.
// Caller must not retain the returned maps beyond the lifetime of the
// Reader's allocator; the maps are independent Go allocations safe to
// outlive the underlying arrow batches.
//
// : ResolveLabels is purely additive in the postings package. The
// streams section in new-format objects continues to exist; ResolveLabels
// does NOT read or modify it.
//
// Column requirement: ResolveLabels projects ColumnName, LabelValue,
// StreamIDBitmap, AND Kind from [Reader.opts.Columns]. All four MUST be
// present — a narrower [ReaderOptions.Columns] projection that omits any
// of them yields "finding ResolveLabels columns: ... not found" at call
// time. Callers that construct [ReaderOptions] manually should pass the
// section's full column set ([Section.Columns]); this is the same shape
// the fixture tests use.
// todo (gsd): clean up the comment before PR
func (r *Reader) ResolveLabels(ctx context.Context, matchers []*labels.Matcher) (map[int64]struct{}, map[int64][]string, error) {
	if !r.ready {
		return nil, nil, errReaderNotOpen
	}

	ctx, span := xcap.StartSpan(ctx, tracer, "postings.Reader.ResolveLabels")
	defer span.End()

	defer r.alloc.Reclaim()

	// Short-circuit: empty matcher list means there is nothing to resolve.
	// Returning nil maps here (rather than empty allocated maps) lets the
	// caller distinguish "no matchers asked" from "matchers asked but
	// nothing matched". The caller should not pretend to filter when no
	// matchers were supplied — that is a programming error to fall
	// through to.
	if len(matchers) == 0 {
		return nil, nil, nil
	}

	// Split matchers into Equal (predicate-pushdown) and other (Go-side
	// row filter). The same shape today's streams-section path uses (per
	// ).
	equalMatchers, otherMatchers := splitByMatchType(matchers)

	// Project the 4 output columns + kind (predicate-only). The kind
	// column gates the read to KindLabel rows; (column_name, label_value)
	// + stream_id_bitmap drive matching and inversion.
	outputCols, err := findColumnsByType(r.opts.Columns,
		ColumnTypeColumnName,
		ColumnTypeLabelValue,
		ColumnTypeStreamIDBitmap,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("finding ResolveLabels columns: %w", err)
	}
	kindCols, err := findColumnsByType(r.opts.Columns, ColumnTypeKind)
	if err != nil {
		return nil, nil, fmt.Errorf("finding ResolveLabels columns: %w", err)
	}
	colColumnName, colLabelValue, colStreamIDBitmap := outputCols[0], outputCols[1], outputCols[2]
	colKind := kindCols[0]

	// Build the inner 4-column projection: [column_name, label_value,
	// stream_id_bitmap, kind]. The kind column is predicate-only; we read
	// it anyway because the dataset layer requires every column referenced
	// by a predicate to be in the projection.
	innerSection := outputCols[0].Section.inner
	innerColumns := []*columnar.Column{
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
		colColumnName:     dset.Columns()[0],
		colLabelValue:     dset.Columns()[1],
		colStreamIDBitmap: dset.Columns()[2],
		colKind:           dset.Columns()[3],
	}

	// Build the predicate (per , post- fix):
	// AND(
	// kind == KindLabel,
	// OR(
	// AND(column_name=N1, label_value=V1), // Equal matcher 1
	// AND(column_name=N2, label_value=V2), // Equal matcher 2
	// column_name=Nx, // non-Equal matcher whose
	// column_name=Ny, // Name is NOT covered
	// ... // by an Equal pair
	// ),
	// )
	// Equal matchers push down as (column_name, label_value) pairs — a row
	// matches one such pair only when both columns equal. Regex / NotEqual
	// / NotRegex matchers push down ONLY on column_name (their Name) so
	// every row of the targeted column is read; the row-level Go
	// evaluation in accumulateLabelRows then applies Matches(label_value)
	// for the value-side filter. This guarantees the row read covers
	// EVERY matcher's column_name — the bug was that non-Equal
	// matchers whose Name did not overlap an Equal matcher's Name had
	// their rows silently filtered out by the predicate.
	//
	// To avoid emitting redundant column_name=N branches when an Equal
	// matcher and a non-Equal matcher share the same Name, we only add
	// a column_name=Nx branch for non-Equal matcher Names NOT already
	// covered by an Equal matcher: the Equal AND(name,value) pair is
	// strictly narrower than column_name=name (it filters by both
	// columns), so the predicate must include the broader column_name=N
	// branch to admit rows the non-Equal matcher might match in that
	// column. We achieve this by dropping the Equal-AND-pair for such
	// shared-name groups and replacing it with the broader column_name=N
	// branch — the Equal matcher's value check is then re-applied
	// row-side. (Pushing both would be sound but redundant; pushing only
	// the AND pair would re-introduce for the value mismatch
	// rows.)
	kindEq := EqualPredicate{
		Column: colKind,
		Value:  scalar.NewInt64Scalar(int64(KindLabel)),
	}
	var builtPredicate Predicate = kindEq
	if len(matchers) > 0 {
		// Set of Names covered by a non-Equal matcher — these names must
		// surface as a broad column_name=Name predicate so the row-level
		// regex/NotEqual fallback sees every row in that column.
		nonEqualNames := make(map[string]struct{})
		for _, m := range otherMatchers {
			nonEqualNames[m.Name] = struct{}{}
		}

		// dedupedBroadNames: per non-Equal name we emit at most ONE
		// column_name=Name branch even if multiple non-Equal matchers
		// share that Name. addedBroad tracks which Names already have a
		// branch.
		addedBroad := make(map[string]struct{})

		var branches []Predicate
		// Branch (a): Equal matchers whose Name is NOT covered by any
		// non-Equal matcher get the precise AND(name, value) pushdown.
		// This is strictly narrower than column_name=name, which is the
		// best we can do for Equal-only Names.
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
		// Branch (b): one column_name=Name per Name targeted by a
		// non-Equal matcher (deduped). If an Equal matcher also targets
		// that Name, this broad branch supersedes its narrow AND pair
		// (we already skipped the Equal pair above) — the Equal value
		// check is reapplied row-side via the equalsByName map.
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
		return nil, nil, fmt.Errorf("opening ResolveLabels adapter: %w", err)
	}

	// Inner schema (4 fields) used by arrowconv.ToRecordBatch — must match
	// the columnar batch column count.
	innerSchema := arrow.NewSchema([]arrow.Field{
		columnToField(colColumnName),
		columnToField(colLabelValue),
		columnToField(colStreamIDBitmap),
		columnToField(colKind),
	}, nil)

	// perMatcherStreams[i] -> set of stream IDs that satisfied matchers[i]
	// across all KindLabel rows seen, where i is the position of the
	// matcher in the (filtered) matchers slice. We key on position rather
	// than (name, value) so non-Equal matchers (whose value is a pattern)
	// participate in the same per-matcher accumulation as Equal matchers
	// — the fix: regex-only multi-matcher resolution now intersects
	// per-matcher sets instead of unioning them.
	perMatcherStreams := make(map[matcherIndex]map[int64]struct{})
	// labelNamesByStream tracks the column_name of each row a stream
	// appeared in. We store as map[int64]map[string]struct{} during the
	// scan for O(1) dedup, then flatten to map[int64][]string at return
	// time. (Plan note: documented in SUMMARY.)
	labelNamesByStreamSet := make(map[int64]map[string]struct{})

	// activeMatchers is the (nil-filtered) matchers slice in the same
	// order the caller supplied — perMatcherStreams keys are indexes
	// into this slice.
	activeMatchers := make([]*labels.Matcher, 0, len(matchers))
	for _, m := range matchers {
		if m == nil {
			continue
		}
		activeMatchers = append(activeMatchers, m)
	}

	// Drain the adapter and accumulate per-matcher stream sets. Each row
	// carries one (column_name, label_value) tuple; for each matcher
	// whose Name == row.column_name AND whose Matches(label_value)
	// returns true, the row's stream IDs are added to that matcher's
	// set. The post-scan AND intersection (below) then produces the
	// conjunction.
	for {
		colBatch, readErr := adapter.Read(ctx, r.alloc, readResolveLabelsBatchSize)
		if colBatch != nil && colBatch.NumRows() > 0 {
			innerRB, convErr := arrowconv.ToRecordBatch(colBatch, innerSchema)
			if convErr != nil {
				return nil, nil, fmt.Errorf("converting ResolveLabels columnar batch to arrow: %w", convErr)
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
			return nil, nil, fmt.Errorf("reading ResolveLabels batch: %w", readErr)
		}
	}

	// AND-across-all-matchers in Go: a stream id must be present in
	// EVERY matcher's set to satisfy the conjunction. This is the 	// fix — regex-only mode previously returned the UNION of per-matcher
	// sets; the function's contract is the intersection.
	//
	// matchingStreamIDs is always freshly allocated ( fix) — the
	// caller owns the returned map.
	var matchingStreamIDs map[int64]struct{}
	if len(activeMatchers) == 0 {
		// Defensive: short-circuited at the top of ResolveLabels, but if
		// every matcher was nil we land here.
		matchingStreamIDs = make(map[int64]struct{})
	} else {
		first := perMatcherStreams[matcherIndex(0)]
		matchingStreamIDs = make(map[int64]struct{}, len(first))
		for id := range first {
			matchingStreamIDs[id] = struct{}{}
		}
		for i := 1; i < len(activeMatchers) && len(matchingStreamIDs) > 0; i++ {
			next := perMatcherStreams[matcherIndex(i)]
			for id := range matchingStreamIDs {
				if _, ok := next[id]; !ok {
					delete(matchingStreamIDs, id)
				}
			}
		}
	}

	// Flatten labelNamesByStream from set-of-set to set-of-slice, scoped
	// to streams in matchingStreamIDs. Streams that contributed to the
	// per-matcher sets but were filtered out by the intersection above
	// are NOT included in the returned labelNamesByStream — only streams
	// that actually appear in matchingStreamIDs.
	var labelNamesByStream map[int64][]string
	if len(matchingStreamIDs) > 0 {
		labelNamesByStream = make(map[int64][]string, len(matchingStreamIDs))
		for id := range matchingStreamIDs {
			nameSet := labelNamesByStreamSet[id]
			if len(nameSet) == 0 {
				continue
			}
			names := make([]string, 0, len(nameSet))
			for n := range nameSet {
				names = append(names, n)
			}
			labelNamesByStream[id] = names
		}
	}

	xcap.RegionFromContext(ctx).Record(xcap.StatPostingsLabelsResolved.Observe(int64(len(matchingStreamIDs))))
	return matchingStreamIDs, labelNamesByStream, nil
}

// splitByMatchType partitions matchers into Equal (predicate-pushdown
// eligible) and other (NotEqual / Regex / NotRegex — Go-side row filter).
// Nil matchers in the input are skipped.
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

// accumulateLabelRows iterates a 4-column inner arrow batch
// (column_name, label_value, stream_id_bitmap, kind) and, for each row,
// evaluates every matcher whose Name targets the row's column_name. The
// row's stream IDs are added to perMatcherStreams[i] for every matcher
// matchers[i] satisfied by the row's (column_name, label_value) pair,
// and labelNamesByStreamSet[streamID] gains the row's column_name for
// every stream that contributed to AT LEAST ONE matcher's set on this
// row (the final flatten step scopes the inversion result to the
// AND-intersected matchingStreamIDs).
//
// Per-matcher evaluation ( + fix):
// - Equal matcher: contributes if column_name == matcher.Name AND
// label_value == matcher.Value.
// - Regex / NotEqual / NotRegex matcher: contributes if
// column_name == matcher.Name AND matcher.Matches(label_value).
//
// Pre-, regex matchers were applied as row-level rejection filters
// (AND on same Name) and only contributed to a single regexUnionStreams
// map — the bug was that regex matchers whose Name did not
// overlap an Equal matcher's Name never had their rows read because the
// predicate pushdown only included Equal-matcher Names. The bug
// was that the regexUnionStreams was a single UNION map across all
// regex matchers, not a per-matcher set, so multi-regex queries
// returned the union instead of the AND.
//
// The new per-matcher accumulation handles every matcher uniformly:
// the caller-side AND intersection (in ResolveLabels) now produces the
// correct conjunction for both single-matcher-type and mixed queries.
//
// The stream_id_bitmap is an LSB byte bitmap from pkg/memory.Bitmap
// (NOT a roaring bitmap — see encode_columnar.go and label_aggregator.go).
// Each byte holds 8 stream IDs: bit position `p` of byte `b` represents
// stream id `b*8 + p`.
func accumulateLabelRows(
	rb arrow.RecordBatch,
	matchers []*labels.Matcher,
	perMatcherStreams map[matcherIndex]map[int64]struct{},
	labelNamesByStreamSet map[int64]map[string]struct{},
) error {
	if rb.NumRows() == 0 {
		return nil
	}
	columnNameCol, ok := rb.Column(0).(*array.String)
	if !ok {
		return fmt.Errorf("ResolveLabels column_name has unexpected type %T", rb.Column(0))
	}
	labelValueCol, ok := rb.Column(1).(*array.String)
	if !ok {
		return fmt.Errorf("ResolveLabels label_value has unexpected type %T", rb.Column(1))
	}
	bitmapCol, ok := rb.Column(2).(*array.Binary)
	if !ok {
		return fmt.Errorf("ResolveLabels stream_id_bitmap has unexpected type %T", rb.Column(2))
	}

	// Group matchers by Name for O(1) per-row dispatch. Each entry maps
	// a Name to the indexes of matchers in `matchers` that target that
	// Name; the per-row Matches check only runs on those matchers.
	matchersByName := make(map[string][]matcherIndex, len(matchers))
	for i, m := range matchers {
		matchersByName[m.Name] = append(matchersByName[m.Name], matcherIndex(i))
	}

	for i := 0; i < int(rb.NumRows()); i++ {
		if columnNameCol.IsNull(i) || labelValueCol.IsNull(i) || bitmapCol.IsNull(i) {
			continue
		}
		name := columnNameCol.Value(i)
		value := labelValueCol.Value(i)
		bitmapBytes := bitmapCol.Value(i)

		// Which matchers (if any) does this row satisfy? A matcher
		// targeting this row's column contributes iff Matches(value)
		// returns true. labels.Matcher.Matches subsumes Equal (exact
		// string), Regex (compiled regex), NotEqual, and NotRegex
		// uniformly — so we no longer special-case Equal vs other.
		applicable := matchersByName[name]
		if len(applicable) == 0 {
			// Row in a column no matcher targets — predicate pushdown
			// should have filtered it; defensive skip.
			continue
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

		// Decode the LSB byte bitmap once and iterate its set bits to
		// produce the per-stream-id contribution.
		for byteIdx, b := range bitmapBytes {
			if b == 0 {
				continue
			}
			for bitPos := 0; bitPos < 8; bitPos++ {
				if (b>>uint(bitPos))&1 == 0 {
					continue
				}
				streamID := int64(byteIdx*8 + bitPos)

				// Accumulate into every matcher's set the row satisfied.
				for _, idx := range matched {
					set, exists := perMatcherStreams[idx]
					if !exists {
						set = make(map[int64]struct{})
						perMatcherStreams[idx] = set
					}
					set[streamID] = struct{}{}
				}

				// Record the column_name for the inversion. We always
				// record (even for streams later filtered out by the
				// AND intersection) — the final flatten step scopes the
				// result to matchingStreamIDs.
				nameSet, exists := labelNamesByStreamSet[streamID]
				if !exists {
					nameSet = make(map[string]struct{})
					labelNamesByStreamSet[streamID] = nameSet
				}
				nameSet[name] = struct{}{}
			}
		}
	}
	return nil
}

// mapPredicates translates a slice of postings [Predicate] values into the
// equivalent slice of [dataset.Predicate] values, using columnLookup to
// resolve each [Column] to its corresponding [dataset.Column].
//
// For simplicity, [mapPredicate] and the functions it calls panic if they
// encounter an unsupported conversion. These should normally be handled by
// [ReaderOptions.Validate], but we catch any panics here to gracefully
// return an error to the caller instead of potentially crashing the
// goroutine.
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

// mapPredicate translates a single postings [Predicate] into the equivalent
// [dataset.Predicate]. mapPredicate panics on an unrecognized predicate type
// or an unsupported scalar conversion; [mapPredicates] recovers those panics
// into errors.
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

// mustConvertType returns the [datasetmd.PhysicalType] corresponding to
// arrowType. mustConvertType panics if arrowType has no supported mapping;
// callers wrap mustConvertType under [mapPredicates] which recovers the
// panic into an error.
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

// StreamLabelColumnNames returns the names of the stream-label columns
// present in the sibling streams.Section discovered at Open time. The
// returned slice is the de-duplicated list of [streams.ColumnTypeLabel]
// column names — i.e. every label name that the streams section materializes
// as its own column.
//
// Callers (notably metastore.indexSectionsReader.filterBloomPredicates) use
// this to decide which user-supplied predicates name stream labels and
// should therefore NOT be evaluated against the bloom rows. Without this,
// predicates that target a stream label not also covered by a matcher would
// be left in the bloom-row predicate set, where they search for a
// column_name bloom row that does not exist and AND-drop every section
// (the false-negative).
//
// Returns nil when the postings section was opened without a parent dataobj
// back-pointer (legacy [Open], used by fixtures that don't carry a streams
// sibling). The Reader must be opened ([Reader.Open]) before this method
// is called.
func (r *Reader) StreamLabelColumnNames() []string {
	if !r.ready || r.streamsSec == nil {
		return nil
	}
	cols := r.streamsSec.Columns()
	names := make([]string, 0, len(cols))
	seen := make(map[string]struct{}, len(cols))
	for _, c := range cols {
		if c == nil || c.Type != streams.ColumnTypeLabel || c.Name == "" {
			continue
		}
		if _, dup := seen[c.Name]; dup {
			continue
		}
		seen[c.Name] = struct{}{}
		names = append(names, c.Name)
	}
	return names
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
