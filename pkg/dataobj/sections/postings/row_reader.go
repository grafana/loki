package postings

import (
	"context"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/columnar"
)

// RowReader reads typed [Posting] rows from a postings [Section].
//
// RowReader wraps a [Reader] to provide typed [Posting] values instead of
// raw columnar arrays.
type RowReader struct {
	reader *Reader

	// off tracks the current row offset (for EOF detection).
	off int
}

// NewRowReader creates a new RowReader that reads typed [Posting] rows from the
// provided [Section].
func NewRowReader(sec *Section) (*RowReader, error) {
	r, err := NewReader(sec)
	if err != nil {
		return nil, fmt.Errorf("creating reader: %w", err)
	}
	return &RowReader{reader: r}, nil
}

// Read reads up to len(p) postings from the reader into p. It returns the
// number of postings read and any error encountered. At the end of the
// section, Read returns 0, [io.EOF].
func (r *RowReader) Read(ctx context.Context, p []Posting) (int, error) {
	if r.off >= r.reader.RowCount() {
		return 0, io.EOF
	}

	n := len(p)
	if n == 0 {
		return 0, nil
	}

	// Read all columns for the requested batch.
	kinds, err := r.reader.readInt64Column(ctx, colKind, n)
	if err != nil && err != io.EOF {
		return 0, fmt.Errorf("reading kind: %w", err)
	}
	objectPaths, err := r.reader.readStringColumn(ctx, colObjectPath, n)
	if err != nil && err != io.EOF {
		return 0, fmt.Errorf("reading object_path: %w", err)
	}
	sectionIndexes, err := r.reader.readInt64Column(ctx, colSectionIndex, n)
	if err != nil && err != io.EOF {
		return 0, fmt.Errorf("reading section_index: %w", err)
	}
	columnNames, err := r.reader.readStringColumn(ctx, colColumnName, n)
	if err != nil && err != io.EOF {
		return 0, fmt.Errorf("reading column_name: %w", err)
	}
	labelValues, err := r.reader.readStringColumn(ctx, colLabelValue, n)
	if err != nil && err != io.EOF {
		return 0, fmt.Errorf("reading label_value: %w", err)
	}
	bloomFilters, err := r.reader.readBytesColumn(ctx, colBloomFilter, n)
	if err != nil && err != io.EOF {
		return 0, fmt.Errorf("reading bloom_filter: %w", err)
	}
	streamIDBitmaps, err := r.reader.readBytesColumn(ctx, colStreamIDBitmap, n)
	if err != nil && err != io.EOF {
		return 0, fmt.Errorf("reading stream_id_bitmap: %w", err)
	}
	uncompressedSizes, err := r.reader.readInt64Column(ctx, colUncompressedSize, n)
	if err != nil && err != io.EOF {
		return 0, fmt.Errorf("reading uncompressed_size: %w", err)
	}
	minTimestamps, err := r.reader.readInt64Column(ctx, colMinTimestamp, n)
	if err != nil && err != io.EOF {
		return 0, fmt.Errorf("reading min_timestamp: %w", err)
	}
	maxTimestamps, err := r.reader.readInt64Column(ctx, colMaxTimestamp, n)
	if err != nil && err != io.EOF {
		return 0, fmt.Errorf("reading max_timestamp: %w", err)
	}

	// All columns should have the same length. Clamp to len(p) so callers
	// passing a smaller destination buffer don't trigger an out-of-bounds write.
	read := min(len(kinds), len(p))
	for i := range read {
		p[i] = Posting{
			Kind:             PostingKind(kinds[i]),
			ObjectPath:       objectPaths[i],
			SectionIndex:     sectionIndexes[i],
			ColumnName:       columnNames[i],
			LabelValue:       labelValues[i],
			BloomFilter:      bloomFilters[i],
			StreamIDBitmap:   streamIDBitmaps[i],
			UncompressedSize: uncompressedSizes[i],
			MinTimestamp:     minTimestamps[i],
			MaxTimestamp:     maxTimestamps[i],
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

// extractStringValues extracts string values from a columnar.Array (non-nullable).
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

// extractBytesValues extracts byte slices from a columnar.Array (binary or
// nullable binary). Null entries are returned as nil slices.
func extractBytesValues(arr columnar.Array) ([][]byte, error) {
	utf8Arr, ok := arr.(*columnar.UTF8)
	if !ok {
		return nil, fmt.Errorf("expected *columnar.UTF8, got %T", arr)
	}
	result := make([][]byte, utf8Arr.Len())
	for i := range utf8Arr.Len() {
		if utf8Arr.IsNull(i) {
			result[i] = nil
		} else {
			val := utf8Arr.Get(i)
			copied := make([]byte, len(val))
			copy(copied, val)
			result[i] = copied
		}
	}
	return result, nil
}
