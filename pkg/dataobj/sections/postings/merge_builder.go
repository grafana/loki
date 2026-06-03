package postings

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// MergeBuilder accumulates posting entries from pre-aggregated rows (e.g., from
// existing postings sections) and merges them. Unlike Builder, which aggregates
// per-observation, MergeBuilder accepts already-aggregated entries.
// Call [MergeBuilder.Flush] to encode all accumulated data and write a postings
// section to the provided [dataobj.SectionWriter].
type MergeBuilder struct {
	metrics *Metrics
	tenant  string
	labels  map[labelPostingKey]LabelEntry
	blooms  map[bloomPostingKey]BloomEntry

	pageSizeHint    int
	pageMaxRowCount int
	estimatedSize   int
}

// NewMergeBuilder creates a new [MergeBuilder] for accumulating pre-aggregated
// posting entries.
//
// pageSizeHint and pageMaxRowCount control page splitting of the underlying
// column builders (0 means use defaults).
// metrics may be nil to disable instrumentation.
func NewMergeBuilder(metrics *Metrics, pageSizeHint, pageMaxRowCount int) *MergeBuilder {
	return &MergeBuilder{
		metrics:         metrics,
		labels:          make(map[labelPostingKey]LabelEntry),
		blooms:          make(map[bloomPostingKey]BloomEntry),
		pageSizeHint:    pageSizeHint,
		pageMaxRowCount: pageMaxRowCount,
	}
}

// SetTenant sets the tenant for this builder.
func (b *MergeBuilder) SetTenant(tenant string) { b.tenant = tenant }

// Tenant returns the tenant for this builder.
func (b *MergeBuilder) Tenant() string { return b.tenant }

// Type returns the [dataobj.SectionType] of the postings builder.
func (b *MergeBuilder) Type() dataobj.SectionType { return sectionType }

// AppendLabelEntry appends a label posting entry directly to the builder.
// Returns an error if an entry with the same (ObjectPath, SectionIndex,
// ColumnName, LabelValue) key already exists.
func (b *MergeBuilder) AppendLabelEntry(entry LabelEntry) error {
	key := labelPostingKey{
		objectPath:   entry.ObjectPath,
		sectionIndex: entry.SectionIndex,
		columnName:   entry.ColumnName,
		labelValue:   entry.LabelValue,
	}

	if _, ok := b.labels[key]; ok {
		return fmt.Errorf("label posting entry with key (%q, %d, %q, %q) already exists", entry.ObjectPath, entry.SectionIndex, entry.ColumnName, entry.LabelValue)
	}

	b.labels[key] = entry

	// Track size: 5 int64 fields + string sizes + bitmap bytes.
	b.estimatedSize += 5*8 + len(entry.ObjectPath) + len(entry.ColumnName) + len(entry.LabelValue) + len(entry.StreamIDBitmap)

	return nil
}

// AppendBloomEntry appends a bloom posting entry directly to the builder.
// Returns an error if an entry with the same (ObjectPath, SectionIndex,
// ColumnName) key already exists.
func (b *MergeBuilder) AppendBloomEntry(entry BloomEntry) error {
	key := bloomPostingKey{
		objectPath:   entry.ObjectPath,
		sectionIndex: entry.SectionIndex,
		columnName:   entry.ColumnName,
	}

	if _, ok := b.blooms[key]; ok {
		return fmt.Errorf("bloom posting entry with key (%q, %d, %q) already exists", entry.ObjectPath, entry.SectionIndex, entry.ColumnName)
	}

	b.blooms[key] = entry

	// Track size: 5 int64 fields + string sizes + bloom bytes + bitmap bytes.
	b.estimatedSize += 5*8 + len(entry.ObjectPath) + len(entry.ColumnName) + len(entry.BloomFilter) + len(entry.StreamIDBitmap)

	return nil
}

// EstimatedSize returns an estimate of the encoded size of the accumulated
// data in bytes.
func (b *MergeBuilder) EstimatedSize() int {
	return b.estimatedSize
}

// Reset clears all accumulated data and resets the builder to a fresh state.
func (b *MergeBuilder) Reset() {
	b.labels = make(map[labelPostingKey]LabelEntry)
	b.blooms = make(map[bloomPostingKey]BloomEntry)
	b.estimatedSize = 0
}

// Flush encodes all accumulated entries into the provided
// [dataobj.SectionWriter] and returns the number of bytes written.
//
// After a successful flush, the builder is reset.
func (b *MergeBuilder) Flush(w dataobj.SectionWriter) (n int64, err error) {
	// Convert maps to slices.
	labelEntries := make([]LabelEntry, 0, len(b.labels))
	for _, entry := range b.labels {
		labelEntries = append(labelEntries, entry)
	}

	bloomEntries := make([]BloomEntry, 0, len(b.blooms))
	for _, entry := range b.blooms {
		bloomEntries = append(bloomEntries, entry)
	}

	if len(labelEntries) == 0 && len(bloomEntries) == 0 {
		return 0, nil
	}

	if b.metrics != nil {
		timer := prometheus.NewTimer(b.metrics.encodeSeconds)
		defer timer.ObserveDuration()
	}

	sortLabelEntries(labelEntries)
	sortBloomEntries(bloomEntries)

	var enc columnar.Encoder
	defer enc.Reset()

	if err := columnarEncode(bloomEntries, labelEntries, &enc, b.pageSizeHint, b.pageMaxRowCount); err != nil {
		return 0, fmt.Errorf("encoding postings: %w", err)
	}

	enc.SetTenant(b.tenant)
	n, err = enc.Flush(w)
	if err == nil {
		b.Reset()
	}
	return n, err
}
