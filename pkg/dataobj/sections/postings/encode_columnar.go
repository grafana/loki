package postings

import (
	"context"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

// ColumnarEncoder encodes Posting rows using columnar primitives.
func ColumnarEncoder(_ context.Context, rows []Posting) (Section, error) {
	var alloc memory.Allocator
	buildAlloc := memory.NewAllocator(&alloc)
	defer buildAlloc.Free()

	kindBuilder := columnar.NewNumberBuilder[int64](buildAlloc)
	objectPathBuilder := columnar.NewUTF8Builder(buildAlloc)
	sectionIndexBuilder := columnar.NewNumberBuilder[int64](buildAlloc)
	columnNameBuilder := columnar.NewUTF8Builder(buildAlloc)
	labelValueBuilder := columnar.NewUTF8Builder(buildAlloc)
	bloomFilterBuilder := columnar.NewUTF8Builder(buildAlloc)
	streamIDBitmapBuilder := columnar.NewUTF8Builder(buildAlloc)
	uncompressedSizeBuilder := columnar.NewNumberBuilder[int64](buildAlloc)
	minTSBuilder := columnar.NewNumberBuilder[int64](buildAlloc)
	maxTSBuilder := columnar.NewNumberBuilder[int64](buildAlloc)

	// Normalize stream ID bitmaps to the same length.
	normalizedBitmaps := columnarNormalizeBitmaps(rows)

	for i, r := range rows {
		kindBuilder.AppendValue(int64(r.Kind))
		objectPathBuilder.AppendValue([]byte(r.ObjectPath))
		sectionIndexBuilder.AppendValue(r.SectionIndex)
		columnNameBuilder.AppendValue([]byte(r.ColumnName))

		if r.Kind == KindBloom {
			labelValueBuilder.AppendNull()
		} else {
			labelValueBuilder.AppendValue([]byte(r.LabelValue))
		}

		if r.BloomFilter == nil {
			bloomFilterBuilder.AppendNull()
		} else {
			bloomFilterBuilder.AppendValue(r.BloomFilter)
		}

		streamIDBitmapBuilder.AppendValue(normalizedBitmaps[i])
		uncompressedSizeBuilder.AppendValue(r.UncompressedSize)
		minTSBuilder.AppendValue(r.MinTimestamp)
		maxTSBuilder.AppendValue(r.MaxTimestamp)
	}

	// Column order must match the order used by DatasetEncoder.
	columnOrder := []string{
		colKind, colObjectPath, colSectionIndex, colColumnName, colLabelValue,
		colBloomFilter, colStreamIDBitmap, colUncompressedSize, colMinTimestamp, colMaxTimestamp,
	}

	columns := map[string]columnar.Array{
		colKind:             kindBuilder.Build(),
		colObjectPath:       objectPathBuilder.Build(),
		colSectionIndex:     sectionIndexBuilder.Build(),
		colColumnName:       columnNameBuilder.Build(),
		colLabelValue:       labelValueBuilder.Build(),
		colBloomFilter:      bloomFilterBuilder.Build(),
		colStreamIDBitmap:   streamIDBitmapBuilder.Build(),
		colUncompressedSize: uncompressedSizeBuilder.Build(),
		colMinTimestamp:     minTSBuilder.Build(),
		colMaxTimestamp:     maxTSBuilder.Build(),
	}

	return Section{
		ColumnNames: columnOrder,
		RowCount:    len(rows),
		OpenColumn: func(name string) (ColumnReader, error) {
			arr, ok := columns[name]
			if !ok {
				return nil, fmt.Errorf("column %q not found", name)
			}
			return &columnarSliceColumnReader{arr: arr}, nil
		},
	}, nil
}

// columnarNormalizeBitmaps pads all stream ID bitmaps to the same length (the
// maximum length) with zero bytes.
func columnarNormalizeBitmaps(rows []Posting) [][]byte {
	maxLen := 0
	for _, r := range rows {
		if len(r.StreamIDBitmap) > maxLen {
			maxLen = len(r.StreamIDBitmap)
		}
	}

	result := make([][]byte, len(rows))
	for i, r := range rows {
		if len(r.StreamIDBitmap) == maxLen {
			result[i] = r.StreamIDBitmap
		} else {
			padded := make([]byte, maxLen)
			copy(padded, r.StreamIDBitmap)
			result[i] = padded
		}
	}
	return result
}

// columnarSliceColumnReader wraps a single columnar.Array as a ColumnReader.
// Returns the full array on the first Read call, then io.EOF.
type columnarSliceColumnReader struct {
	arr  columnar.Array
	read bool
}

func (r *columnarSliceColumnReader) Read(_ context.Context, _ int) (columnar.Array, error) {
	if r.read {
		return nil, io.EOF
	}
	r.read = true
	return r.arr, nil
}

func (r *columnarSliceColumnReader) Close() error { return nil }
