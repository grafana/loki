package postings

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

// MergeBuilder writes already-aggregated posting entries, such as the rows of
// existing postings sections, to postings sections. Unlike Builder, which
// aggregates observations, MergeBuilder does not aggregate or sort.
// Call [MergeBuilder.Flush] to encode all accumulated data and write a postings
// section to the provided [dataobj.SectionWriter].
//
// The caller must append the entries of each kind in strictly increasing
// [CompareRows] order. MergeBuilder writes them in that order.
type MergeBuilder struct {
	metrics       *Metrics
	tenant        string
	labels        []LabelEntry
	blooms        []BloomEntry
	estimatedSize int

	encoder *postingsEncoder
}

// NewMergeBuilder creates a new [MergeBuilder] for accumulating pre-aggregated
// posting entries.
//
// pageSizeHint and pageMaxRowCount control page splitting of the underlying
// column builders (0 means use defaults). targetSectionSizeBytes is the uncompressed
// size threshold in bytes at which Flush splits accumulated entries into multiple
// sections; it must be > 0. metrics may be nil to disable instrumentation.
func NewMergeBuilder(metrics *Metrics, pageSizeHint, pageMaxRowCount, targetSectionSizeBytes int) *MergeBuilder {
	return &MergeBuilder{
		metrics: metrics,
		encoder: newPostingsEncoder(pageSizeHint, pageMaxRowCount, targetSectionSizeBytes),
	}
}

// SetTenant sets the tenant for this builder.
func (b *MergeBuilder) SetTenant(tenant string) { b.tenant = tenant }

// Tenant returns the tenant for this builder.
func (b *MergeBuilder) Tenant() string { return b.tenant }

// Type returns the [dataobj.SectionType] of the postings builder.
func (b *MergeBuilder) Type() dataobj.SectionType { return sectionType }

// AppendLabelEntry appends a label posting entry directly to the builder.
//
// It returns an error if entry does not sort after the previous label entry in
// [CompareRows] order.
//
// It does not find two entries for one logs section and posting key when
// their time ranges differ. [CompareRows] does not put such entries next to
// each other, so the caller must remove them.
func (b *MergeBuilder) AppendLabelEntry(entry LabelEntry) error {
	if n := len(b.labels); n > 0 {
		if err := checkAppendOrder(b.labels[n-1].Row(), entry.Row()); err != nil {
			return err
		}
	}

	b.labels = append(b.labels, entry)
	b.estimatedSize += labelEntrySize(entry)

	return nil
}

// AppendBloomEntry appends a bloom posting entry directly to the builder.
//
// It returns an error if entry does not sort after the previous bloom entry in
// [CompareRows] order.
//
// It does not find two entries for one logs section and posting key when
// their time ranges differ. [CompareRows] does not put such entries next to
// each other, so the caller must remove them.
func (b *MergeBuilder) AppendBloomEntry(entry BloomEntry) error {
	if n := len(b.blooms); n > 0 {
		if err := checkAppendOrder(b.blooms[n-1].Row(), entry.Row()); err != nil {
			return err
		}
	}

	b.blooms = append(b.blooms, entry)
	b.estimatedSize += bloomEntrySize(entry)

	return nil
}

// checkAppendOrder returns an error if next does not sort after prev in
// [CompareRows] order.
func checkAppendOrder(prev, next Row) error {
	switch c := CompareRows(prev, next); {
	case c == 0:
		return fmt.Errorf("duplicate posting entry %s", describeRowOrder(next))
	case c > 0:
		return fmt.Errorf("posting entry %s sorts before the previous entry %s", describeRowOrder(next), describeRowOrder(prev))
	default:
		return nil
	}
}

// describeRowOrder formats the fields of row that [CompareRows] uses.
func describeRowOrder(row Row) string {
	return fmt.Sprintf("(kind %d, column %q, value %q, time [%d, %d], object %q, section %d)",
		row.Kind, row.ColumnName, row.LabelValue, row.MinTimestamp, row.MaxTimestamp, row.ObjectPath, row.SectionIndex)
}

// EstimatedSize returns an estimate of the encoded size of the accumulated
// data in bytes.
func (b *MergeBuilder) EstimatedSize() int {
	return b.estimatedSize
}

// Reset clears all accumulated data and resets the builder to a fresh state.
// The builder keeps the capacity of its buffers for the next section.
func (b *MergeBuilder) Reset() {
	// Clear before truncating, so the reused buffers do not keep the bitmaps
	// and bloom filters of the flushed entries reachable.
	clear(b.labels)
	clear(b.blooms)
	b.labels = b.labels[:0]
	b.blooms = b.blooms[:0]
	b.estimatedSize = 0
}

// Flush encodes all accumulated entries into the provided
// [dataobj.SectionWriter] and returns the number of bytes written.
//
// After a successful flush, the builder is reset.
func (b *MergeBuilder) Flush(w dataobj.SectionWriter) (int64, error) {
	if len(b.labels) == 0 && len(b.blooms) == 0 {
		return 0, nil
	}

	if b.metrics != nil {
		timer := prometheus.NewTimer(b.metrics.encodeSeconds)
		defer timer.ObserveDuration()
	}

	nBloom, err := b.encoder.encodeBloomEntries(w, b.tenant, b.blooms)
	if err != nil {
		return 0, err
	}
	nLabel, err := b.encoder.encodeLabelEntries(w, b.tenant, b.labels)
	if err != nil {
		return 0, err
	}
	b.Reset()
	return nBloom + nLabel, nil
}
