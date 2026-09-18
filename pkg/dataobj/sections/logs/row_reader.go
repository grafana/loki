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

	// projSectionColumns are the projected columns, in the same order as the dataset columns
	// given to the reader. Read decodes a row by position, so the two must stay aligned. It
	// holds every recognized column when no projection is set.
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

// SetProjectedColumns restricts the reader to the given column types plus the metadata
// columns named in metadataNames. A [ColumnTypeMetadata] entry in types reads all metadata
// columns instead.
//
// The field of a column that is not read comes back as its zero value.
//
// The columns a stream match or a predicate needs are read whether or not they are listed,
// because a predicate that finds no column reduces to "drop every row". A column added
// that way is decoded into the [Record] as well, so a pushed-down metadata predicate makes
// its key visible even when the caller did not project it.
//
// When SetProjectedColumns is not called, every recognized column is read.
//
// SetProjectedColumns may only be called before reading begins or after a call to
// [RowReader.Reset].
func (r *RowReader) SetProjectedColumns(types []ColumnType, metadataNames []string) error {
	if r.ready {
		return fmt.Errorf("cannot change projected columns after reading has started")
	}

	// A projection has to select something. The reader cannot read zero columns, and an
	// empty selection is a caller mistake.
	if len(types) == 0 && len(metadataNames) == 0 {
		return fmt.Errorf("projection must select at least one column")
	}

	r.projTypes = make(map[ColumnType]struct{}, len(types))
	for _, t := range types {
		r.projTypes[t] = struct{}{}
	}
	r.projMetadata = make(map[string]struct{}, len(metadataNames))
	for _, name := range metadataNames {
		r.projMetadata[name] = struct{}{}
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

	// datasetColumns and sectionColumns are parallel: datasetColumns[i] describes the same
	// column as sectionColumns[i]. The projection below filters both together so they stay
	// that way, because the reader addresses a row's values by position.
	datasetColumns, sectionColumns := dset.Columns(), r.sec.Columns()
	if r.projected {
		types, metadataNames := r.projectionColumns()
		datasetColumns, sectionColumns = projectColumns(datasetColumns, sectionColumns, types, metadataNames)

		// The section holds none of the projected columns, so there is nothing to read. The
		// dataset reader cannot be opened on an empty column set, so report it here rather
		// than let it panic later.
		if len(datasetColumns) == 0 {
			return fmt.Errorf("none of the projected columns are present in the section")
		}
	}

	// The matched stream IDs are not part of r.predicates, so build them as a separate
	// predicate; RowReaderOptions.Predicates are ANDed together.
	var predicates []dataset.Predicate
	if p := streamIDPredicate(maps.Keys(r.matchIDs), datasetColumns, sectionColumns); p != nil {
		predicates = append(predicates, p)
	}

	for _, predicate := range r.predicates {
		p := translateLogsPredicate(predicate, datasetColumns, sectionColumns)

		// A constant predicate that keeps every row constrains nothing, so drop it. One that
		// drops every row stays: the dataset reader prunes it to an empty row range, so the
		// read ends cleanly without touching object storage.
		if keep, ok := dataset.IsConstPredicate(p); ok && keep {
			continue
		}

		predicates = append(predicates, p)
	}

	readerOpts := dataset.RowReaderOptions{
		Dataset:           dset,
		Columns:           datasetColumns,
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

	r.projSectionColumns = sectionColumns
	r.ready = true
	return nil
}

// projectionColumns returns the column types and metadata names to read: the caller's
// projection, widened with whatever the stream match and the predicates need in order to
// evaluate.
func (r *RowReader) projectionColumns() (types map[ColumnType]struct{}, metadataNames map[string]struct{}) {
	types = make(map[ColumnType]struct{}, len(r.projTypes))
	metadataNames = make(map[string]struct{}, len(r.projMetadata))
	maps.Copy(types, r.projTypes)
	maps.Copy(metadataNames, r.projMetadata)

	// A stream match is evaluated as a predicate on the stream ID column, so that column has
	// to be read; without it the match reduces to a constant that drops every row.
	if len(r.matchIDs) > 0 {
		types[ColumnTypeStreamID] = struct{}{}
	}

	// Same for the predicates. translateLogsPredicate later looks each one's column up among
	// the projected columns, and turns a predicate whose column is missing into a constant
	// that keeps or drops every row instead of filtering on it.
	for _, p := range r.predicates {
		projectPredicateColumns(p, types, metadataNames)
	}
	return types, metadataNames
}

// projectPredicateColumns records the columns p reads into types and metadataNames.
//
// It panics on a predicate it does not recognize. Every predicate has to name its columns
// here: one that does not would leave its column unprojected, and translateLogsPredicate
// would then find no column for it and turn it into a constant, so it would silently keep
// or drop every row instead of filtering.
func projectPredicateColumns(p RowPredicate, types map[ColumnType]struct{}, metadataNames map[string]struct{}) {
	switch p := p.(type) {
	case AndRowPredicate:
		projectPredicateColumns(p.Left, types, metadataNames)
		projectPredicateColumns(p.Right, types, metadataNames)
	case OrRowPredicate:
		projectPredicateColumns(p.Left, types, metadataNames)
		projectPredicateColumns(p.Right, types, metadataNames)
	case NotRowPredicate:
		projectPredicateColumns(p.Inner, types, metadataNames)
	case TimeRangeRowPredicate:
		types[ColumnTypeTimestamp] = struct{}{}
	case LogMessageFilterRowPredicate:
		types[ColumnTypeMessage] = struct{}{}
	case MetadataMatcherRowPredicate:
		metadataNames[p.Key] = struct{}{}
	case MetadataFilterRowPredicate:
		metadataNames[p.Key] = struct{}{}
	case nil:
		// A nil branch reads nothing, which is how translateLogsPredicate treats it too.
	default:
		panic(fmt.Sprintf("unsupported predicate type %T", p))
	}
}

// projectColumns keeps the columns the projection selects, returning the dataset and
// section slices still aligned by position.
func projectColumns(datasetColumns []dataset.Column, sectionColumns []*Column, types map[ColumnType]struct{}, metadataNames map[string]struct{}) ([]dataset.Column, []*Column) {
	_, allMetadata := types[ColumnTypeMetadata]

	outDataset := make([]dataset.Column, 0, len(datasetColumns))
	outSection := make([]*Column, 0, len(sectionColumns))
	for i, col := range sectionColumns {
		var keep bool
		if col.Type == ColumnTypeMetadata {
			_, named := metadataNames[col.Name]
			keep = allMetadata || named
		} else {
			_, keep = types[col.Type]
		}
		if keep {
			outDataset = append(outDataset, datasetColumns[i])
			outSection = append(outSection, col)
		}
	}
	return outDataset, outSection
}

// Reset resets the RowReader with a new Section to read from. Reset allows
// reusing a RowReader without allocating a new one.
//
// Any set predicate, stream match and column projection are cleared when Reset is called.
//
// Reset may be called with a nil section to clear the RowReader without needing a new one.
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
	var values []dataset.Value
	for i := range ids {
		values = append(values, dataset.Int64Value(i))
	}

	// No IDs means no stream filter at all. Decide that before looking the column up, so an
	// unmatched read does not depend on whether the column was projected: reading the whole
	// section without projecting the stream ID would otherwise drop every row.
	if len(values) == 0 {
		return nil
	}

	streamIDColumn := findDatasetColumn(columns, columnDesc, func(col *Column) bool {
		return col.Type == ColumnTypeStreamID
	})
	if streamIDColumn == nil {
		return dataset.FalsePredicate{}
	}

	return dataset.InPredicate{
		Column: streamIDColumn,
		// A logs section sorts by stream_id, so this check sees long runs of the same
		// value. The reader is single-threaded, so a memoized set can cache the
		// previous result and turn most per-row checks into a comparison.
		Values: dataset.NewMemoizedInt64ValueSet(values),
	}
}

// translateLogsPredicate converts p into the equivalent dataset predicate.
func translateLogsPredicate(p RowPredicate, dsetColumns []dataset.Column, actualColumns []*Column) dataset.Predicate {
	if p == nil {
		return nil
	}

	switch p := p.(type) {
	case AndRowPredicate:
		return dataset.FoldAndPredicate(
			translateLogsPredicate(p.Left, dsetColumns, actualColumns),
			translateLogsPredicate(p.Right, dsetColumns, actualColumns),
		)

	case OrRowPredicate:
		return dataset.FoldOrPredicate(
			translateLogsPredicate(p.Left, dsetColumns, actualColumns),
			translateLogsPredicate(p.Right, dsetColumns, actualColumns),
		)

	case NotRowPredicate:
		return dataset.FoldNotPredicate(translateLogsPredicate(p.Inner, dsetColumns, actualColumns))

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
			// The section holds no column for the key, so every row reads as an empty value
			// for it. Keep the whole section when the empty value matches, drop it otherwise.
			return dataset.NewConstPredicate(p.Value == "")
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
			// The section holds no column for the key, so every row reads as an empty value
			// for it. A negation such as key!="v" therefore keeps the whole section.
			return dataset.NewConstPredicate(p.Keep(p.Key, ""))
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
