package postings

import (
	"context"
	"fmt"
	"sort"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataset/array"
	"github.com/grafana/loki/v3/pkg/dataset/types"
	"github.com/grafana/loki/v3/pkg/memory"
)

// columnName constants for each column in the postings section.
const (
	colKind             = "kind"
	colObjectPath       = "object_path"
	colSectionIndex     = "section_index"
	colColumnName       = "column_name"
	colLabelValue       = "label_value"
	colBloomFilter      = "bloom_filter"
	colStreamIDBitmap   = "stream_id_bitmap"
	colUncompressedSize = "uncompressed_size"
	colMinTimestamp     = "min_timestamp"
	colMaxTimestamp     = "max_timestamp"
)

// NamedArray pairs an [array.Array] descriptor with a column name.
type NamedArray struct {
	Name  string
	Array array.Array
}

// Section holds the encoded column arrays for one flushed postings section.
type Section struct {
	Columns []NamedArray
	Store   array.Source // The backing store for reading buffer data.
}

// Builder accumulates [Posting] rows and encodes them into columnar arrays.
//
// Flush writes are done via [Builder.FlushToArrays], which encodes all
// accumulated rows into a set of [NamedArray] values that can be read back
// via a [Reader].
type Builder struct {
	tenant            string
	targetSectionSize int

	rows []Posting
}

// defaultTargetSectionSize is a sensible default for the target section size.
const defaultTargetSectionSize = 256 * 1024 * 1024 // 256 MiB

// NewBuilder creates a new Builder. targetSectionSize controls when accumulated
// data is large enough to split into multiple sections; use 0 for the default.
func NewBuilder(targetSectionSize int) *Builder {
	if targetSectionSize <= 0 {
		targetSectionSize = defaultTargetSectionSize
	}
	return &Builder{
		targetSectionSize: targetSectionSize,
	}
}

// SetTenant sets the tenant for this builder.
func (b *Builder) SetTenant(tenant string) { b.tenant = tenant }

// Tenant returns the tenant for this builder.
func (b *Builder) Tenant() string { return b.tenant }

// Type returns the [dataobj.SectionType] of the postings builder.
func (b *Builder) Type() dataobj.SectionType { return sectionType }

// Append adds a [Posting] row to the builder.
func (b *Builder) Append(p Posting) {
	b.rows = append(b.rows, p)
}

// labelValueSortKey returns the sort key for a label value. Nil (Bloom
// postings) sorts as empty string so Bloom entries come before Label entries
// for the same column_name.
func labelValueSortKey(lv *string) string {
	if lv == nil {
		return ""
	}
	return *lv
}

// EstimatedSize returns an estimate of the encoded size of the accumulated
// rows in bytes.
func (b *Builder) EstimatedSize() int {
	var total int
	for _, r := range b.rows {
		// 4 int64 columns × 8 bytes + kind (int64) = 5 * 8 = 40
		total += 5 * 8
		total += len(r.ObjectPath) + len(r.ColumnName)
		if r.LabelValue != nil {
			total += len(*r.LabelValue)
		}
		total += len(r.BloomFilter) + len(r.StreamIDBitmap)
	}
	return total
}

// Reset clears all accumulated rows and resets the builder to a fresh state.
func (b *Builder) Reset() {
	b.rows = b.rows[:0]
}

// FlushToArrays sorts the accumulated rows by [Kind, ColumnName, LabelValue,
// MinTimestamp], encodes them column-by-column into [array.Array] descriptors
// backed by the returned [array.Sink]/[array.Source], and returns one or more
// [Section] values.
//
// After a successful flush, the builder is reset.
func (b *Builder) FlushToArrays(ctx context.Context) ([]Section, error) {
	if len(b.rows) == 0 {
		return nil, nil
	}

	// Sort rows by [Kind, ColumnName, LabelValue, MinTimestamp].
	sort.SliceStable(b.rows, func(i, j int) bool {
		ri, rj := b.rows[i], b.rows[j]
		if ri.Kind != rj.Kind {
			return ri.Kind < rj.Kind
		}
		if ri.ColumnName != rj.ColumnName {
			return ri.ColumnName < rj.ColumnName
		}
		lvi := labelValueSortKey(ri.LabelValue)
		lvj := labelValueSortKey(rj.LabelValue)
		if lvi != lvj {
			return lvi < lvj
		}
		return ri.MinTimestamp < rj.MinTimestamp
	})

	// Determine section splits based on targetSectionSize.
	splits := b.computeSplits()

	sections := make([]Section, 0, len(splits))
	for _, chunk := range splits {
		sec, err := encodeRows(ctx, chunk)
		if err != nil {
			return nil, fmt.Errorf("encoding postings rows: %w", err)
		}
		sections = append(sections, sec)
	}

	b.Reset()
	return sections, nil
}

// computeSplits divides b.rows into chunks, each estimated to be at most
// targetSectionSize bytes. Returns at least one chunk.
func (b *Builder) computeSplits() [][]Posting {
	if len(b.rows) == 0 {
		return nil
	}

	var (
		chunks     [][]Posting
		chunkStart = 0
		chunkSize  = 0
	)

	for i, r := range b.rows {
		rowSize := 5*8 + len(r.ObjectPath) + len(r.ColumnName) + len(r.BloomFilter) + len(r.StreamIDBitmap)
		if r.LabelValue != nil {
			rowSize += len(*r.LabelValue)
		}
		if chunkSize+rowSize > b.targetSectionSize && chunkSize > 0 {
			chunks = append(chunks, b.rows[chunkStart:i])
			chunkStart = i
			chunkSize = 0
		}
		chunkSize += rowSize
	}

	// Append final chunk.
	chunks = append(chunks, b.rows[chunkStart:])
	return chunks
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

// encodeRows encodes a slice of rows into a [Section].
func encodeRows(ctx context.Context, rows []Posting) (Section, error) {
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
	namedArrays := make([]NamedArray, 0, 10)

	flush := func(w array.Writer, name string) error {
		arr, err := w.Flush(ctx, store)
		if err != nil {
			return fmt.Errorf("flushing %s: %w", name, err)
		}
		namedArrays = append(namedArrays, NamedArray{Name: name, Array: arr})
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

	return Section{Columns: namedArrays, Store: store}, nil
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
