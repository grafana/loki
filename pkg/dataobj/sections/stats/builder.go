package stats

import (
	"fmt"
	"sort"
	"strings"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
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

// Builder accumulates [Stat] rows and encodes them into a section via a
// [dataobj.SectionWriter].
//
// Flush writes are done via [Builder.Flush], which encodes all accumulated
// rows and writes them to the provided [dataobj.SectionWriter].
type Builder struct {
	tenant string
	encode SectionEncoder

	rows []Stat
}

// NewBuilder creates a new Builder.
// encode is the [SectionEncoder] to use for encoding rows.
func NewBuilder(encode SectionEncoder) *Builder {
	return &Builder{
		encode: encode,
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

// compareStats returns true if a should sort before b, using the sort order:
// label values in sort schema order, then MinTimestamp, then MaxTimestamp.
func compareStats(a, b Stat) bool {
	keys := strings.Split(a.SortSchema, ",")
	for _, key := range keys {
		va, vb := a.Labels[key], b.Labels[key]
		if va != vb {
			return va < vb
		}
	}
	if a.MinTimestamp != b.MinTimestamp {
		return a.MinTimestamp < b.MinTimestamp
	}
	return a.MaxTimestamp < b.MaxTimestamp
}

// Flush sorts the accumulated rows by label values in sort schema order, then
// by MinTimestamp, then by MaxTimestamp. It encodes them via the [SectionEncoder]
// and writes the result to the provided [dataobj.SectionWriter].
//
// After a successful flush, the builder is reset.
func (b *Builder) Flush(w dataobj.SectionWriter) (n int64, err error) {
	if len(b.rows) == 0 {
		return 0, nil
	}

	// Sort rows by label values in sort schema order, then by MinTimestamp,
	// then by MaxTimestamp as a final tie-breaker.
	sort.SliceStable(b.rows, func(i, j int) bool {
		return compareStats(b.rows[i], b.rows[j])
	})

	var enc columnar.Encoder
	defer enc.Reset()

	if err := b.encode(b.rows, &enc); err != nil {
		return 0, fmt.Errorf("encoding stats: %w", err)
	}

	enc.SetTenant(b.tenant)
	n, err = enc.Flush(w)
	if err == nil {
		b.Reset()
	}
	return n, err
}
