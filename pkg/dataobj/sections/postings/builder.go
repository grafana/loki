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
//
// TODO(twhitney): Implement the [dataobj.SectionBuilder] interface by changing
// Flush to accept a [dataobj.SectionWriter] and encode via [columnar.Encoder].
// This should be done alongside the stats builder when on-disk serialization lands.
type Builder struct {
	tenant            string
	targetSectionSize int
	encode            SectionEncoder

	rows []Posting
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

// Type returns the [dataobj.SectionType] of the postings builder.
func (b *Builder) Type() dataobj.SectionType { return sectionType }

// Append adds a [Posting] row to the builder.
// TODO(twhitney): revisit overhead of accumulating all postings for the lifetime of
// a builder after implementing serialization.
func (b *Builder) Append(p Posting) {
	b.rows = append(b.rows, p)
}

// comparePostings returns true if a should sort before b, using the sort order
// [Kind, ColumnName, LabelValue, MinTimestamp, MaxTimestamp]. All comparisons
// are lexicographic. Empty LabelValue (used by Bloom entries) sorts before any
// non-empty value for the same column_name.
func comparePostings(a, b Posting) bool {
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if a.ColumnName != b.ColumnName {
		return a.ColumnName < b.ColumnName
	}
	if a.LabelValue != b.LabelValue {
		return a.LabelValue < b.LabelValue
	}
	if a.MinTimestamp != b.MinTimestamp {
		return a.MinTimestamp < b.MinTimestamp
	}
	return a.MaxTimestamp < b.MaxTimestamp
}

// EstimatedSize returns an estimate of the encoded size of the accumulated
// rows in bytes.
func (b *Builder) EstimatedSize() int {
	var total int
	for _, r := range b.rows {
		total += r.Size()
	}
	return total
}

// Reset clears all accumulated rows and resets the builder to a fresh state.
func (b *Builder) Reset() {
	b.rows = b.rows[:0]
}

// Flush sorts the accumulated rows by [Kind, ColumnName, LabelValue,
// MinTimestamp, MaxTimestamp], encodes them column-by-column into [Section]
// values, and returns one or more [Section] values.
//
// After a successful flush, the builder is reset.
func (b *Builder) Flush(ctx context.Context) ([]Section, error) {
	if len(b.rows) == 0 {
		return nil, nil
	}

	// Sort rows by [Kind, ColumnName, LabelValue, MinTimestamp, MaxTimestamp] (lexicographic).
	sort.SliceStable(b.rows, func(i, j int) bool {
		return comparePostings(b.rows[i], b.rows[j])
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
		rowSize := r.Size()
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
