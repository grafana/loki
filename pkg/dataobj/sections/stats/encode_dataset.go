package stats

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataset/array"
	"github.com/grafana/loki/v3/pkg/dataset/types"
	"github.com/grafana/loki/v3/pkg/memory"
)

// namedArray pairs an array.Array descriptor with a column name (internal to this file).
type namedArray struct {
	Name  string
	Array array.Array
}

// DatasetEncoder is a [SectionEncoder] that encodes rows using the dataset API.
func DatasetEncoder(ctx context.Context, rows []Stat) (Section, error) {
	var alloc memory.Allocator
	store := &inMemoryStore{}

	// Create writers for each column.
	int64Spec := &array.SpecPlain{}
	int64Type := &types.Int64{Nullable: false}

	utf8Spec := &array.SpecBinary{Offsets: &array.SpecPlain{}}
	utf8Type := &types.UTF8{Nullable: false}

	objectPathWriter, err := array.NewWriter(&alloc, utf8Spec, utf8Type)
	if err != nil {
		return Section{}, fmt.Errorf("creating object_path writer: %w", err)
	}
	sectionIndexWriter, err := array.NewWriter(&alloc, int64Spec, int64Type)
	if err != nil {
		return Section{}, fmt.Errorf("creating section_index writer: %w", err)
	}
	runIDWriter, err := array.NewWriter(&alloc, int64Spec, int64Type)
	if err != nil {
		return Section{}, fmt.Errorf("creating run_id writer: %w", err)
	}
	sortSchemaWriter, err := array.NewWriter(&alloc, utf8Spec, utf8Type)
	if err != nil {
		return Section{}, fmt.Errorf("creating sort_schema writer: %w", err)
	}
	serviceNameWriter, err := array.NewWriter(&alloc, utf8Spec, utf8Type)
	if err != nil {
		return Section{}, fmt.Errorf("creating service_name writer: %w", err)
	}
	minTsWriter, err := array.NewWriter(&alloc, int64Spec, int64Type)
	if err != nil {
		return Section{}, fmt.Errorf("creating min_timestamp writer: %w", err)
	}
	maxTsWriter, err := array.NewWriter(&alloc, int64Spec, int64Type)
	if err != nil {
		return Section{}, fmt.Errorf("creating max_timestamp writer: %w", err)
	}
	rowCountWriter, err := array.NewWriter(&alloc, int64Spec, int64Type)
	if err != nil {
		return Section{}, fmt.Errorf("creating row_count writer: %w", err)
	}
	uncompressedSizeWriter, err := array.NewWriter(&alloc, int64Spec, int64Type)
	if err != nil {
		return Section{}, fmt.Errorf("creating uncompressed_size writer: %w", err)
	}

	// Build columnar arrays for all rows.
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

	// Append built arrays to writers.
	if err := objectPathWriter.Append(objectPathBuilder.Build()); err != nil {
		return Section{}, fmt.Errorf("appending object_path: %w", err)
	}
	if err := sectionIndexWriter.Append(sectionIndexBuilder.Build()); err != nil {
		return Section{}, fmt.Errorf("appending section_index: %w", err)
	}
	if err := runIDWriter.Append(runIDBuilder.Build()); err != nil {
		return Section{}, fmt.Errorf("appending run_id: %w", err)
	}
	if err := sortSchemaWriter.Append(sortSchemaBuilder.Build()); err != nil {
		return Section{}, fmt.Errorf("appending sort_schema: %w", err)
	}
	if err := serviceNameWriter.Append(serviceNameBuilder.Build()); err != nil {
		return Section{}, fmt.Errorf("appending service_name: %w", err)
	}
	if err := minTsWriter.Append(minTsBuilder.Build()); err != nil {
		return Section{}, fmt.Errorf("appending min_timestamp: %w", err)
	}
	if err := maxTsWriter.Append(maxTsBuilder.Build()); err != nil {
		return Section{}, fmt.Errorf("appending max_timestamp: %w", err)
	}
	if err := rowCountWriter.Append(rowCountBuilder.Build()); err != nil {
		return Section{}, fmt.Errorf("appending row_count: %w", err)
	}
	if err := uncompressedSizeWriter.Append(uncompressedSizeBuilder.Build()); err != nil {
		return Section{}, fmt.Errorf("appending uncompressed_size: %w", err)
	}

	// Flush writers to the in-memory store.
	namedArrays := make([]namedArray, 0, 9)

	flush := func(w array.Writer, name string) error {
		arr, err := w.Flush(ctx, store)
		if err != nil {
			return fmt.Errorf("flushing %s: %w", name, err)
		}
		namedArrays = append(namedArrays, namedArray{Name: name, Array: arr})
		return nil
	}

	if err := flush(objectPathWriter, colObjectPath); err != nil {
		return Section{}, err
	}
	if err := flush(sectionIndexWriter, colSectionIndex); err != nil {
		return Section{}, err
	}
	if err := flush(runIDWriter, colRunID); err != nil {
		return Section{}, err
	}
	if err := flush(sortSchemaWriter, colSortSchema); err != nil {
		return Section{}, err
	}
	if err := flush(serviceNameWriter, colServiceName); err != nil {
		return Section{}, err
	}
	if err := flush(minTsWriter, colMinTimestamp); err != nil {
		return Section{}, err
	}
	if err := flush(maxTsWriter, colMaxTimestamp); err != nil {
		return Section{}, err
	}
	if err := flush(rowCountWriter, colRowCount); err != nil {
		return Section{}, err
	}
	if err := flush(uncompressedSizeWriter, colUncompressedSize); err != nil {
		return Section{}, err
	}

	columnNames := make([]string, len(namedArrays))
	for i, na := range namedArrays {
		columnNames[i] = na.Name
	}

	return Section{
		ColumnNames: columnNames,
		RowCount:    len(rows),
		OpenColumn: func(name string) (ColumnReader, error) {
			for _, na := range namedArrays {
				if na.Name == name {
					var readerAlloc memory.Allocator
					r, err := array.NewReader(&readerAlloc, na.Array, store)
					if err != nil {
						return nil, fmt.Errorf("creating reader for column %q: %w", name, err)
					}
					return &datasetColumnReader{reader: r, alloc: &readerAlloc}, nil
				}
			}
			return nil, fmt.Errorf("column %q not found", name)
		},
	}, nil
}

// datasetColumnReader wraps an array.Reader as a ColumnReader.
type datasetColumnReader struct {
	reader array.Reader
	alloc  *memory.Allocator
}

func (r *datasetColumnReader) Read(ctx context.Context, count int) (columnar.Array, error) {
	return r.reader.Read(ctx, r.alloc, count)
}

func (r *datasetColumnReader) Close() error {
	return r.reader.Close()
}

// inMemoryStore implements both [array.Sink] and [array.Source] by storing
// buffers in memory.
type inMemoryStore struct {
	bufs []array.BufferData
}

var (
	_ array.Source = (*inMemoryStore)(nil)
	_ array.Sink   = (*inMemoryStore)(nil)
)

func (s *inMemoryStore) ReadBuffers(_ context.Context, _ *memory.Allocator, bufs []array.Buffer) ([]array.BufferData, error) {
	res := make([]array.BufferData, len(bufs))
	for i, buf := range bufs {
		if buf.ID < 0 || buf.ID >= int64(len(s.bufs)) {
			return nil, fmt.Errorf("invalid buffer ID: %d", buf.ID)
		}
		res[i] = s.bufs[buf.ID]
	}
	return res, nil
}

func (s *inMemoryStore) WriteBuffers(_ context.Context, data []array.BufferData) ([]array.Buffer, error) {
	results := make([]array.Buffer, len(data))
	for i, d := range data {
		// Clone buffers since we must not retain data beyond the call.
		clone := make(array.BufferData, len(d))
		copy(clone, d)
		s.bufs = append(s.bufs, clone)
		results[i].ID = int64(len(s.bufs) - 1)
	}
	return results, nil
}
