package streams

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"maps"
	"strconv"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/symbolizer"
)

// RowReader reads the set of streams from an [Object].
type RowReader struct {
	sec   *Section
	ready bool

	matchIDs  map[int64]struct{}
	predicate RowPredicate

	buf []dataset.Row

	reader  *dataset.RowReader
	columns []dataset.Column

	symbols *symbolizer.Symbolizer
}

var errRowReaderNotOpen = errors.New("row reader not opened")

// NewRowReader creates a new RowReader that reads rows from the provided
// [Section].
//
// Call [RowReader.Open] before calling [RowReader.Read].
func NewRowReader(sec *Section) *RowReader {
	var sr RowReader
	sr.Reset(sec)
	return &sr
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

// SetPredicate sets the predicate to use for filtering logs. [LogsReader.Read]
// will only return logs for which the predicate passes.
//
// SetPredicate returns an error if the predicate is not supported by
// LogsReader.
//
// A predicate may only be set before reading begins or after a call to
// [RowReader.Reset].
func (r *RowReader) SetPredicate(p RowPredicate) error {
	if r.ready {
		return fmt.Errorf("cannot change predicate after reading has started")
	}

	r.predicate = p
	return nil
}

// MatchStreams provides a sequence of stream IDs for the reader to match.
// [RowReader.Read] will only return the streams with the provided IDs.
//
// MatchStreams may be called multiple times to match multiple sets of streams.
// An empty set (or never calling it) applies no stream-ID filter.
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

// Read reads up to the next len(s) streams from the reader and stores them
// into s. It returns the number of streams read and any error encountered. At
// the end of the stream section, Read returns 0, io.EOF.
func (r *RowReader) Read(ctx context.Context, s []Stream) (int, error) {
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
		if err := decodeRow(r.sec.Columns(), r.buf[i], &s[i], r.symbols, nil, false); err != nil {
			return i, fmt.Errorf("decoding stream: %w", err)
		}
	}

	return n, nil
}

func (r *RowReader) initReader(ctx context.Context) error {
	dset, err := r.sec.makeDataset()
	if err != nil {
		return fmt.Errorf("creating section dataset: %w", err)
	}
	columns := dset.Columns()

	// The matched stream IDs aren't part of r.predicate; build them as a separate predicate and AND
	// them with the user predicate (RowReaderOptions.Predicates are ANDed together).
	var predicates []dataset.Predicate
	if p := streamIDPredicate(maps.Keys(r.matchIDs), columns, r.sec.Columns()); p != nil {
		predicates = append(predicates, p)
	}
	if p := translateStreamsPredicate(r.predicate, columns, r.sec.Columns()); p != nil {
		predicates = append(predicates, p)
	}

	readerOpts := dataset.RowReaderOptions{
		Dataset:           dset,
		Columns:           columns,
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

	r.columns = columns
	r.ready = true
	return nil
}

// Reset resets the RowReader with a new decoder to read from. Reset allows
// reusing a RowReader without allocating a new one.
//
// Any set predicate is cleared when Reset is called.
//
// Reset may be called with a nil object and a negative section index to clear
// the RowReader without needing a new object.
func (r *RowReader) Reset(sec *Section) {
	r.sec = sec
	r.matchIDs = nil
	r.predicate = nil
	r.ready = false
	r.columns = nil

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

func translateStreamsPredicate(p RowPredicate, dsetColumns []dataset.Column, actualColumns []*Column) dataset.Predicate {
	if p == nil {
		return nil
	}

	switch p := p.(type) {
	case AndRowPredicate:
		return dataset.AndPredicate{
			Left:  translateStreamsPredicate(p.Left, dsetColumns, actualColumns),
			Right: translateStreamsPredicate(p.Right, dsetColumns, actualColumns),
		}

	case OrRowPredicate:
		return dataset.OrPredicate{
			Left:  translateStreamsPredicate(p.Left, dsetColumns, actualColumns),
			Right: translateStreamsPredicate(p.Right, dsetColumns, actualColumns),
		}

	case NotRowPredicate:
		return dataset.NotPredicate{
			Inner: translateStreamsPredicate(p.Inner, dsetColumns, actualColumns),
		}

	case TimeRangeRowPredicate:
		minTimestamp := findDatasetColumn(dsetColumns, actualColumns, func(col *Column) bool {
			return col.Type == ColumnTypeMinTimestamp
		})
		maxTimestamp := findDatasetColumn(dsetColumns, actualColumns, func(col *Column) bool {
			return col.Type == ColumnTypeMaxTimestamp
		})
		if minTimestamp == nil || maxTimestamp == nil {
			return dataset.FalsePredicate{}
		}
		return convertStreamsTimePredicate(p, minTimestamp, maxTimestamp)

	case LabelMatcherRowPredicate:
		metadataColumn := findDatasetColumn(dsetColumns, actualColumns, func(col *Column) bool {
			return col.Type == ColumnTypeLabel && col.Name == p.Name
		})
		if metadataColumn == nil {
			return dataset.FalsePredicate{}
		}
		return dataset.EqualPredicate{
			Column: metadataColumn,
			Value:  dataset.BinaryValue(unsafeSlice(p.Value, 0)),
		}

	case LabelFilterRowPredicate:
		metadataColumn := findDatasetColumn(dsetColumns, actualColumns, func(col *Column) bool {
			return col.Type == ColumnTypeLabel && col.Name == p.Name
		})
		if metadataColumn == nil {
			return dataset.FalsePredicate{}
		}
		return dataset.FuncPredicate{
			Column: metadataColumn,
			Keep: func(_ dataset.Column, value dataset.Value) bool {
				return p.Keep(p.Name, valueToString(value))
			},
		}

	default:
		panic(fmt.Sprintf("unsupported predicate type %T", p))
	}
}

func convertStreamsTimePredicate(p TimeRangeRowPredicate, minColumn, maxColumn dataset.Column) dataset.Predicate {
	switch {
	case p.IncludeStart && p.IncludeEnd: // !max.Before(p.StartTime) && !min.After(p.EndTime)
		return dataset.AndPredicate{
			Left: dataset.NotPredicate{
				Inner: dataset.LessThanPredicate{
					Column: maxColumn,
					Value:  dataset.Int64Value(p.StartTime.UnixNano()),
				},
			},
			Right: dataset.NotPredicate{
				Inner: dataset.GreaterThanPredicate{
					Column: minColumn,
					Value:  dataset.Int64Value(p.EndTime.UnixNano()),
				},
			},
		}

	case p.IncludeStart && !p.IncludeEnd: // !max.Before(p.StartTime) && min.Before(p.EndTime)
		return dataset.AndPredicate{
			Left: dataset.NotPredicate{
				Inner: dataset.LessThanPredicate{
					Column: maxColumn,
					Value:  dataset.Int64Value(p.StartTime.UnixNano()),
				},
			},
			Right: dataset.LessThanPredicate{
				Column: minColumn,
				Value:  dataset.Int64Value(p.EndTime.UnixNano()),
			},
		}

	case !p.IncludeStart && p.IncludeEnd: // max.After(p.StartTime) && !min.After(p.EndTime)
		return dataset.AndPredicate{
			Left: dataset.GreaterThanPredicate{
				Column: maxColumn,
				Value:  dataset.Int64Value(p.StartTime.UnixNano()),
			},
			Right: dataset.NotPredicate{
				Inner: dataset.GreaterThanPredicate{
					Column: minColumn,
					Value:  dataset.Int64Value(p.EndTime.UnixNano()),
				},
			},
		}

	case !p.IncludeStart && !p.IncludeEnd: // max.After(p.StartTime) && min.Before(p.EndTime)
		return dataset.AndPredicate{
			Left: dataset.GreaterThanPredicate{
				Column: maxColumn,
				Value:  dataset.Int64Value(p.StartTime.UnixNano()),
			},
			Right: dataset.LessThanPredicate{
				Column: minColumn,
				Value:  dataset.Int64Value(p.EndTime.UnixNano()),
			},
		}

	default:
		panic("unreachable")
	}
}

// streamIDPredicate builds an InPredicate that keeps only the given stream IDs. It returns nil when no
// IDs are requested (no filter) and a FalsePredicate when the section has no stream_id column.
func streamIDPredicate(ids iter.Seq[int64], columns []dataset.Column, actual []*Column) dataset.Predicate {
	streamIDColumn := findDatasetColumn(columns, actual, func(col *Column) bool {
		return col.Type == ColumnTypeStreamID
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
		Values: dataset.NewInt64ValueSet(values),
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
