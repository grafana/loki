package indexpointers

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/symbolizer"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// RowReader is a reader for index pointers in a data object.
type RowReader struct {
	sec   *Section
	ready bool

	predicate RowPredicate

	buf []dataset.Row

	reader  *dataset.RowReader
	columns []dataset.Column

	symbols *symbolizer.Symbolizer
}

var errRowReaderNotOpen = errors.New("row reader not opened")

// NewRowReader creates a new RowReader for the given section.
//
// Call [RowReader.Open] before calling [RowReader.Read].
func NewRowReader(sec *Section) *RowReader {
	var r RowReader
	r.Reset(sec)
	return &r
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
		if err := decodeRow(r.sec.Columns(), r.buf[i], &s[i], r.symbols); err != nil {
			return i, fmt.Errorf("decoding stream: %w", err)
		}
	}

	return n, nil
}

func (r *RowReader) initReader(ctx context.Context) error {
	dset, err := columnar.MakeDataset(r.sec.inner, r.sec.inner.Columns())
	if err != nil {
		return fmt.Errorf("creating section dataset: %w", err)
	}
	columns := dset.Columns()

	var predicates []dataset.Predicate
	if p := translateIndexPointersPredicate(r.predicate, columns); p != nil {
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
	r.predicate = nil
	r.ready = false
	r.columns = nil

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
		if desc.ColumnDesc().Tag == "min_timestamp" {
			minTimestampColumn = desc
		}
		if desc.ColumnDesc().Tag == "max_timestamp" {
			maxTimestampColumn = desc
		}
	}

	switch p := p.(type) {
	case TimeRangeRowPredicate:
		return convertTimeRangePredicate(p, minTimestampColumn, maxTimestampColumn)

	default:
		panic(fmt.Sprintf("unsupported predicate type %T", p))
	}
}

func convertTimeRangePredicate(p TimeRangeRowPredicate, minTimestampColumn, maxTimestampColumn dataset.Column) dataset.Predicate {
	return dataset.AndPredicate{
		Left: dataset.GreaterThanPredicate{
			Column: maxTimestampColumn,
			Value:  dataset.Int64Value(p.Start.UnixNano() - 1),
		},
		Right: dataset.LessThanPredicate{
			Column: minTimestampColumn,
			Value:  dataset.Int64Value(p.End.UnixNano() + 1),
		},
	}
}
