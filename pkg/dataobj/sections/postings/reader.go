package postings

import (
	"context"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/dataset/array"
	"github.com/grafana/loki/v3/pkg/memory"
)

// Reader reads columnar data from a postings [Section].
//
// Use [RowReader] for higher-level access to typed [Posting] rows.
type Reader struct {
	sec   *Section
	alloc *memory.Allocator

	// Per-column readers, keyed by column name index (matching Section.Columns order).
	readers  []array.Reader
	rowCount int
}

// NewReader creates a new Reader that reads from the provided [Section].
func NewReader(sec *Section) (*Reader, error) {
	if sec == nil {
		return nil, fmt.Errorf("section must not be nil")
	}

	var alloc memory.Allocator

	readers := make([]array.Reader, len(sec.Columns))
	var rowCount int

	for i, col := range sec.Columns {
		r, err := array.NewReader(&alloc, col.Array, sec.Store)
		if err != nil {
			// Close any readers already opened.
			for j := 0; j < i; j++ {
				_ = readers[j].Close()
			}
			return nil, fmt.Errorf("creating reader for column %q: %w", col.Name, err)
		}
		readers[i] = r
		rowCount = col.Array.Stats.RowCount
	}

	return &Reader{
		sec:      sec,
		alloc:    &alloc,
		readers:  readers,
		rowCount: rowCount,
	}, nil
}

// RowCount returns the total number of rows in the section.
func (r *Reader) RowCount() int { return r.rowCount }

// columnIndex returns the index of the named column, or -1 if not found.
func (r *Reader) columnIndex(name string) int {
	for i, col := range r.sec.Columns {
		if col.Name == name {
			return i
		}
	}
	return -1
}

// readInt64Column reads up to count values from a named int64 column.
func (r *Reader) readInt64Column(ctx context.Context, name string, count int) ([]int64, error) {
	idx := r.columnIndex(name)
	if idx < 0 {
		return nil, fmt.Errorf("column %q not found", name)
	}
	arr, err := r.readers[idx].Read(ctx, r.alloc, count)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("reading column %q: %w", name, err)
	}
	if arr == nil {
		return nil, io.EOF
	}
	return extractInt64Values(arr)
}

// readStringColumn reads up to count values from a named UTF8 column.
func (r *Reader) readStringColumn(ctx context.Context, name string, count int) ([]string, error) {
	idx := r.columnIndex(name)
	if idx < 0 {
		return nil, fmt.Errorf("column %q not found", name)
	}
	arr, err := r.readers[idx].Read(ctx, r.alloc, count)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("reading column %q: %w", name, err)
	}
	if arr == nil {
		return nil, io.EOF
	}
	return extractStringValues(arr)
}

// readNullableStringColumn reads up to count values from a named nullable UTF8
// column. Null entries are returned as nil pointers.
func (r *Reader) readNullableStringColumn(ctx context.Context, name string, count int) ([]*string, error) {
	idx := r.columnIndex(name)
	if idx < 0 {
		return nil, fmt.Errorf("column %q not found", name)
	}
	arr, err := r.readers[idx].Read(ctx, r.alloc, count)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("reading column %q: %w", name, err)
	}
	if arr == nil {
		return nil, io.EOF
	}
	return extractNullableStringValues(arr)
}

// readBytesColumn reads up to count values from a named binary column.
// Null entries are returned as nil slices.
func (r *Reader) readBytesColumn(ctx context.Context, name string, count int) ([][]byte, error) {
	idx := r.columnIndex(name)
	if idx < 0 {
		return nil, fmt.Errorf("column %q not found", name)
	}
	arr, err := r.readers[idx].Read(ctx, r.alloc, count)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("reading column %q: %w", name, err)
	}
	if arr == nil {
		return nil, io.EOF
	}
	return extractBytesValues(arr)
}

// Close closes the reader and releases any resources.
func (r *Reader) Close() error {
	var firstErr error
	for _, rdr := range r.readers {
		if err := rdr.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
