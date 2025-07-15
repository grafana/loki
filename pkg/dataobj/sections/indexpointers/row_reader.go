package indexpointers

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/indexpointersmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/symbolizer"
)

// RowReader is a reader for index pointers in a data object.
type RowReader struct {
	sec   *Section
	ready bool

	predicate RowPredicate

	buf []dataset.Row

	reader     *dataset.Reader
	columns    []dataset.Column
	columnDesc []*indexpointersmd.ColumnDesc

	symbols *symbolizer.Symbolizer
}

// NewRowReader creates a new RowReader for the given section.
func NewRowReader(sec *Section) *RowReader {
	var r RowReader
	r.Reset(sec)
	return &r
}

// SetPredicate sets the predicate to use for filtering indexpointers. [RowReader.Read]
// will only return indexpointers for which the predicate passes.
//
// SetPredicate returns an error if the predicate is not supported by
// RowReader.
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

// Read reads up to the next len(s) indexpointers from the reader and stores them
// into s. It returns the number of indexpointers read and any error encountered. At
// the end of the indexpointers section, Read returns 0, io.EOF.
func (r *RowReader) Read(ctx context.Context, s []IndexPointer) (int, error) {
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
		if err := decodeRow(r.columnDesc, r.buf[i], &s[i], r.symbols); err != nil {
			return i, fmt.Errorf("decoding stream: %w", err)
		}
	}

	return n, nil
}

func (r *RowReader) initReader(ctx context.Context) error {
	dec := newDecoder(r.sec.reader)

	columnDescs, err := dec.Columns(ctx)
	if err != nil {
		return fmt.Errorf("reading columns: %w", err)
	}

	dset, err := newColumnsDataset(r.sec.Columns())
	if err != nil {
		return fmt.Errorf("creating section dataset: %w", err)
	}
	columns := dset.Columns()

	var predicates []dataset.Predicate
	if p := translateIndexPointersPredicate(r.predicate, columns); p != nil {
		predicates = append(predicates, p)
	}

	readerOpts := dataset.ReaderOptions{
		Dataset:    dset,
		Columns:    columns,
		Predicates: predicates,

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
	r.columnDesc = nil

	if r.symbols != nil {
		r.symbols.Reset()
	}
}

// Close closes the RowReader and releases any resources it holds. Closed
// RowReaders can be reused by calling [RowReader.Reset].
func (r *RowReader) Close() error {
	if r.reader != nil {
		return r.reader.Close()
	}
	return nil
}

func translateIndexPointersPredicate(p RowPredicate, columns []dataset.Column) dataset.Predicate {
	if p == nil {
		return nil
	}

	var minTimestampColumn dataset.Column
	var maxTimestampColumn dataset.Column
	for _, desc := range columns {
		if desc.ColumnInfo().Name == "min_timestamp" {
			minTimestampColumn = desc
		}
		if desc.ColumnInfo().Name == "max_timestamp" {
			maxTimestampColumn = desc
		}
	}

	switch p := p.(type) {
	case TimeRangePredicate:
		return convertTimeRangePredicate(p, minTimestampColumn, maxTimestampColumn)

	default:
		panic(fmt.Sprintf("unsupported predicate type %T", p))
	}
}

func convertTimeRangePredicate(p TimeRangePredicate, minTimestampColumn, maxTimestampColumn dataset.Column) dataset.Predicate {
	return dataset.AndPredicate{
		Left: dataset.OrPredicate{
			Left: dataset.EqualPredicate{
				Column: minTimestampColumn,
				Value:  dataset.Int64Value(p.MinTimestamp.Unix()),
			},
			Right: dataset.GreaterThanPredicate{
				Column: minTimestampColumn,
				Value:  dataset.Int64Value(p.MinTimestamp.Unix()),
			},
		},
		Right: dataset.OrPredicate{
			Left: dataset.EqualPredicate{
				Column: maxTimestampColumn,
				Value:  dataset.Int64Value(p.MaxTimestamp.Unix()),
			},
			Right: dataset.LessThanPredicate{
				Column: maxTimestampColumn,
				Value:  dataset.Int64Value(p.MaxTimestamp.Unix()),
			},
		},
	}

}
