package postings

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// Builder aggregates posting observations and builds bitmaps and bloom filters
// incrementally. Call [Builder.Flush] to encode all accumulated data and write
// a postings section to the provided [dataobj.SectionWriter].
type Builder struct {
	metrics *Metrics
	tenant  string
	labels  *labelAggregator
	blooms  *bloomAggregator

	pageSizeHint    int
	pageMaxRowCount int
}

// pageSizeHint and pageMaxRowCount control page splitting of the underlying
// column builders (0 means use defaults).
// metrics may be nil to disable instrumentation.
func NewBuilder(metrics *Metrics, pageSizeHint, pageMaxRowCount int) *Builder {
	return &Builder{
		metrics:         metrics,
		labels:          newLabelAggregator(),
		blooms:          newBloomAggregator(),
		pageSizeHint:    pageSizeHint,
		pageMaxRowCount: pageMaxRowCount,
	}
}

// SetTenant sets the tenant for this builder.
func (b *Builder) SetTenant(tenant string) { b.tenant = tenant }

// Tenant returns the tenant for this builder.
func (b *Builder) Tenant() string { return b.tenant }

// Type returns the [dataobj.SectionType] of the postings builder.
func (b *Builder) Type() dataobj.SectionType { return sectionType }

// PrepareBloomColumn initializes the bloom filter for a specific column. Must
// be called before any ObserveBloomPosting calls for the given
// (objectPath, sectionIndex, columnName) combination.
func (b *Builder) PrepareBloomColumn(objectPath string, sectionIndex int64, columnName string, estimatedCardinality uint) {
	b.blooms.PrepareColumn(objectPath, sectionIndex, columnName, estimatedCardinality)
}

// ObserveLabelPosting records a label posting observation. Multiple
// observations for the same (ObjectPath, SectionIndex, ColumnName, LabelValue)
// key are aggregated into a single posting.
func (b *Builder) ObserveLabelPosting(obs LabelObservation) {
	b.labels.Observe(obs)
}

// ObserveBloomPosting records a bloom posting observation. Returns an error if
// the column has not been prepared via PrepareBloomColumn.
func (b *Builder) ObserveBloomPosting(obs BloomObservation) error {
	return b.blooms.Observe(obs)
}

// BloomBytes returns the marshaled bloom filter bytes for a specific column.
// Returns an error if the column has not been prepared.
func (b *Builder) BloomBytes(objectPath string, sectionIndex int64, columnName string) ([]byte, error) {
	return b.blooms.BloomBytes(objectPath, sectionIndex, columnName)
}

// EstimatedSize returns an estimate of the encoded size of the accumulated
// data in bytes.
func (b *Builder) EstimatedSize() int {
	return b.labels.EstimatedSize() + b.blooms.EstimatedSize()
}

// Reset clears all accumulated data and resets the builder to a fresh state.
func (b *Builder) Reset() {
	b.labels.Reset()
	b.blooms.Reset()
}

// Flush encodes all accumulated observations into the provided
// [dataobj.SectionWriter] and returns the number of bytes written.
//
// After a successful flush, the builder is reset.
func (b *Builder) Flush(w dataobj.SectionWriter) (n int64, err error) {
	labelEntries := b.labels.Entries()
	bloomEntries, err := b.blooms.Entries()
	if err != nil {
		return 0, fmt.Errorf("converting bloom entries: %w", err)
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
