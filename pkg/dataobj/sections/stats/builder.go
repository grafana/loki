package stats

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

// columnName constants for each column in the stats section.
const (
	colObjectPath       = "object_path"
	colSectionIndex     = "section_index"
	colSortSchema       = "sort_schema"
	colMinTimestamp     = "min_timestamp"
	colMaxTimestamp     = "max_timestamp"
	colRowCount         = "row_count"
	colUncompressedSize = "uncompressed_size"
)

// Builder accumulates [Stat] rows and encodes them into columnar arrays.
//
// Flush writes are done via [Builder.Flush], which encodes all accumulated
// rows into a set of [Section] values that can be read back via a [Reader].
//
// TODO(twhitney): Implement the [dataobj.SectionBuilder] interface by changing
// Flush to accept a [dataobj.SectionWriter] and encode via [columnar.Encoder].
// This should be done alongside the postings builder when on-disk serialization lands.
type Builder struct {
	tenant            string
	targetSectionSize int
	encode            SectionEncoder

	rows []Stat
}

// defaultTargetSectionSize is used when no target section size is provided
// (e.g., in tests). Production callers always pass TargetSectionSize from config.
const defaultTargetSectionSize = 256 * 1024 * 1024 // 256 MiB

// NewBuilder creates a new Builder. targetSectionSize controls when accumulated
// data is large enough to split into multiple sections; use 0 for the default.
// encode is the SectionEncoder to use for encoding rows.
func NewBuilder(targetSectionSize int, encode SectionEncoder) *Builder {
	if targetSectionSize <= 0 {
		targetSectionSize = defaultTargetSectionSize
	}
	return &Builder{
		targetSectionSize: targetSectionSize,
		encode:            encode,
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
//   - 5 int64 columns × 8 bytes = 40 bytes (SectionIndex, MinTimestamp, MaxTimestamp, RowCount, UncompressedSize)
//   - len(ObjectPath) + len(SortSchema) bytes for fixed string columns
//   - sum of len(k)+len(v) for all entries in Labels
func (b *Builder) EstimatedSize() int {
	var total int
	for _, r := range b.rows {
		total += 5 * 8 // int64 columns: SectionIndex, MinTimestamp, MaxTimestamp, RowCount, UncompressedSize
		total += len(r.ObjectPath) + len(r.SortSchema)
		for k, v := range r.Labels {
			total += len(k) + len(v)
		}
	}
	return total
}

// Reset clears all accumulated rows and resets the builder to a fresh state.
func (b *Builder) Reset() {
	b.rows = b.rows[:0]
}

// Flush sorts the accumulated rows by label values in sort schema order, then
// by MinTimestamp. It encodes them column-by-column into [Section] values,
// and returns one or more [Section] values. When the accumulated row size
// exceeds the configured targetSectionSize, the rows are split across multiple
// sections.
//
// After a successful flush, the builder is reset.
func (b *Builder) Flush(ctx context.Context) ([]Section, error) {
	if len(b.rows) == 0 {
		return nil, nil
	}

	// Sort rows by label values in sort schema order, then by MinTimestamp,
	// then by MaxTimestamp as a final tie-breaker.
	// Get sort key order from the first row's SortSchema.
	keys := strings.Split(b.rows[0].SortSchema, ",")
	sort.SliceStable(b.rows, func(i, j int) bool {
		ri, rj := b.rows[i], b.rows[j]
		for _, key := range keys {
			vi, vj := ri.Labels[key], rj.Labels[key]
			if vi != vj {
				return vi < vj
			}
		}
		if ri.MinTimestamp != rj.MinTimestamp {
			return ri.MinTimestamp < rj.MinTimestamp
		}
		return ri.MaxTimestamp < rj.MaxTimestamp
	})

	// Determine section splits based on targetSectionSize.
	splits := b.computeSplits()

	sections := make([]Section, 0, len(splits))
	for _, chunk := range splits {
		sec, err := b.encode(ctx, chunk)
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
		rowSize := 6*8 + len(r.ObjectPath) + len(r.SortSchema)
		for k, v := range r.Labels {
			rowSize += len(k) + len(v)
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
