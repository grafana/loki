package stats

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

// ColumnarEncoder encodes Stat rows using the existing columnar primitives.
// This is an alternative to DatasetEncoder for use without the new dataset API.
func ColumnarEncoder(_ context.Context, rows []Stat) (Section, error) {
	var alloc memory.Allocator
	buildAlloc := memory.NewAllocator(&alloc)
	defer buildAlloc.Free()

	objectPathBuilder := columnar.NewUTF8Builder(buildAlloc)
	sectionIndexBuilder := columnar.NewNumberBuilder[int64](buildAlloc)
	sortSchemaBuilder := columnar.NewUTF8Builder(buildAlloc)
	minTSBuilder := columnar.NewNumberBuilder[int64](buildAlloc)
	maxTSBuilder := columnar.NewNumberBuilder[int64](buildAlloc)
	rowCountBuilder := columnar.NewNumberBuilder[int64](buildAlloc)
	uncompressedSizeBuilder := columnar.NewNumberBuilder[int64](buildAlloc)

	// Determine dynamic label columns from the sort schema of the first row.
	var labelKeys []string
	if len(rows) > 0 {
		labelKeys = strings.Split(rows[0].SortSchema, ",")
		// Filter out empty strings (e.g. when SortSchema is "")
		filtered := labelKeys[:0]
		for _, k := range labelKeys {
			if k != "" {
				filtered = append(filtered, k)
			}
		}
		labelKeys = filtered
	}

	// Build a builder for each label key.
	labelBuilders := make(map[string]*columnar.UTF8Builder, len(labelKeys))
	for _, key := range labelKeys {
		labelBuilders[key] = columnar.NewUTF8Builder(buildAlloc)
	}

	for _, r := range rows {
		objectPathBuilder.AppendValue([]byte(r.ObjectPath))
		sectionIndexBuilder.AppendValue(r.SectionIndex)
		sortSchemaBuilder.AppendValue([]byte(r.SortSchema))
		for _, key := range labelKeys {
			labelBuilders[key].AppendValue([]byte(r.Labels[key]))
		}
		minTSBuilder.AppendValue(r.MinTimestamp)
		maxTSBuilder.AppendValue(r.MaxTimestamp)
		rowCountBuilder.AppendValue(r.RowCount)
		uncompressedSizeBuilder.AppendValue(r.UncompressedSize)
	}

	// Column order: fixed prefix, dynamic label columns, fixed suffix.
	// object_path, section_index, sort_schema, <label columns>, min_timestamp, max_timestamp, row_count, uncompressed_size
	columnOrder := []string{
		colObjectPath, colSectionIndex, colSortSchema,
	}
	columnOrder = append(columnOrder, labelKeys...)
	columnOrder = append(columnOrder, colMinTimestamp, colMaxTimestamp, colRowCount, colUncompressedSize)

	// Build the columns map with fixed columns plus dynamic label columns.
	columns := map[string]columnar.Array{
		colObjectPath:       objectPathBuilder.Build(),
		colSectionIndex:     sectionIndexBuilder.Build(),
		colSortSchema:       sortSchemaBuilder.Build(),
		colMinTimestamp:     minTSBuilder.Build(),
		colMaxTimestamp:     maxTSBuilder.Build(),
		colRowCount:         rowCountBuilder.Build(),
		colUncompressedSize: uncompressedSizeBuilder.Build(),
	}
	for _, key := range labelKeys {
		columns[key] = labelBuilders[key].Build()
	}

	return Section{
		ColumnNames: columnOrder,
		RowCount:    len(rows),
		OpenColumn: func(name string) (ColumnReader, error) {
			arr, ok := columns[name]
			if !ok {
				return nil, fmt.Errorf("column %q not found", name)
			}
			return &sliceColumnReader{arr: arr}, nil
		},
	}, nil
}

// sliceColumnReader wraps a single columnar.Array as a ColumnReader.
// Returns the full array on the first Read call, then io.EOF.
type sliceColumnReader struct {
	arr  columnar.Array
	read bool
}

func (r *sliceColumnReader) Read(_ context.Context, _ int) (columnar.Array, error) {
	if r.read {
		return nil, io.EOF
	}
	r.read = true
	return r.arr, nil
}

func (r *sliceColumnReader) Close() error { return nil }
