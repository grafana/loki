package stats

import (
	"context"
	"fmt"
	"io"
)

// Reader reads columnar data from a stats [Section].
//
// Use [RowReader] for higher-level access to typed [Stat] rows.
type Reader struct {
	sec      *Section
	readers  map[string]ColumnReader
	rowCount int
}

// NewReader creates a new Reader that reads from the provided [Section].
func NewReader(sec *Section) (*Reader, error) {
	if sec == nil {
		return nil, fmt.Errorf("section must not be nil")
	}
	return &Reader{
		sec:      sec,
		readers:  make(map[string]ColumnReader),
		rowCount: sec.RowCount,
	}, nil
}

// RowCount returns the total number of rows in the section.
func (r *Reader) RowCount() int { return r.rowCount }

// getOrOpenColumn lazily opens and caches a ColumnReader for the named column.
func (r *Reader) getOrOpenColumn(name string) (ColumnReader, error) {
	if cr, ok := r.readers[name]; ok {
		return cr, nil
	}
	cr, err := r.sec.OpenColumn(name)
	if err != nil {
		return nil, err
	}
	r.readers[name] = cr
	return cr, nil
}

// readInt64Column reads up to count values from a named int64 column.
// Returns the values slice and the number of values read.
func (r *Reader) readInt64Column(ctx context.Context, name string, count int) ([]int64, error) {
	cr, err := r.getOrOpenColumn(name)
	if err != nil {
		return nil, fmt.Errorf("column %q not found: %w", name, err)
	}
	arr, err := cr.Read(ctx, count)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("reading column %q: %w", name, err)
	}
	if arr == nil {
		return nil, io.EOF
	}
	return extractInt64Values(arr)
}

// readStringColumn reads up to count values from a named UTF8 column.
// Returns the string slice and the number of values read.
func (r *Reader) readStringColumn(ctx context.Context, name string, count int) ([]string, error) {
	cr, err := r.getOrOpenColumn(name)
	if err != nil {
		return nil, fmt.Errorf("column %q not found: %w", name, err)
	}
	arr, err := cr.Read(ctx, count)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("reading column %q: %w", name, err)
	}
	if arr == nil {
		return nil, io.EOF
	}
	return extractStringValues(arr)
}

// Close closes the reader and releases any resources.
func (r *Reader) Close() error {
	var firstErr error
	for _, cr := range r.readers {
		if err := cr.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
