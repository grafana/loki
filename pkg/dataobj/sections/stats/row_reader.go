package stats

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// RowReader reads typed [Stat] rows from a stats [Section].
type RowReader struct {
	sec   *Section
	ready bool

	buf     []dataset.Row
	reader  *dataset.RowReader
	columns []dataset.Column
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

// Read reads up to the next len(s) stats from the reader and stores them
// into s. It returns the number of stats read and any error encountered. At
// the end of the stats section, Read returns 0, io.EOF.
func (r *RowReader) Read(ctx context.Context, s []Stat) (int, error) {
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
		if err := decodeRow(r.sec.Columns(), r.buf[i], &s[i]); err != nil {
			return i, fmt.Errorf("decoding stat: %w", err)
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
}

// Close closes the RowReader and releases any resources it holds. Closed
// RowReaders can be reused by calling [RowReader.Reset].
func (r *RowReader) Close() error {
	if r.reader != nil {
		return r.reader.Close()
	}
	return nil
}

// decodeRow decodes a dataset.Row into a Stat. Column values are matched by
// position using the Section's column list.
func decodeRow(columns []*Column, row dataset.Row, s *Stat) error {
	// First pass: read fixed columns and discover sort schema for label allocation.
	for columnIndex, val := range row.Values {
		if columnIndex >= len(columns) {
			break
		}
		column := columns[columnIndex]
		switch column.Type {
		case ColumnTypeObjectPath:
			s.ObjectPath = string(val.Binary())
		case ColumnTypeSectionIndex:
			s.SectionIndex = val.Int64()
		case ColumnTypeSortSchema:
			s.SortSchema = string(val.Binary())
		case ColumnTypeMinTimestamp:
			s.MinTimestamp = val.Int64()
		case ColumnTypeMaxTimestamp:
			s.MaxTimestamp = val.Int64()
		case ColumnTypeRowCount:
			s.RowCount = val.Int64()
		case ColumnTypeUncompressedSize:
			s.UncompressedSize = val.Int64()
		}
	}

	// Second pass: collect dynamic label columns.
	// Labels are columns with type ColumnTypeLabel; Name holds the label key.
	if s.Labels == nil {
		// Determine how many label columns there are.
		var labelCount int
		for _, col := range columns {
			if col.Type == ColumnTypeLabel {
				labelCount++
			}
		}
		if labelCount > 0 {
			s.Labels = make(map[string]string, labelCount)
		}
	}

	// Discover label keys from SortSchema (for reference), but use column metadata for assignment.
	_ = strings.Split(s.SortSchema, ",") // validates SortSchema usage; actual keys come from column.Name

	for columnIndex, val := range row.Values {
		if columnIndex >= len(columns) {
			break
		}
		column := columns[columnIndex]
		if column.Type == ColumnTypeLabel {
			if !val.IsNil() {
				if s.Labels == nil {
					s.Labels = make(map[string]string)
				}
				s.Labels[column.Name] = string(val.Binary())
			}
		}
	}

	return nil
}
