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
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/symbolizer"
)

// RowReader reads the set of logs from an [Object].
type RowReader struct {
	sec   *Section
	ready bool

	matchIDs   map[int64]struct{}
	predicates []RowPredicate

	buf []dataset.Row

	reader     *dataset.Reader
	columns    []dataset.Column
	columnDesc []*logsmd.ColumnDesc

	symbols *symbolizer.Symbolizer
}

// NewRowReader creates a new WowReader that reads from the provided [Section].
func NewRowReader(sec *Section) *RowReader {
	var lr RowReader
	lr.Reset(sec)
	return &lr
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

// Read reads up to the next len(s) records from the reader and stores them
// into s. It returns the number of records read and any error encountered. At
// the end of the logs section, Read returns 0, io.EOF.
func (r *RowReader) Read(ctx context.Context, s []Record) (int, error) {
	if r.sec == nil {
		return 0, io.EOF
	}

	if !r.ready {
		err := r.initReader(ctx)
		if err != nil {
			return 0, err
		}
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
		err := decodeRow(r.columnDesc, r.buf[i], &s[i], r.symbols)
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
	dec := newDecoder(r.sec.reader)

	columnDescs, err := dec.Columns(ctx)
	if err != nil {
		return fmt.Errorf("reading columns: %w", err)
	}

	dset, err := newColumnsDataset(r.sec.Columns())
	if err != nil {
		return fmt.Errorf("creating columns dataset: %w", err)
	}
	columns := dset.Columns()

	// r.predicate doesn't contain mappings of stream IDs; we need to build
	// that as a separate predicate and AND them together.
	var predicates []dataset.Predicate
	if p := streamIDPredicate(maps.Keys(r.matchIDs), columns, columnDescs); p != nil {
		predicates = append(predicates, p)
	}

	for _, predicate := range r.predicates {
		if p := translateLogsPredicate(predicate, columns, columnDescs); p != nil {
			predicates = append(predicates, p)
		}
	}

	readerOpts := dataset.ReaderOptions{
		Dataset:    dset,
		Columns:    columns,
		Predicates: orderPredicates(predicates),

		TargetCacheSize: 16_000_000, // Permit up to 16MB of cache pages.
	}

	if r.reader == nil {
		r.reader = dataset.NewReader(readerOpts)
	} else {
		r.reader.Reset(readerOpts)
	}

	if r.symbols == nil {
		r.symbols = symbolizer.New(128, 100_000)
	} else {
		r.symbols.Reset()
	}

	r.columnDesc = columnDescs
	r.columns = columns
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

	r.columns = nil
	r.columnDesc = nil

	if r.symbols != nil {
		r.symbols.Reset()
	}

	// We leave r.reader as-is to avoid reallocating; it'll be reset on the first
	// call to Read.
}

// Close closes the RowReader and releases any resources it holds. Closed
// RowReaders can be reused by calling [RowReader.Reset].
func (r *RowReader) Close() error {
	if r.reader != nil {
		return r.reader.Close()
	}
	return nil
}

func streamIDPredicate(ids iter.Seq[int64], columns []dataset.Column, columnDesc []*logsmd.ColumnDesc) dataset.Predicate {
	streamIDColumn := findColumnFromDesc(columns, columnDesc, func(desc *logsmd.ColumnDesc) bool {
		return desc.Type == logsmd.COLUMN_TYPE_STREAM_ID
	})
	if streamIDColumn == nil {
		return dataset.FalsePredicate{}
	}

	var values []dataset.Value
	for id := range ids {
		values = append(values, dataset.Int64Value(id))
	}

	if len(values) == 0 {
		return nil
	}

	return dataset.InPredicate{
		Column: streamIDColumn,
		Values: values,
	}
}

func translateLogsPredicate(p RowPredicate, columns []dataset.Column, columnDesc []*logsmd.ColumnDesc) dataset.Predicate {
	if p == nil {
		return nil
	}

	switch p := p.(type) {
	case AndRowPredicate:
		return dataset.AndPredicate{
			Left:  translateLogsPredicate(p.Left, columns, columnDesc),
			Right: translateLogsPredicate(p.Right, columns, columnDesc),
		}

	case OrRowPredicate:
		return dataset.OrPredicate{
			Left:  translateLogsPredicate(p.Left, columns, columnDesc),
			Right: translateLogsPredicate(p.Right, columns, columnDesc),
		}

	case NotRowPredicate:
		return dataset.NotPredicate{
			Inner: translateLogsPredicate(p.Inner, columns, columnDesc),
		}

	case TimeRangeRowPredicate:
		timeColumn := findColumnFromDesc(columns, columnDesc, func(desc *logsmd.ColumnDesc) bool {
			return desc.Type == logsmd.COLUMN_TYPE_TIMESTAMP
		})
		if timeColumn == nil {
			return dataset.FalsePredicate{}
		}
		return convertLogsTimePredicate(p, timeColumn)

	case LogMessageFilterRowPredicate:
		messageColumn := findColumnFromDesc(columns, columnDesc, func(desc *logsmd.ColumnDesc) bool {
			return desc.Type == logsmd.COLUMN_TYPE_MESSAGE
		})
		if messageColumn == nil {
			return dataset.FalsePredicate{}
		}

		return dataset.FuncPredicate{
			Column: messageColumn,
			Keep: func(_ dataset.Column, value dataset.Value) bool {
				if value.Type() == datasetmd.VALUE_TYPE_BYTE_ARRAY {
					// To handle older dataobjs that still use string type for message column. This can be removed in future.
					return p.Keep(value.ByteArray())
				}

				return p.Keep(value.ByteArray())
			},
		}

	case MetadataMatcherRowPredicate:
		metadataColumn := findColumnFromDesc(columns, columnDesc, func(desc *logsmd.ColumnDesc) bool {
			return desc.Type == logsmd.COLUMN_TYPE_METADATA && desc.Info.Name == p.Key
		})
		if metadataColumn == nil {
			return dataset.FalsePredicate{}
		}
		return dataset.EqualPredicate{
			Column: metadataColumn,
			Value:  dataset.ByteArrayValue(unsafeSlice(p.Value, 0)),
		}

	case MetadataFilterRowPredicate:
		metadataColumn := findColumnFromDesc(columns, columnDesc, func(desc *logsmd.ColumnDesc) bool {
			return desc.Type == logsmd.COLUMN_TYPE_METADATA && desc.Info.Name == p.Key
		})
		if metadataColumn == nil {
			return dataset.FalsePredicate{}
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

func findColumnFromDesc[Desc any](columns []dataset.Column, descs []Desc, check func(Desc) bool) dataset.Column {
	for i, desc := range descs {
		if check(desc) {
			return columns[i]
		}
	}
	return nil
}

func valueToString(value dataset.Value) string {
	switch value.Type() {
	case datasetmd.VALUE_TYPE_UNSPECIFIED:
		return ""
	case datasetmd.VALUE_TYPE_INT64:
		return strconv.FormatInt(value.Int64(), 10)
	case datasetmd.VALUE_TYPE_UINT64:
		return strconv.FormatUint(value.Uint64(), 10)
	case datasetmd.VALUE_TYPE_BYTE_ARRAY:
		return unsafeString(value.ByteArray())
	default:
		panic(fmt.Sprintf("unsupported value type %s", value.Type()))
	}
}
