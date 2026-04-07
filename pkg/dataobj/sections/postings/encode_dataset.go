package postings

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

// normalizeBitmaps pads all bitmaps to the same length (the maximum length)
// with zero bytes.
func normalizeBitmaps(rows []Posting) [][]byte {
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

// DatasetEncoder is a [SectionEncoder] that encodes rows using the dataset API.
func DatasetEncoder(ctx context.Context, rows []Posting) (Section, error) {
	var alloc memory.Allocator
	store := &inMemoryStore{}

	// Non-nullable specs.
	int64Spec := &array.SpecPlain{}
	int64Type := &types.Int64{Nullable: false}

	utf8Spec := &array.SpecBinary{Offsets: &array.SpecPlain{}}
	utf8Type := &types.UTF8{Nullable: false}

	binarySpec := &array.SpecBinary{Offsets: &array.SpecPlain{}}
	binaryType := &types.Binary{Nullable: false}

	// Nullable specs.
	nullableUTF8Spec := &array.SpecBinary{Offsets: &array.SpecPlain{}, Validity: &array.SpecBool{}}
	nullableUTF8Type := &types.UTF8{Nullable: true}

	nullableBinarySpec := &array.SpecBinary{Offsets: &array.SpecPlain{}, Validity: &array.SpecBool{}}
	nullableBinaryType := &types.Binary{Nullable: true}

	// Create writers for each column.
	kindWriter, err := array.NewWriter(&alloc, int64Spec, int64Type)
	if err != nil {
		return Section{}, fmt.Errorf("creating kind writer: %w", err)
	}
	objectPathWriter, err := array.NewWriter(&alloc, utf8Spec, utf8Type)
	if err != nil {
		return Section{}, fmt.Errorf("creating object_path writer: %w", err)
	}
	sectionIndexWriter, err := array.NewWriter(&alloc, int64Spec, int64Type)
	if err != nil {
		return Section{}, fmt.Errorf("creating section_index writer: %w", err)
	}
	columnNameWriter, err := array.NewWriter(&alloc, utf8Spec, utf8Type)
	if err != nil {
		return Section{}, fmt.Errorf("creating column_name writer: %w", err)
	}
	labelValueWriter, err := array.NewWriter(&alloc, nullableUTF8Spec, nullableUTF8Type)
	if err != nil {
		return Section{}, fmt.Errorf("creating label_value writer: %w", err)
	}
	bloomFilterWriter, err := array.NewWriter(&alloc, nullableBinarySpec, nullableBinaryType)
	if err != nil {
		return Section{}, fmt.Errorf("creating bloom_filter writer: %w", err)
	}
	streamIDBitmapWriter, err := array.NewWriter(&alloc, binarySpec, binaryType)
	if err != nil {
		return Section{}, fmt.Errorf("creating stream_id_bitmap writer: %w", err)
	}
	uncompressedSizeWriter, err := array.NewWriter(&alloc, int64Spec, int64Type)
	if err != nil {
		return Section{}, fmt.Errorf("creating uncompressed_size writer: %w", err)
	}
	minTsWriter, err := array.NewWriter(&alloc, int64Spec, int64Type)
	if err != nil {
		return Section{}, fmt.Errorf("creating min_timestamp writer: %w", err)
	}
	maxTsWriter, err := array.NewWriter(&alloc, int64Spec, int64Type)
	if err != nil {
		return Section{}, fmt.Errorf("creating max_timestamp writer: %w", err)
	}

	// Build columnar arrays for all rows.
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
	minTsBuilder := columnar.NewNumberBuilder[int64](buildAlloc)
	maxTsBuilder := columnar.NewNumberBuilder[int64](buildAlloc)

	// Normalize stream ID bitmaps to the same length.
	normalizedBitmaps := normalizeBitmaps(rows)

	for i, r := range rows {
		kindBuilder.AppendValue(int64(r.Kind))
		objectPathBuilder.AppendValue([]byte(r.ObjectPath))
		sectionIndexBuilder.AppendValue(r.SectionIndex)
		columnNameBuilder.AppendValue([]byte(r.ColumnName))

		if r.LabelValue == nil {
			labelValueBuilder.AppendNull()
		} else {
			labelValueBuilder.AppendValue([]byte(*r.LabelValue))
		}

		if r.BloomFilter == nil {
			bloomFilterBuilder.AppendNull()
		} else {
			bloomFilterBuilder.AppendValue(r.BloomFilter)
		}

		streamIDBitmapBuilder.AppendValue(normalizedBitmaps[i])
		uncompressedSizeBuilder.AppendValue(r.UncompressedSize)
		minTsBuilder.AppendValue(r.MinTimestamp)
		maxTsBuilder.AppendValue(r.MaxTimestamp)
	}

	// Append built arrays to writers.
	if err := kindWriter.Append(kindBuilder.Build()); err != nil {
		return Section{}, fmt.Errorf("appending kind: %w", err)
	}
	if err := objectPathWriter.Append(objectPathBuilder.Build()); err != nil {
		return Section{}, fmt.Errorf("appending object_path: %w", err)
	}
	if err := sectionIndexWriter.Append(sectionIndexBuilder.Build()); err != nil {
		return Section{}, fmt.Errorf("appending section_index: %w", err)
	}
	if err := columnNameWriter.Append(columnNameBuilder.Build()); err != nil {
		return Section{}, fmt.Errorf("appending column_name: %w", err)
	}
	if err := labelValueWriter.Append(labelValueBuilder.Build()); err != nil {
		return Section{}, fmt.Errorf("appending label_value: %w", err)
	}
	if err := bloomFilterWriter.Append(bloomFilterBuilder.Build()); err != nil {
		return Section{}, fmt.Errorf("appending bloom_filter: %w", err)
	}
	if err := streamIDBitmapWriter.Append(streamIDBitmapBuilder.Build()); err != nil {
		return Section{}, fmt.Errorf("appending stream_id_bitmap: %w", err)
	}
	if err := uncompressedSizeWriter.Append(uncompressedSizeBuilder.Build()); err != nil {
		return Section{}, fmt.Errorf("appending uncompressed_size: %w", err)
	}
	if err := minTsWriter.Append(minTsBuilder.Build()); err != nil {
		return Section{}, fmt.Errorf("appending min_timestamp: %w", err)
	}
	if err := maxTsWriter.Append(maxTsBuilder.Build()); err != nil {
		return Section{}, fmt.Errorf("appending max_timestamp: %w", err)
	}

	// Flush writers to the in-memory store.
	namedArrays := make([]namedArray, 0, 10)

	flush := func(w array.Writer, name string) error {
		arr, err := w.Flush(ctx, store)
		if err != nil {
			return fmt.Errorf("flushing %s: %w", name, err)
		}
		namedArrays = append(namedArrays, namedArray{Name: name, Array: arr})
		return nil
	}

	if err := flush(kindWriter, colKind); err != nil {
		return Section{}, err
	}
	if err := flush(objectPathWriter, colObjectPath); err != nil {
		return Section{}, err
	}
	if err := flush(sectionIndexWriter, colSectionIndex); err != nil {
		return Section{}, err
	}
	if err := flush(columnNameWriter, colColumnName); err != nil {
		return Section{}, err
	}
	if err := flush(labelValueWriter, colLabelValue); err != nil {
		return Section{}, err
	}
	if err := flush(bloomFilterWriter, colBloomFilter); err != nil {
		return Section{}, err
	}
	if err := flush(streamIDBitmapWriter, colStreamIDBitmap); err != nil {
		return Section{}, err
	}
	if err := flush(uncompressedSizeWriter, colUncompressedSize); err != nil {
		return Section{}, err
	}
	if err := flush(minTsWriter, colMinTimestamp); err != nil {
		return Section{}, err
	}
	if err := flush(maxTsWriter, colMaxTimestamp); err != nil {
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
