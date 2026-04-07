package stats

import (
	"context"
	"fmt"
	"io"

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
	runIDBuilder := columnar.NewNumberBuilder[int64](buildAlloc)
	sortSchemaBuilder := columnar.NewUTF8Builder(buildAlloc)
	serviceNameBuilder := columnar.NewUTF8Builder(buildAlloc)
	minTsBuilder := columnar.NewNumberBuilder[int64](buildAlloc)
	maxTsBuilder := columnar.NewNumberBuilder[int64](buildAlloc)
	rowCountBuilder := columnar.NewNumberBuilder[int64](buildAlloc)
	uncompressedSizeBuilder := columnar.NewNumberBuilder[int64](buildAlloc)

	for _, r := range rows {
		objectPathBuilder.AppendValue([]byte(r.ObjectPath))
		sectionIndexBuilder.AppendValue(r.SectionIndex)
		runIDBuilder.AppendValue(r.RunID)
		sortSchemaBuilder.AppendValue([]byte(r.SortSchema))
		serviceNameBuilder.AppendValue([]byte(r.ServiceName))
		minTsBuilder.AppendValue(r.MinTimestamp)
		maxTsBuilder.AppendValue(r.MaxTimestamp)
		rowCountBuilder.AppendValue(r.RowCount)
		uncompressedSizeBuilder.AppendValue(r.UncompressedSize)
	}

	// Column order must match the order used by DatasetEncoder.
	columnOrder := []string{
		colObjectPath, colSectionIndex, colRunID, colSortSchema, colServiceName,
		colMinTimestamp, colMaxTimestamp, colRowCount, colUncompressedSize,
	}

	columns := map[string]columnar.Array{
		colObjectPath:       objectPathBuilder.Build(),
		colSectionIndex:     sectionIndexBuilder.Build(),
		colRunID:            runIDBuilder.Build(),
		colSortSchema:       sortSchemaBuilder.Build(),
		colServiceName:      serviceNameBuilder.Build(),
		colMinTimestamp:     minTsBuilder.Build(),
		colMaxTimestamp:     maxTsBuilder.Build(),
		colRowCount:         rowCountBuilder.Build(),
		colUncompressedSize: uncompressedSizeBuilder.Build(),
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
