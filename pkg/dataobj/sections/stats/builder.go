package stats

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

// columnName constants for each column in the stats section.
const (
	colObjectPath       = "object_path"
	colSectionIndex     = "section_index"
	colRunID            = "run_id"
	colSortSchema       = "sort_schema"
	colServiceName      = "service_name"
	colMinTimestamp     = "min_timestamp"
	colMaxTimestamp     = "max_timestamp"
	colRowCount         = "row_count"
	colUncompressedSize = "uncompressed_size"
)

// NamedArray pairs an [array.Array] descriptor with a column name.
type NamedArray struct {
	Name  string
	Array array.Array
}

// Section holds the encoded column arrays for one flushed stats section.
type Section struct {
	Columns []NamedArray
	Store   array.Source // The backing store for reading buffer data.
}

// Builder accumulates [Stat] rows and encodes them into columnar arrays.
//
// Flush writes are done via [Builder.FlushToArrays], which encodes all
// accumulated rows into a set of [NamedArray] values that can be read back
// via a [Reader].
//
// The full [dataobj.SectionBuilder] interface (including Flush to a
// [dataobj.SectionWriter]) is not yet implemented; that will be added when
// on-disk serialization lands.
type Builder struct {
	tenant            string
	targetSectionSize int

	rows []Stat
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

// Type returns the [dataobj.SectionType] of the stats builder.
func (b *Builder) Type() dataobj.SectionType { return sectionType }

// Append adds a [Stat] row to the builder.
func (b *Builder) Append(stat Stat) {
	b.rows = append(b.rows, stat)
}

// EstimatedSize returns an estimate of the encoded size of the accumulated
// rows in bytes. The estimate uses a per-row heuristic:
//   - 6 int64 columns × 8 bytes = 48 bytes (SectionIndex, RunID, MinTimestamp, MaxTimestamp, RowCount, UncompressedSize)
//   - len(ObjectPath) + len(SortSchema) + len(ServiceName) bytes for string columns
func (b *Builder) EstimatedSize() int {
	var total int
	for _, r := range b.rows {
		total += 6 * 8 // int64 columns: SectionIndex, RunID, MinTimestamp, MaxTimestamp, RowCount, UncompressedSize
		total += len(r.ObjectPath) + len(r.SortSchema) + len(r.ServiceName)
	}
	return total
}

// Reset clears all accumulated rows and resets the builder to a fresh state.
func (b *Builder) Reset() {
	b.rows = b.rows[:0]
}

// FlushToArrays sorts the accumulated rows by [ServiceName, MinTimestamp],
// encodes them column-by-column into [array.Array] descriptors backed by the
// returned [array.Sink]/[array.Source], and returns one or more [Section]
// values. When the accumulated row size exceeds the configured
// targetSectionSize, the rows are split across multiple sections.
//
// After a successful flush, the builder is reset.
func (b *Builder) FlushToArrays(ctx context.Context) ([]Section, error) {
	if len(b.rows) == 0 {
		return nil, nil
	}

	// Sort rows by [ServiceName, MinTimestamp].
	sort.SliceStable(b.rows, func(i, j int) bool {
		ri, rj := b.rows[i], b.rows[j]
		if ri.ServiceName != rj.ServiceName {
			return ri.ServiceName < rj.ServiceName
		}
		return ri.MinTimestamp < rj.MinTimestamp
	})

	// Determine section splits based on targetSectionSize.
	splits := b.computeSplits()

	sections := make([]Section, 0, len(splits))
	for _, chunk := range splits {
		sec, err := encodeRows(ctx, chunk)
		if err != nil {
			return nil, fmt.Errorf("encoding stats rows: %w", err)
		}
		sections = append(sections, sec)
	}

	b.Reset()
	return sections, nil
}

// computeSplits divides b.rows into chunks, each estimated to be at most
// targetSectionSize bytes. Returns at least one chunk.
func (b *Builder) computeSplits() [][]Stat {
	if len(b.rows) == 0 {
		return nil
	}

	var (
		chunks     [][]Stat
		chunkStart = 0
		chunkSize  = 0
	)

	for i, r := range b.rows {
		rowSize := 6*8 + len(r.ObjectPath) + len(r.SortSchema) + len(r.ServiceName)
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

// encodeRows encodes a slice of rows into a [Section].
func encodeRows(ctx context.Context, rows []Stat) (Section, error) {
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
	namedArrays := make([]NamedArray, 0, 9)

	flush := func(w array.Writer, name string) error {
		arr, err := w.Flush(ctx, store)
		if err != nil {
			return fmt.Errorf("flushing %s: %w", name, err)
		}
		namedArrays = append(namedArrays, NamedArray{Name: name, Array: arr})
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
