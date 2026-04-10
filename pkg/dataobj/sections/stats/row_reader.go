package stats

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/grafana/loki/v3/pkg/columnar"
)

// RowReader reads typed [Stat] rows from a stats [Section].
//
// RowReader wraps a [Reader] to provide typed [Stat] values instead of
// raw columnar arrays.
type RowReader struct {
	reader *Reader

	// off tracks the current row offset (for EOF detection).
	off int
}

// NewRowReader creates a new RowReader that reads typed [Stat] rows from the
// provided [Section].
func NewRowReader(sec *Section) (*RowReader, error) {
	r, err := NewReader(sec)
	if err != nil {
		return nil, fmt.Errorf("creating reader: %w", err)
	}
	return &RowReader{reader: r}, nil
}

// Read reads up to len(s) stats from the reader into s. It returns the number
// of stats read and any error encountered. At the end of the section, Read
// returns 0, [io.EOF].
func (r *RowReader) Read(ctx context.Context, s []Stat) (int, error) {
	if r.off >= r.reader.RowCount() {
		return 0, io.EOF
	}

	n := len(s)
	if n == 0 {
		return 0, nil
	}

	// Read all fixed columns for the requested batch.
	objectPaths, err := r.reader.readStringColumn(ctx, colObjectPath, n)
	if err != nil && err != io.EOF {
		return 0, fmt.Errorf("reading object_path: %w", err)
	}
	sectionIndexes, err := r.reader.readInt64Column(ctx, colSectionIndex, n)
	if err != nil && err != io.EOF {
		return 0, fmt.Errorf("reading section_index: %w", err)
	}
	sortSchemas, err := r.reader.readStringColumn(ctx, colSortSchema, n)
	if err != nil && err != io.EOF {
		return 0, fmt.Errorf("reading sort_schema: %w", err)
	}
	minTimestamps, err := r.reader.readInt64Column(ctx, colMinTimestamp, n)
	if err != nil && err != io.EOF {
		return 0, fmt.Errorf("reading min_timestamp: %w", err)
	}
	maxTimestamps, err := r.reader.readInt64Column(ctx, colMaxTimestamp, n)
	if err != nil && err != io.EOF {
		return 0, fmt.Errorf("reading max_timestamp: %w", err)
	}
	rowCounts, err := r.reader.readInt64Column(ctx, colRowCount, n)
	if err != nil && err != io.EOF {
		return 0, fmt.Errorf("reading row_count: %w", err)
	}
	uncompressedSizes, err := r.reader.readInt64Column(ctx, colUncompressedSize, n)
	if err != nil && err != io.EOF {
		return 0, fmt.Errorf("reading uncompressed_size: %w", err)
	}

	// Discover label column names from the sort_schema of the first row.
	var keys []string
	if len(sortSchemas) > 0 && sortSchemas[0] != "" {
		for _, k := range strings.Split(sortSchemas[0], ",") {
			if k != "" {
				keys = append(keys, k)
			}
		}
	}

	// Read each label column.
	labelColumns := make(map[string][]string, len(keys))
	for _, key := range keys {
		vals, err := r.reader.readStringColumn(ctx, key, n)
		if err != nil && err != io.EOF {
			return 0, fmt.Errorf("reading label column %q: %w", key, err)
		}
		labelColumns[key] = vals
	}

	// All columns should have the same length. Clamp to len(s) so callers
	// passing a smaller destination buffer don't trigger an out-of-bounds write.
	read := min(len(objectPaths), len(s))
	for i := range read {
		labels := make(map[string]string, len(keys))
		for _, key := range keys {
			if i < len(labelColumns[key]) {
				labels[key] = labelColumns[key][i]
			}
		}
		s[i] = Stat{
			ObjectPath:       objectPaths[i],
			SectionIndex:     sectionIndexes[i],
			SortSchema:       sortSchemas[i],
			Labels:           labels,
			MinTimestamp:     minTimestamps[i],
			MaxTimestamp:     maxTimestamps[i],
			RowCount:         rowCounts[i],
			UncompressedSize: uncompressedSizes[i],
		}
	}

	r.off += read
	if r.off >= r.reader.RowCount() {
		return read, io.EOF
	}
	return read, nil
}

// Close closes the RowReader and releases any resources it holds.
func (r *RowReader) Close() error {
	return r.reader.Close()
}

// extractInt64Values extracts int64 values from a columnar.Array.
func extractInt64Values(arr columnar.Array) ([]int64, error) {
	numArr, ok := arr.(*columnar.Number[int64])
	if !ok {
		return nil, fmt.Errorf("expected *columnar.Number[int64], got %T", arr)
	}
	values := numArr.Values()
	result := make([]int64, len(values))
	copy(result, values)
	return result, nil
}

// extractStringValues extracts string values from a columnar.Array.
func extractStringValues(arr columnar.Array) ([]string, error) {
	utf8Arr, ok := arr.(*columnar.UTF8)
	if !ok {
		return nil, fmt.Errorf("expected *columnar.UTF8, got %T", arr)
	}
	result := make([]string, utf8Arr.Len())
	for i := range utf8Arr.Len() {
		result[i] = string(utf8Arr.Get(i))
	}
	return result, nil
}
