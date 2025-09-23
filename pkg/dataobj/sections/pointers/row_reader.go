package pointers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"maps"

	"github.com/bits-and-blooms/bloom/v3"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/symbolizer"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// RowReader reads the set of streams from an [Object].
type RowReader struct {
	sec   *Section
	ready bool

	matchIDs map[int64]struct{}

	predicate RowPredicate

	buf []dataset.Row

	reader  *dataset.Reader
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
func (r *RowReader) Read(ctx context.Context, s []SectionPointer) (int, error) {
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
		if err := decodeRow(r.sec.Columns(), r.buf[i], &s[i], r.symbols); err != nil {
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
	if len(r.matchIDs) > 0 {
		predicates = append(predicates, streamIDPredicate(maps.Keys(r.matchIDs), columns, r.sec.Columns()))
	}

	if p := translatePointersPredicate(r.predicate, columns, r.sec.Columns()); p != nil {
		predicates = append(predicates, p)
	}

	readerOpts := dataset.ReaderOptions{
		Dataset:    dset,
		Columns:    columns,
		Predicates: predicates,
		Prefetch:   true,
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

func streamIDPredicate(ids iter.Seq[int64], dsetColumns []dataset.Column, actualColumns []*Column) dataset.Predicate {
	streamIDColumn := findDatasetColumn(dsetColumns, actualColumns, func(desc *Column) bool {
		return desc.Type == ColumnTypeStreamID
	})
	if streamIDColumn == nil {
		return dataset.FalsePredicate{}
	}

	var values []dataset.Value
	for i := range ids {
		values = append(values, dataset.Int64Value(i))
	}

	return dataset.InPredicate{
		Column: streamIDColumn,
		Values: dataset.NewInt64ValueSet(values),
	}
}

func translatePointersPredicate(p RowPredicate, dsetColumns []dataset.Column, actualColumns []*Column) dataset.Predicate {
	if p == nil {
		return nil
	}

	nameColumn := findDatasetColumn(dsetColumns, actualColumns, func(desc *Column) bool {
		return desc.Type == ColumnTypeColumnName
	})

	bloomColumn := findDatasetColumn(dsetColumns, actualColumns, func(desc *Column) bool {
		return desc.Type == ColumnTypeValuesBloomFilter
	})

	startColumn := findDatasetColumn(dsetColumns, actualColumns, func(desc *Column) bool {
		return desc.Type == ColumnTypeMinTimestamp
	})

	endColumn := findDatasetColumn(dsetColumns, actualColumns, func(desc *Column) bool {
		return desc.Type == ColumnTypeMaxTimestamp
	})

	switch p := p.(type) {
	case AndRowPredicate:
		return dataset.AndPredicate{
			Left:  translatePointersPredicate(p.Left, dsetColumns, actualColumns),
			Right: translatePointersPredicate(p.Right, dsetColumns, actualColumns),
		}
	case BloomExistenceRowPredicate:
		return convertBloomExistenceRowPredicate(p, nameColumn, bloomColumn)
	case TimeRangeRowPredicate:
		return convertTimeRangeRowPredicate(p, startColumn, endColumn)
	default:
		panic(fmt.Sprintf("unsupported predicate type %T", p))
	}
}

func convertBloomExistenceRowPredicate(p BloomExistenceRowPredicate, nameColumn, bloomColumn dataset.Column) dataset.Predicate {
	if nameColumn == nil && bloomColumn == nil {
		// If there are no name or bloom columns present, it means this section doesn't have any relevant columns for this predicate.
		// This can happen if a whole section only contains pointers of a different Kind.
		return dataset.FalsePredicate{}
	}

	// TODO: Make this more efficient by not re-allocating the bloom filter each time
	return dataset.AndPredicate{
		Left: dataset.EqualPredicate{
			Column: nameColumn,
			Value:  dataset.BinaryValue([]byte(p.Name)),
		},
		Right: dataset.FuncPredicate{
			Column: bloomColumn,
			Keep: func(_ dataset.Column, value dataset.Value) bool {
				bloomBytes := value.Binary()
				bf := bloom.New(1, 1) // Dummy values
				_, err := bf.ReadFrom(bytes.NewReader(bloomBytes))
				if err != nil {
					// If the bloom filter is invalid, we assume it would pass.
					return true
				}
				return bf.TestString(p.Value)
			},
		},
	}
}

func convertTimeRangeRowPredicate(p TimeRangeRowPredicate, startColumn, endColumn dataset.Column) dataset.Predicate {
	return dataset.AndPredicate{
		Left: dataset.GreaterThanPredicate{
			Column: endColumn,
			Value:  dataset.Int64Value(p.Start.UnixNano() - 1),
		},
		Right: dataset.LessThanPredicate{
			Column: startColumn,
			Value:  dataset.Int64Value(p.End.UnixNano() + 1),
		},
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
