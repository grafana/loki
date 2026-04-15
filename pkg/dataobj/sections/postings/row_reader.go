package postings

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
	sectionscolumnar "github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// RowReader reads the set of postings from a [Section].
type RowReader struct {
	sec   *Section
	ready bool

	buf     []dataset.Row
	reader  *dataset.RowReader
	columns []dataset.Column

	// colIndex maps each Section Column index to the corresponding dataset column index.
	colIndex []int
}

var errRowReaderNotOpen = errors.New("row reader not opened")

// NewRowReader creates a new RowReader that reads rows from the provided
// [Section].
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

// Read reads up to the next len(p) postings from the reader and stores them
// into p. It returns the number of postings read and any error encountered. At
// the end of the postings section, Read returns 0, io.EOF.
func (r *RowReader) Read(ctx context.Context, p []Posting) (int, error) {
	if r.sec == nil {
		return 0, io.EOF
	}

	if !r.ready {
		return 0, errRowReaderNotOpen
	}

	r.buf = slicegrow.GrowToCap(r.buf, len(p))
	r.buf = r.buf[:len(p)]
	n, err := r.reader.Read(ctx, r.buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, fmt.Errorf("reading rows: %w", err)
	} else if n == 0 && errors.Is(err, io.EOF) {
		return 0, io.EOF
	}

	for i := range r.buf[:n] {
		if err := r.decodeRow(r.buf[i], &p[i]); err != nil {
			return i, fmt.Errorf("decoding posting: %w", err)
		}
	}

	return n, nil
}

func (r *RowReader) initReader(ctx context.Context) error {
	dset, err := sectionscolumnar.MakeDataset(r.sec.inner, r.sec.inner.Columns())
	if err != nil {
		return fmt.Errorf("creating section dataset: %w", err)
	}
	columns := dset.Columns()

	readerOpts := dataset.RowReaderOptions{
		Dataset:  dset,
		Columns:  columns,
		Prefetch: true,
	}

	if r.reader == nil {
		r.reader = dataset.NewRowReader(readerOpts)
	} else {
		r.reader.Reset(readerOpts)
	}
	if err := r.reader.Open(ctx); err != nil {
		return fmt.Errorf("opening row reader: %w", err)
	}

	// Build the mapping from Section Column index → dataset column index.
	// The dataset includes ALL columns from s.inner.Columns(), in order.
	// r.sec.columns only contains RECOGNIZED columns (unrecognized ones were skipped in init).
	// We need to map each recognized column to its position in the dataset columns slice.
	//
	// The inner columnar columns and the dataset columns are in the same order,
	// so we find the position of each recognized column's inner column in the
	// full columnar column list.
	allInnerCols := r.sec.inner.Columns()
	r.colIndex = make([]int, len(r.sec.columns))
	for i, secCol := range r.sec.columns {
		for j, innerCol := range allInnerCols {
			if innerCol == secCol.inner {
				r.colIndex[i] = j
				break
			}
		}
	}

	r.columns = columns
	r.ready = true
	return nil
}

// Reset resets the RowReader with a new section to read from. Reset allows
// reusing a RowReader without allocating a new one.
//
// Reset may be called with a nil section to clear the RowReader.
func (r *RowReader) Reset(sec *Section) {
	r.sec = sec
	r.ready = false
	r.columns = nil
	r.colIndex = nil

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

// decodeRow decodes a dataset.Row into a Posting using the column mapping built
// during initReader.
func (r *RowReader) decodeRow(row dataset.Row, p *Posting) error {
	for i, col := range r.sec.columns {
		idx := r.colIndex[i]
		val := row.Values[idx]
		switch col.Type {
		case ColumnTypeKind:
			p.Kind = PostingKind(val.Int64())
		case ColumnTypeObjectPath:
			p.ObjectPath = string(val.Binary())
		case ColumnTypeSectionIndex:
			p.SectionIndex = val.Int64()
		case ColumnTypeColumnName:
			p.ColumnName = string(val.Binary())
		case ColumnTypeLabelValue:
			if val.IsNil() {
				p.LabelValue = ""
			} else {
				p.LabelValue = string(val.Binary())
			}
		case ColumnTypeBloomFilter:
			if val.IsNil() {
				p.BloomFilter = nil
			} else {
				b := val.Binary()
				p.BloomFilter = make([]byte, len(b))
				copy(p.BloomFilter, b)
			}
		case ColumnTypeStreamIDBitmap:
			if val.IsNil() {
				p.StreamIDBitmap = nil
			} else {
				b := val.Binary()
				p.StreamIDBitmap = make([]byte, len(b))
				copy(p.StreamIDBitmap, b)
			}
		case ColumnTypeUncompressedSize:
			p.UncompressedSize = val.Int64()
		case ColumnTypeMinTimestamp:
			p.MinTimestamp = val.Int64()
		case ColumnTypeMaxTimestamp:
			p.MaxTimestamp = val.Int64()
		}
	}
	return nil
}
