package postings

import (
	"context"
	"fmt"
	"sort"

	"github.com/grafana/loki/v3/pkg/dataobj"
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

// Builder accumulates [Posting] rows and encodes them into columnar arrays.
//
// Flush writes are done via [Builder.Flush], which encodes all accumulated
// rows into a set of [Section] values that can be read back via a [Reader].
type Builder struct {
	tenant            string
	targetSectionSize int
	encode            SectionEncoder

	rows []Posting
}

// defaultTargetSectionSize is a sensible default for the target section size.
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

// Flush sorts the accumulated rows by [Kind, ColumnName, LabelValue,
// MinTimestamp], encodes them column-by-column into [Section] values,
// and returns one or more [Section] values.
//
// After a successful flush, the builder is reset.
func (b *Builder) Flush(ctx context.Context) ([]Section, error) {
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
		sec, err := b.encode(ctx, chunk)
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
