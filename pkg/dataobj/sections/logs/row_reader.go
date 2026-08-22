package logs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"maps"
	"strconv"
	"unsafe"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/symbolizer"
)

// RowReader reads the set of logs from an [Object].
type RowReader struct {
	sec   *Section
	ready bool

	matchIDs   map[int64]struct{}
	predicates []RowPredicate

	// Column projection. When projected is false, every recognized column is read.
	projected    bool
	projTypes    map[ColumnType]struct{}
	projMetadata map[string]struct{}

	buf []dataset.Row

	reader *dataset.RowReader

	// projSectionColumns are the projected logs columns, parallel to the dataset columns passed to the reader,
	// and used to decode each row. They are the full recognized set when no projection is set.
	projSectionColumns []*Column

	symbols *symbolizer.Symbolizer
}

var errRowReaderNotOpen = errors.New("row reader not opened")

// NewRowReader creates a new RowReader that reads from the provided [Section].
//
// Call [RowReader.Open] before calling [RowReader.Read].
func NewRowReader(sec *Section) *RowReader {
	var lr RowReader
	lr.Reset(sec)
	return &lr
}

// Open initializes RowReader resources.
//
// Open must be called before [RowReader.Read]. Open is safe to call multiple
// times. Open is a no-op when the reader has no section.
func (r *RowReader) Open(ctx context.Context) error {
	if r.sec == nil || r.ready {
		return nil
	}

	if err := r.initReader(ctx); err != nil {
		_ = r.Close()
		return fmt.Errorf("initializing row reader: %w", err)
	}
	return nil
}

// MatchStreams provides a sequence of stream IDs for the logs reader to match.
// [RowReader.Read] will only return logs for the provided stream IDs.
//
// MatchStreams may be called multiple times to match multiple sets of streams.
//
// MatchStreams may only be called before reading begins or after a call to
// [RowReader.Reset].
func (r *RowReader) MatchStreams(ids iter.Seq[int64]) error {
	if r.ready {
		return fmt.Errorf("cannot change matched streams after reading has started")
	}

	if r.matchIDs == nil {
		r.matchIDs = make(map[int64]struct{})
	}
	for id := range ids {
		r.matchIDs[id] = struct{}{}
	}
	return nil
}

// SetPredicate sets the predicates to use for filtering logs. [RowReader.Read]
// will only return logs for which the predicate passes.
//
// Predicates may only be set before reading begins or after a call to
// [RowReader.Reset].
func (r *RowReader) SetPredicates(p []RowPredicate) error {
	if r.ready {
		return fmt.Errorf("cannot change predicate after reading has started")
	}

	r.predicates = p
	return nil
}

// SetColumns restricts the reader to the given column types plus the named metadata columns; other
// columns (notably the message column) are not read from object storage. The stream-ID column is always
// read when a stream match is set, and the timestamp column when a time-range predicate is set, whether
// or not they are listed here, so neither of those predicates silently reduces to "drop every row". A
// pushed-down metadata predicate still needs its key projected explicitly. When SetColumns is not called,
// all recognized columns are read.
//
// SetColumns may only be called before reading begins or after a call to [RowReader.Reset].
func (r *RowReader) SetColumns(types []ColumnType, metadataNames []string) error {
	if r.ready {
		return fmt.Errorf("cannot change columns after reading has started")
	}

	r.projTypes = make(map[ColumnType]struct{}, len(types))
	for _, t := range types {
		r.projTypes[t] = struct{}{}
	}
	r.projMetadata = make(map[string]struct{}, len(metadataNames))
	for _, n := range metadataNames {
		r.projMetadata[n] = struct{}{}
	}
	r.projected = true
	return nil
}

// Read reads up to the next len(s) records from the reader and stores them
// into s. It returns the number of records read and any error encountered. At
// the end of the logs section, Read returns 0, io.EOF.
func (r *RowReader) Read(ctx context.Context, s []Record) (int, error) {
	if r.sec == nil {
		return 0, io.EOF
	}

	if !r.ready {
		return 0, errRowReaderNotOpen
	}

	r.buf = slicegrow.GrowToCap(r.buf, len(s))
	r.buf = r.buf[:len(s)]

	n, err := r.reader.Read(ctx, r.buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, fmt.Errorf("reading rows: %w", err)
	} else if n == 0 && errors.Is(err, io.EOF) {
		return 0, io.EOF
	}

	for i := range r.buf[:n] {
		err := DecodeRow(r.projSectionColumns, r.buf[i], &s[i], r.symbols)
		if err != nil {
			return i, fmt.Errorf("decoding record: %w", err)
		}
	}

	return n, nil
}

func unsafeSlice(data string, capacity int) []byte {
	if capacity <= 0 {
		capacity = len(data)
	}
	return unsafe.Slice(unsafe.StringData(data), capacity)
}

func unsafeString(data []byte) string {
	return unsafe.String(unsafe.SliceData(data), len(data))
}

func (r *RowReader) initReader(ctx context.Context) error {
	dset, err := r.sec.makeDataset()
	if err != nil {
		return fmt.Errorf("creating section dataset: %w", err)
	}

	// datasetColumns and sectionColumns are parallel: datasetColumns[i] is the dataset column for the recognized
	// logs column sectionColumns[i]. Decoding is positional, so the projected slices below must stay aligned
	// and be passed to both the reader (RowReaderOptions.Columns) and DecodeRow.
	datasetColumns := dset.Columns()
	sectionColumns := r.sec.Columns()

	projDatasetColumns := datasetColumns
	projSectionColumns := sectionColumns
	if r.projected {
		// A pushed-down predicate reads its column directly, so that column must be projected. Include the
		// stream-ID column whenever streams are matched and the timestamp column whenever a time-range
		// predicate is set, even if the caller left them out of SetColumns; otherwise findDatasetColumn
		// would miss the column and the predicate would reduce to FalsePredicate — dropping every row.
		projTypes := maps.Clone(r.projTypes)
		if len(r.matchIDs) > 0 {
			projTypes[ColumnTypeStreamID] = struct{}{}
		}
		if predicatesNeedTimestamp(r.predicates) {
			projTypes[ColumnTypeTimestamp] = struct{}{}
		}

		projDatasetColumns = make([]dataset.Column, 0, len(datasetColumns))
		projSectionColumns = make([]*Column, 0, len(sectionColumns))
		_, allMetadata := projTypes[ColumnTypeMetadata]
		for i, lc := range sectionColumns {
			keep := false
			if lc.Type == ColumnTypeMetadata {
				_, named := r.projMetadata[lc.Name]
				keep = allMetadata || named
			} else {
				_, keep = projTypes[lc.Type]
			}
			if keep {
				projDatasetColumns = append(projDatasetColumns, datasetColumns[i])
				projSectionColumns = append(projSectionColumns, lc)
			}
		}
	}

	// r.predicate doesn't contain mappings of stream IDs; we need to build
	// that as a separate predicate and AND them together.
	var predicates []dataset.Predicate
	if p := streamIDPredicate(maps.Keys(r.matchIDs), projDatasetColumns, projSectionColumns); p != nil {
		predicates = append(predicates, p)
	}

	for _, predicate := range r.predicates {
		if p := translateLogsPredicate(predicate, projDatasetColumns, projSectionColumns); p != nil {
			predicates = append(predicates, p)
		}
	}

	readerOpts := dataset.RowReaderOptions{
		Dataset:           dset,
		Columns:           projDatasetColumns,
		Predicates:        predicates,
		PrefetchAllOnOpen: true,
	}

	if r.reader == nil {
		r.reader = dataset.NewRowReader(readerOpts)
	} else {
		r.reader.Reset(readerOpts)
	}
	if err := r.reader.Open(ctx); err != nil {
		return fmt.Errorf("opening row reader: %w", err)
	}

	if r.symbols == nil {
		r.symbols = symbolizer.New(128, 100_000)
	} else {
		r.symbols.Reset()
	}

	r.projSectionColumns = projSectionColumns
	r.ready = true
	return nil
}

// Reset resets the RowReader with a new Section to read from. Reset allows
// reusing a RowReader without allocating a new one.
//
// Any set predicate is cleared when Reset is called.
//
// Reset may be called with a nil object and a negative section index to clear
// the RowReader without needing a new object.
func (r *RowReader) Reset(sec *Section) {
	r.sec = sec
	r.ready = false

	clear(r.matchIDs)
	r.predicates = nil

	r.projected = false
	clear(r.projTypes)
	clear(r.projMetadata)
	r.projSectionColumns = nil

	if r.symbols != nil {
		r.symbols.Reset()
	}

	// We leave r.reader as-is to avoid reallocating; it'll be reset on the first
	// call to Open.
}

// Close closes the RowReader and releases any resources it holds. Closed
// RowReaders can be reused by calling [RowReader.Reset].
func (r *RowReader) Close() error {
	if r.reader != nil {
		return r.reader.Close()
	}
	return nil
}

func streamIDPredicate(ids iter.Seq[int64], columns []dataset.Column, columnDesc []*Column) dataset.Predicate {
	streamIDColumn := findDatasetColumn(columns, columnDesc, func(col *Column) bool {
		return col.Type == ColumnTypeStreamID
	})
	if streamIDColumn == nil {
		return dataset.FalsePredicate{}
	}

	var values []dataset.Value
	for i := range ids {
		values = append(values, dataset.Int64Value(i))
	}

	if len(values) == 0 {
		return nil
	}

	return dataset.InPredicate{
		Column: streamIDColumn,
		// A logs section sorts by stream_id, so this check sees long runs of the same
		// value. The reader is single-threaded, so a memoized set can cache the
		// previous result and turn most per-row checks into a comparison.
		Values: dataset.NewMemoizedInt64ValueSet(values),
	}
}

// predicatesNeedTimestamp reports whether any predicate in the trees filters on the timestamp column, so
// the reader can project that column even when the caller omitted it from SetColumns.
func predicatesNeedTimestamp(preds []RowPredicate) bool {
	for _, p := range preds {
		if predicateNeedsTimestamp(p) {
			return true
		}
	}
	return false
}

func predicateNeedsTimestamp(p RowPredicate) bool {
	switch p := p.(type) {
	case TimeRangeRowPredicate:
		return true
	case AndRowPredicate:
		return predicateNeedsTimestamp(p.Left) || predicateNeedsTimestamp(p.Right)
	case OrRowPredicate:
		return predicateNeedsTimestamp(p.Left) || predicateNeedsTimestamp(p.Right)
	case NotRowPredicate:
		return predicateNeedsTimestamp(p.Inner)
	default:
		return false
	}
}

func translateLogsPredicate(p RowPredicate, datasetColumns []dataset.Column, actualColumns []*Column) dataset.Predicate {
	if p == nil {
		return nil
	}

	switch p := p.(type) {
	case AndRowPredicate:
		return dataset.AndPredicate{
			Left:  translateLogsPredicate(p.Left, datasetColumns, actualColumns),
			Right: translateLogsPredicate(p.Right, datasetColumns, actualColumns),
		}

	case OrRowPredicate:
		return dataset.OrPredicate{
			Left:  translateLogsPredicate(p.Left, datasetColumns, actualColumns),
			Right: translateLogsPredicate(p.Right, datasetColumns, actualColumns),
		}

	case NotRowPredicate:
		return dataset.NotPredicate{
			Inner: translateLogsPredicate(p.Inner, datasetColumns, actualColumns),
		}

	case TimeRangeRowPredicate:
		timeColumn := findDatasetColumn(datasetColumns, actualColumns, func(col *Column) bool {
			return col.Type == ColumnTypeTimestamp
		})
		if timeColumn == nil {
			return dataset.FalsePredicate{}
		}
		return convertLogsTimePredicate(p, timeColumn)

	case LogMessageFilterRowPredicate:
		messageColumn := findDatasetColumn(datasetColumns, actualColumns, func(col *Column) bool {
			return col.Type == ColumnTypeMessage
		})
		if messageColumn == nil {
			return dataset.FalsePredicate{}
		}

		return dataset.FuncPredicate{
			Column: messageColumn,
			Keep: func(_ dataset.Column, value dataset.Value) bool {
				if value.Type() == datasetmd.PHYSICAL_TYPE_BINARY {
					// To handle older dataobjs that still use string type for message column. This can be removed in future.
					return p.Keep(value.Binary())
				}

				return p.Keep(value.Binary())
			},
		}

	case MetadataMatcherRowPredicate:
		metadataColumn := findDatasetColumn(datasetColumns, actualColumns, func(col *Column) bool {
			return col.Type == ColumnTypeMetadata && col.Name == p.Key
		})
		if metadataColumn == nil {
			// The column is absent from this section, so every row reads as an empty value for the key:
			// keep the whole section only when the empty value matches.
			return constPredicate(p.Value == "")
		}
		return dataset.EqualPredicate{
			Column: metadataColumn,
			Value:  dataset.BinaryValue(unsafeSlice(p.Value, 0)),
		}

	case MetadataFilterRowPredicate:
		metadataColumn := findDatasetColumn(datasetColumns, actualColumns, func(col *Column) bool {
			return col.Type == ColumnTypeMetadata && col.Name == p.Key
		})
		if metadataColumn == nil {
			// The column is absent from this section, so every row reads as an empty value for the key:
			// keep the whole section only when the empty value matches.
			return constPredicate(p.Keep(p.Key, ""))
		}
		return dataset.FuncPredicate{
			Column: metadataColumn,
			Keep: func(_ dataset.Column, value dataset.Value) bool {
				return p.Keep(p.Key, valueToString(value))
			},
		}

	default:
		panic(fmt.Sprintf("unsupported predicate type %T", p))
	}
}

// constPredicate returns a predicate that keeps every row when keep is true and drops every row
// otherwise. It reduces a metadata predicate whose column is absent from a section.
func constPredicate(keep bool) dataset.Predicate {
	if keep {
		return dataset.TruePredicate{}
	}
	return dataset.FalsePredicate{}
}

func convertLogsTimePredicate(p TimeRangeRowPredicate, column dataset.Column) dataset.Predicate {
	var start dataset.Predicate = dataset.GreaterThanPredicate{
		Column: column,
		Value:  dataset.Int64Value(p.StartTime.UnixNano()),
	}
	if p.IncludeStart {
		start = dataset.OrPredicate{
			Left: start,
			Right: dataset.EqualPredicate{
				Column: column,
				Value:  dataset.Int64Value(p.StartTime.UnixNano()),
			},
		}
	}

	var end dataset.Predicate = dataset.LessThanPredicate{
		Column: column,
		Value:  dataset.Int64Value(p.EndTime.UnixNano()),
	}
	if p.IncludeEnd {
		end = dataset.OrPredicate{
			Left: end,
			Right: dataset.EqualPredicate{
				Column: column,
				Value:  dataset.Int64Value(p.EndTime.UnixNano()),
			},
		}
	}

	return dataset.AndPredicate{
		Left:  start,
		Right: end,
	}
}

func findDatasetColumn(columns []dataset.Column, actual []*Column, check func(*Column) bool) dataset.Column {
	for i, desc := range actual {
		if check(desc) {
			return columns[i]
		}
	}
	return nil
}

func valueToString(value dataset.Value) string {
	switch value.Type() {
	case datasetmd.PHYSICAL_TYPE_UNSPECIFIED:
		return ""
	case datasetmd.PHYSICAL_TYPE_INT64:
		return strconv.FormatInt(value.Int64(), 10)
	case datasetmd.PHYSICAL_TYPE_UINT64:
		return strconv.FormatUint(value.Uint64(), 10)
	case datasetmd.PHYSICAL_TYPE_BINARY:
		return unsafeString(value.Binary())
	default:
		panic(fmt.Sprintf("unsupported value type %s", value.Type()))
	}
}
