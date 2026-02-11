package streams

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/symbolizer"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// RowReader reads the set of streams from an [Object].
type RowReader struct {
	sec   *Section
	ready bool

	predicate RowPredicate

	buf []dataset.Row

	reader  *dataset.RowReader
	columns []dataset.Column

	symbols *symbolizer.Symbolizer
}

// NewRowReader creates a new RowReader that reads rows from the provided
// [Section].
func NewRowReader(sec *Section) *RowReader {
	var sr RowReader
	sr.Reset(sec)
	return &sr
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

// Read reads up to the next len(s) streams from the reader and stores them
// into s. It returns the number of streams read and any error encountered. At
// the end of the stream section, Read returns 0, io.EOF.
func (r *RowReader) Read(ctx context.Context, s []Stream) (int, error) {
	if r.sec == nil {
		return 0, io.EOF
	}

	if !r.ready {
		err := r.initReader()
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
		if err := decodeRow(r.sec.Columns(), r.buf[i], &s[i], r.symbols, nil, false); err != nil {
			return i, fmt.Errorf("decoding stream: %w", err)
		}
	}

	return n, nil
}

func (r *RowReader) initReader() error {
	dset, err := columnar.MakeDataset(r.sec.inner, r.sec.inner.Columns())
	if err != nil {
		return fmt.Errorf("creating section dataset: %w", err)
	}
	columns := dset.Columns()

	var predicates []dataset.Predicate
	if p := translateStreamsPredicate(r.predicate, columns, r.sec.Columns()); p != nil {
		predicates = append(predicates, p)
	}

	readerOpts := dataset.RowReaderOptions{
		Dataset:    dset,
		Columns:    columns,
		Predicates: predicates,
		Prefetch:   true,
	}

	if r.reader == nil {
		r.reader = dataset.NewRowReader(readerOpts)
	} else {
		r.reader.Reset(readerOpts)
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
	r.predicate = nil
	r.ready = false
	r.columns = nil

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
