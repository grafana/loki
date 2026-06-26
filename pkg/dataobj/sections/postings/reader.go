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
	memoryv2 "github.com/grafana/loki/v3/pkg/memory"
	"github.com/grafana/loki/v3/pkg/xcap"
)

var tracer = otel.Tracer("pkg/dataobj/sections/postings")

// RegionPrefix is the prefix used for postings reader xcap regions.
var RegionPrefix = "postings.Reader."

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
		if s == nil {
			errs = append(errs, fmt.Errorf("scalar is nil"))
			return
		}
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
		ctx, r.readSpan = xcap.StartSpan(ctx, tracer, RegionPrefix+"Read")
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

	ctx, span := xcap.StartSpan(ctx, tracer, RegionPrefix+"Open")
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

// PointerRow is one deduplicated (object, section, stream) tuple produced by the
// label scan ([Reader.ScanLabelsInto] then [StreamScan.Finalize]), carrying the
// time bounds merged across that stream's label postings.
type PointerRow struct {
	ObjectPath   string
	SectionIndex int64
	StreamID     int64
	MinTimestamp int64
	MaxTimestamp int64
	HasBounds    bool
}

type pointerRowKey struct {
	objectPath   string
	sectionIndex int64
	streamID     int64
}

func mergePointerRowBounds(row *PointerRow, minTs, maxTs int64, hasBounds bool) {
	if !hasBounds {
		return
	}
	if !row.HasBounds {
		row.MinTimestamp = minTs
		row.MaxTimestamp = maxTs
		row.HasBounds = true
		return
	}
	if minTs < row.MinTimestamp {
		row.MinTimestamp = minTs
	}
	if maxTs > row.MaxTimestamp {
		row.MaxTimestamp = maxTs
	}
}

// readBloomRowsBatchSize is the inner read size used by [Reader.ReadBloomRows]
// when draining the columnar adapter.
const readBloomRowsBatchSize = 4096

// ReadBloomRows returns arrow RecordBatch for all KindBloom rows.
func (r *Reader) ReadBloomRows(ctx context.Context) (arrow.RecordBatch, error) {
	return r.readBloomRows(ctx, nil)
}

// ReadBloomRowsForColumns returns KindBloom rows limited to the provided
// column names. Empty names are ignored; an empty resulting set is equivalent to
// [ReadBloomRows].
func (r *Reader) ReadBloomRowsForColumns(ctx context.Context, columnNames []string) (arrow.RecordBatch, error) {
	return r.readBloomRows(ctx, columnNames)
}

func (r *Reader) readBloomRows(ctx context.Context, columnNames []string) (arrow.RecordBatch, error) {
	if !r.ready {
		return nil, errReaderNotOpen
	}

	ctx, span := xcap.StartSpan(ctx, tracer, RegionPrefix+"ReadBloomRows")
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

	predicateExprs := buildBloomScanPredicates(colKind, outputCols[2], columnNames)
	preds, err := mapPredicates(predicateExprs, columnLookup)
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

	xcap.RegionFromContext(ctx).Record(StatPostingsBloomRowsRead.Observe(collected.NumRows()))
	return collected, nil
}

func buildBloomScanPredicates(colKind, colColumnName *Column, columnNames []string) []Predicate {
	predicateExprs := []Predicate{
		EqualPredicate{
			Column: colKind,
			Value:  scalar.NewInt64Scalar(int64(KindBloom)),
		},
	}

	// Optional column-name pushdown to avoid scanning unrelated bloom rows.
	if len(columnNames) == 0 {
		return predicateExprs
	}

	uniq := make(map[string]struct{}, len(columnNames))
	values := make([]scalar.Scalar, 0, len(columnNames))
	for _, name := range columnNames {
		if name == "" {
			continue
		}
		if _, exists := uniq[name]; exists {
			continue
		}
		uniq[name] = struct{}{}
		values = append(values, scalar.NewStringScalar(name))
	}
	if len(values) == 0 {
		return predicateExprs
	}
	if len(values) == 1 {
		predicateExprs = append(predicateExprs, EqualPredicate{
			Column: colColumnName,
			Value:  values[0],
		})
		return predicateExprs
	}

	predicateExprs = append(predicateExprs, InPredicate{
		Column: colColumnName,
		Values: values,
	})
	return predicateExprs
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

// streamScanBatchSize bounds per-batch allocations while amortising fixed costs when draining the scan adapter.
const streamScanBatchSize = 4096

type matcherIndex int

// StreamScanResult carries everything produced by a single pass over KindLabel rows:
type StreamScanResult struct {
	MatchingStreamRefs map[StreamRef]struct{}
	LabelColumnNames   []string
	// MatchingLabelColumnNames is the distinct set of label names observed on
	// the matched streams only.
	MatchingLabelColumnNames []string
	Pointers                 []PointerRow
}

// StreamScan holds the cross-section accumulators for one logical pass over KindLabel rows.
type StreamScan struct {
	activeMatchers     []*labels.Matcher
	perMatcherStreams  map[matcherIndex]map[StreamRef]struct{}
	perMatcherHasName  map[matcherIndex]map[StreamRef]struct{}
	matchersByName     map[string][]matcherIndex
	missingMatches     []bool
	allStreams         map[StreamRef]struct{}
	allColumnNames     map[string]struct{}
	labelNamesByStream map[StreamRef]map[string]struct{}
	pointerRows        []PointerRow
	seen               map[pointerRowKey]int
	startNanos         int64
	endNanos           int64
}

// NewStreamScan returns an accumulator for resolving streams and pointers across
// one or more postings sections. Feed each section's KindLabel rows with
// [Reader.ScanLabelsInto], then call [StreamScan.Finalize] once.
func NewStreamScan(matchers []*labels.Matcher, start, end time.Time) *StreamScan {
	active := make([]*labels.Matcher, 0, len(matchers))
	for _, m := range matchers {
		if m == nil {
			continue
		}
		active = append(active, m)
	}

	matchersByName := make(map[string][]matcherIndex, len(active))
	perMatcherStreams := make(map[matcherIndex]map[StreamRef]struct{}, len(active))
	perMatcherHasName := make(map[matcherIndex]map[StreamRef]struct{}, len(active))
	missingMatches := make([]bool, len(active))
	for i, m := range active {
		idx := matcherIndex(i)
		matchersByName[m.Name] = append(matchersByName[m.Name], idx)
		perMatcherStreams[idx] = make(map[StreamRef]struct{})
		perMatcherHasName[idx] = make(map[StreamRef]struct{})
		missingMatches[i] = matcherMatchesMissingLabel(m)
	}

	return &StreamScan{
		activeMatchers:     active,
		perMatcherStreams:  perMatcherStreams,
		perMatcherHasName:  perMatcherHasName,
		matchersByName:     matchersByName,
		missingMatches:     missingMatches,
		allStreams:         make(map[StreamRef]struct{}),
		allColumnNames:     make(map[string]struct{}),
		labelNamesByStream: make(map[StreamRef]map[string]struct{}),
		seen:               make(map[pointerRowKey]int),
		startNanos:         start.UnixNano(),
		endNanos:           end.UnixNano(),
	}
}

func matcherMatchesMissingLabel(m *labels.Matcher) bool {
	switch m.Type {
	case labels.MatchEqual:
		return m.Value == ""
	case labels.MatchNotEqual:
		return m.Value != ""
	case labels.MatchRegexp, labels.MatchNotRegexp:
		return m.Matches("")
	default:
		return false
	}
}

// ScanLabelsInto drains this reader's KindLabel rows in batchSize-row batches into acc.
// ScanLabelsInto scans this section's KindLabel rows into acc. It returns
// whether the section was productive (yielded at least one matching stream) so
// the metastore layer can record the metastore.sections.productive stat without
// the postings package depending on the metastore package.
func (r *Reader) ScanLabelsInto(ctx context.Context, acc *StreamScan, batchSize int) (bool, error) {
	if !r.ready {
		return false, errReaderNotOpen
	}
	if batchSize <= 0 {
		batchSize = streamScanBatchSize
	}

	ctx, span := xcap.StartSpan(ctx, tracer, RegionPrefix+"ScanLabels")
	defer span.End()
	defer r.alloc.Reclaim()

	cols, err := findColumnsByType(r.opts.Columns,
		ColumnTypeObjectPath,
		ColumnTypeSectionIndex,
		ColumnTypeColumnName,
		ColumnTypeLabelValue,
		ColumnTypeStreamIDBitmap,
		ColumnTypeMinTimestamp,
		ColumnTypeMaxTimestamp,
		ColumnTypeKind,
	)
	if err != nil {
		return false, fmt.Errorf("finding ScanLabels columns: %w", err)
	}
	colObjectPath, colSectionIndex, colColumnName, colLabelValue, colStreamIDBitmap, colMinTs, colMaxTs, colKind :=
		cols[0], cols[1], cols[2], cols[3], cols[4], cols[5], cols[6], cols[7]

	innerSection := cols[0].Section.inner
	innerColumns := []*columnar.Column{
		colObjectPath.inner,
		colSectionIndex.inner,
		colColumnName.inner,
		colLabelValue.inner,
		colStreamIDBitmap.inner,
		colMinTs.inner,
		colMaxTs.inner,
		colKind.inner,
	}

	dset, err := columnar.MakeDataset(innerSection, innerColumns)
	if err != nil {
		return false, fmt.Errorf("creating dataset: %w", err)
	} else if len(dset.Columns()) != len(innerColumns) {
		return false, fmt.Errorf("dataset has %d columns, expected %d", len(dset.Columns()), len(innerColumns))
	}

	columnLookup := map[*Column]dataset.Column{
		colObjectPath:     dset.Columns()[0],
		colSectionIndex:   dset.Columns()[1],
		colColumnName:     dset.Columns()[2],
		colLabelValue:     dset.Columns()[3],
		colStreamIDBitmap: dset.Columns()[4],
		colMinTs:          dset.Columns()[5],
		colMaxTs:          dset.Columns()[6],
		colKind:           dset.Columns()[7],
	}

	// Push only kind == KindLabel; the row loop does matcher eval, name
	// collection, and pointer-bounds accumulation row-side.
	kindEq := EqualPredicate{
		Column: colKind,
		Value:  scalar.NewInt64Scalar(int64(KindLabel)),
	}
	preds, err := mapPredicates([]Predicate{kindEq}, columnLookup)
	if err != nil {
		return false, fmt.Errorf("mapping predicates: %w", err)
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
		return false, fmt.Errorf("opening ScanLabels adapter: %w", err)
	}

	// Inner schema (8 fields) — must match the columnar batch column count/order.
	innerSchema := arrow.NewSchema([]arrow.Field{
		columnToField(colObjectPath),
		columnToField(colSectionIndex),
		columnToField(colColumnName),
		columnToField(colLabelValue),
		columnToField(colStreamIDBitmap),
		columnToField(colMinTs),
		columnToField(colMaxTs),
		columnToField(colKind),
	}, nil)

	var productive bool
	for {
		colBatch, readErr := adapter.Read(ctx, r.alloc, batchSize)
		if colBatch != nil && colBatch.NumRows() > 0 {
			innerRB, convErr := arrowconv.ToRecordBatch(colBatch, innerSchema)
			if convErr != nil {
				return false, fmt.Errorf("converting ScanLabels columnar batch to arrow: %w", convErr)
			}
			batchProductive, err := accumulateStreamScan(
				innerRB,
				acc,
			)
			if err != nil {
				return false, err
			}
			productive = productive || batchProductive
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return false, fmt.Errorf("reading ScanLabels batch: %w", readErr)
		}
	}

	return productive, nil
}

// Finalize ANDs the per-matcher stream sets and scopes the accumulated pointer
// rows to the surviving streams. It must be called once, after every section has
// been scanned into the StreamScan.
func (acc *StreamScan) Finalize(ctx context.Context) *StreamScanResult {
	acc.applyMissingLabelSemantics()
	matchingStreams := intersectMatcherStreams(acc.perMatcherStreams, len(acc.activeMatchers))

	labelNames := make([]string, 0, len(acc.allColumnNames))
	for name := range acc.allColumnNames {
		labelNames = append(labelNames, name)
	}

	matchingLabelNamesMap := make(map[string]struct{})
	for ref := range matchingStreams {
		names := acc.labelNamesByStream[ref]
		for name := range names {
			matchingLabelNamesMap[name] = struct{}{}
		}
	}
	matchingLabelNames := make([]string, 0, len(matchingLabelNamesMap))
	for name := range matchingLabelNamesMap {
		matchingLabelNames = append(matchingLabelNames, name)
	}

	// Scope the accumulated pointer rows (all streams) to the matching streams,
	// preserving first-seen order.
	var matchedRows []PointerRow
	for _, row := range acc.pointerRows {
		ref := StreamRef{ObjectPath: row.ObjectPath, StreamID: row.StreamID}
		if _, ok := matchingStreams[ref]; ok {
			matchedRows = append(matchedRows, row)
		}
	}

	xcap.RegionFromContext(ctx).Record(StatPostingsLabelsResolved.Observe(int64(len(matchingStreams))))
	xcap.RegionFromContext(ctx).Record(StatPostingsPointersRead.Observe(int64(len(matchedRows))))
	return &StreamScanResult{
		MatchingStreamRefs:       matchingStreams,
		LabelColumnNames:         labelNames,
		MatchingLabelColumnNames: matchingLabelNames,
		Pointers:                 matchedRows,
	}
}

func (acc *StreamScan) applyMissingLabelSemantics() {
	if len(acc.activeMatchers) == 0 || len(acc.allStreams) == 0 {
		return
	}

	for i := range acc.activeMatchers {
		if !acc.missingMatches[i] {
			continue
		}
		idx := matcherIndex(i)
		matched := acc.perMatcherStreams[idx]
		hasName := acc.perMatcherHasName[idx]
		for ref := range acc.allStreams {
			if _, has := hasName[ref]; has {
				continue
			}
			matched[ref] = struct{}{}
		}
	}
}

// intersectMatcherStreams ANDs the per-matcher stream sets: a stream survives
// only if it appears under every matcher.
func intersectMatcherStreams(perMatcherStreams map[matcherIndex]map[StreamRef]struct{}, activeMatchers int) map[StreamRef]struct{} {
	if activeMatchers == 0 {
		return make(map[StreamRef]struct{})
	}
	first := perMatcherStreams[matcherIndex(0)]
	matchingStreams := make(map[StreamRef]struct{}, len(first))
	for streamRef := range first {
		matchingStreams[streamRef] = struct{}{}
	}
	for i := 1; i < activeMatchers && len(matchingStreams) > 0; i++ {
		next := perMatcherStreams[matcherIndex(i)]
		for streamRef := range matchingStreams {
			if _, ok := next[streamRef]; !ok {
				delete(matchingStreams, streamRef)
			}
		}
	}
	return matchingStreams
}

// accumulateStreamScan processes one batch of the unified KindLabel scan, feeding
// three accumulators from a single decode of each row's bitmap:
//   - perMatcherStreams: streams satisfying each matcher (matcher eval; ignores time)
//   - allColumnNames: every distinct label name (independent of matchers and time)
//   - pointerRows/seen: per-(object,section,stream) time bounds for ALL streams,
//     pruned to rows overlapping [acc.startNanos,acc.endNanos]; scoped to matching streams
//     by the caller.
//
// Column order: object_path, section_index, column_name, label_value,
// stream_id_bitmap, min_timestamp, max_timestamp, kind.
func accumulateStreamScan(
	rb arrow.RecordBatch,
	acc *StreamScan,
) (bool, error) {
	if rb.NumRows() == 0 {
		return false, nil
	}
	var productive bool

	objectPathCol, ok := rb.Column(0).(*array.String)
	if !ok {
		return productive, fmt.Errorf("ResolveStreamsAndPointers object_path has unexpected type %T", rb.Column(0))
	}
	sectionIndexCol, ok := rb.Column(1).(*array.Int64)
	if !ok {
		return productive, fmt.Errorf("ResolveStreamsAndPointers section_index has unexpected type %T", rb.Column(1))
	}
	columnNameCol, ok := rb.Column(2).(*array.String)
	if !ok {
		return productive, fmt.Errorf("ResolveStreamsAndPointers column_name has unexpected type %T", rb.Column(2))
	}
	labelValueCol, ok := rb.Column(3).(*array.String)
	if !ok {
		return productive, fmt.Errorf("ResolveStreamsAndPointers label_value has unexpected type %T", rb.Column(3))
	}
	bitmapCol, ok := rb.Column(4).(*array.Binary)
	if !ok {
		return productive, fmt.Errorf("ResolveStreamsAndPointers stream_id_bitmap has unexpected type %T", rb.Column(4))
	}
	minTsCol, ok := rb.Column(5).(*array.Timestamp)
	if !ok {
		return productive, fmt.Errorf("ResolveStreamsAndPointers min_timestamp has unexpected type %T", rb.Column(5))
	}
	maxTsCol, ok := rb.Column(6).(*array.Timestamp)
	if !ok {
		return productive, fmt.Errorf("ResolveStreamsAndPointers max_timestamp has unexpected type %T", rb.Column(6))
	}

	for i := 0; i < int(rb.NumRows()); i++ {
		// Full label-name set: any non-empty column_name, independent of the
		// matcher and time logic below.
		if !columnNameCol.IsNull(i) {
			if name := columnNameCol.Value(i); name != "" {
				acc.allColumnNames[name] = struct{}{}
			}
		}

		// Both the matcher and pointer accumulators need object_path + bitmap.
		if objectPathCol.IsNull(i) || bitmapCol.IsNull(i) {
			continue
		}
		objectPath := objectPathCol.Value(i)
		bitmapBytes := bitmapCol.Value(i)

		// Matchers this row satisfies (needs column_name + label_value).
		var targeted []matcherIndex
		matchedSmall := [8]matcherIndex{}
		matched := matchedSmall[:0]
		labelName := ""
		hasLabelName := false
		if !columnNameCol.IsNull(i) && !labelValueCol.IsNull(i) {
			name := columnNameCol.Value(i)
			value := labelValueCol.Value(i)
			labelName = name
			hasLabelName = name != ""
			targeted = acc.matchersByName[name]
			if len(targeted) > cap(matched) {
				matched = make([]matcherIndex, 0, len(targeted))
			}
			for _, idx := range targeted {
				if acc.activeMatchers[idx].Matches(value) {
					matched = append(matched, idx)
				}
			}
		}

		// Whether this row contributes pointer bounds (needs section_index;
		// time-filtered). A null bound can't be pruned, so keep it and emit zero.
		boundsOK := false
		var sectionIndex int64
		var minTs, maxTs int64
		hasBounds := false
		if !sectionIndexCol.IsNull(i) {
			sectionIndex = sectionIndexCol.Value(i)
			if !minTsCol.IsNull(i) && !maxTsCol.IsNull(i) {
				hasBounds = true
				minTs = int64(minTsCol.Value(i))
				maxTs = int64(maxTsCol.Value(i))
				boundsOK = !(maxTs < acc.startNanos || minTs > acc.endNanos)
			} else {
				boundsOK = true
			}
		}

		if len(matched) == 0 && !boundsOK {
			continue
		}
		productive = true

		// Single decode of the LSB bitmap: byte b, bit p set => streamID = b*8+p.
		for byteIdx, b := range bitmapBytes {
			if b == 0 {
				continue
			}
			for bitPos := 0; bitPos < 8; bitPos++ {
				if (b>>uint(bitPos))&1 == 0 {
					continue
				}
				streamID := int64(byteIdx*8 + bitPos)
				ref := StreamRef{ObjectPath: objectPath, StreamID: streamID}
				acc.allStreams[ref] = struct{}{}
				if hasLabelName {
					names := acc.labelNamesByStream[ref]
					if names == nil {
						names = make(map[string]struct{})
						acc.labelNamesByStream[ref] = names
					}
					names[labelName] = struct{}{}
				}

				for _, idx := range targeted {
					acc.perMatcherHasName[idx][ref] = struct{}{}
				}

				for _, idx := range matched {
					acc.perMatcherStreams[idx][ref] = struct{}{}
				}

				if boundsOK {
					key := pointerRowKey{objectPath: objectPath, sectionIndex: sectionIndex, streamID: streamID}
					if idx, exists := acc.seen[key]; exists {
						mergePointerRowBounds(&acc.pointerRows[idx], minTs, maxTs, hasBounds)
					} else {
						row := PointerRow{ObjectPath: objectPath, SectionIndex: sectionIndex, StreamID: streamID}
						mergePointerRowBounds(&row, minTs, maxTs, hasBounds)
						acc.pointerRows = append(acc.pointerRows, row)
						acc.seen[key] = len(acc.pointerRows) - 1
					}
				}
			}
		}
	}
	return productive, nil
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
