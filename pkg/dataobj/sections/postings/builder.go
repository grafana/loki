package postings

import (
	"fmt"
	"sort"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
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
// observations for the same
// (ObjectPath, SectionIndex, ColumnName, LabelValue) key are aggregated into a
// single posting. Returns an error if the key was already added via
// AppendLabelEntry.
func (b *Builder) ObserveLabelPosting(obs LabelObservation) error {
	return b.labels.Observe(obs)
}

// ObserveBloomPosting records a bloom posting observation. Returns an error if
// the column has not been prepared via PrepareBloomColumn, or if the same
// (ObjectPath, SectionIndex, ColumnName) key was previously added via
// AppendBloomEntry.
func (b *Builder) ObserveBloomPosting(obs BloomObservation) error {
	return b.blooms.Observe(obs)
}

// BloomBytes returns the marshaled bloom filter bytes for a specific column.
// Returns an error if the column has not been prepared.
func (b *Builder) BloomBytes(objectPath string, sectionIndex int64, columnName string) ([]byte, error) {
	return b.blooms.BloomBytes(objectPath, sectionIndex, columnName)
}

// AppendLabelEntry appends a pre-aggregated label posting entry directly to
// the builder, bypassing the per-observation aggregation path. This is useful
// when replaying pre-aggregated rows from source sections, as it prevents
// double-counting of UncompressedSize. Returns an error if an entry with the
// same (ObjectPath, SectionIndex, ColumnName, LabelValue) key already exists.
func (b *Builder) AppendLabelEntry(entry LabelEntry) error {
	// Reconstruct the bitmap from raw bytes
	internalEntry := &labelPostingEntry{
		ObjectPath:       entry.ObjectPath,
		SectionIndex:     entry.SectionIndex,
		ColumnName:       entry.ColumnName,
		LabelValue:       entry.LabelValue,
		MinTimestamp:     entry.MinTimestamp.UnixNano(),
		MaxTimestamp:     entry.MaxTimestamp.UnixNano(),
		UncompressedSize: entry.UncompressedSize,
	}

	// Reconstruct the bitmap from the provided bytes
	if len(entry.StreamIDBitmap) == 0 {
		// Empty bitmap
		internalEntry.bitmap = memory.NewBitmap(nil, 0)
	} else {
		// Create a bitmap from the raw bytes
		// Calculate the bitmap length from the bytes
		bitmapLen := len(entry.StreamIDBitmap) * 8
		internalEntry.bitmap = memory.BitmapFrom(entry.StreamIDBitmap, bitmapLen, 0)
	}

	return b.labels.AppendEntry(internalEntry)
}

// AppendBloomEntry appends a pre-aggregated bloom posting entry directly to
// the builder, bypassing the per-observation aggregation path. This is useful
// when replaying pre-aggregated rows from source sections, as it prevents
// double-counting of UncompressedSize. Returns an error if an entry with the
// same (ObjectPath, SectionIndex, ColumnName) key already exists.
func (b *Builder) AppendBloomEntry(entry BloomEntry) error {
	// Unmarshal the bloom filter from the provided bytes
	bloomFilter := &bloom.BloomFilter{}
	if err := bloomFilter.UnmarshalBinary(entry.BloomFilter); err != nil {
		return fmt.Errorf("unmarshaling bloom filter: %w", err)
	}

	// Reconstruct the bitmap from raw bytes
	internalEntry := &bloomPostingEntry{
		ObjectPath:       entry.ObjectPath,
		SectionIndex:     entry.SectionIndex,
		ColumnName:       entry.ColumnName,
		bloomFilter:      bloomFilter,
		MinTimestamp:     entry.MinTimestamp.UnixNano(),
		MaxTimestamp:     entry.MaxTimestamp.UnixNano(),
		UncompressedSize: entry.UncompressedSize,
	}

	// Reconstruct the bitmap from the provided bytes
	if len(entry.StreamIDBitmap) == 0 {
		// Empty bitmap
		internalEntry.bitmap = memory.NewBitmap(nil, 0)
	} else {
		// Create a bitmap from the raw bytes
		// Calculate the bitmap length from the bytes
		bitmapLen := len(entry.StreamIDBitmap) * 8
		internalEntry.bitmap = memory.BitmapFrom(entry.StreamIDBitmap, bitmapLen, 0)
	}

	return b.blooms.AppendEntry(internalEntry)
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
	bloomEntries := b.blooms.Entries()

	if len(labelEntries) == 0 && len(bloomEntries) == 0 {
		return 0, nil
	}

	if b.metrics != nil {
		timer := prometheus.NewTimer(b.metrics.encodeSeconds)
		defer timer.ObserveDuration()
	}

	// Sort label entries by [objectPath, sectionIndex, columnName, labelValue].
	sort.SliceStable(labelEntries, func(i, j int) bool {
		a, bEntry := labelEntries[i], labelEntries[j]
		if a.ObjectPath != bEntry.ObjectPath {
			return a.ObjectPath < bEntry.ObjectPath
		}
		if a.SectionIndex != bEntry.SectionIndex {
			return a.SectionIndex < bEntry.SectionIndex
		}
		if a.ColumnName != bEntry.ColumnName {
			return a.ColumnName < bEntry.ColumnName
		}
		return a.LabelValue < bEntry.LabelValue
	})

	// Sort bloom entries by [objectPath, sectionIndex, columnName].
	sort.SliceStable(bloomEntries, func(i, j int) bool {
		a, bEntry := bloomEntries[i], bloomEntries[j]
		if a.ObjectPath != bEntry.ObjectPath {
			return a.ObjectPath < bEntry.ObjectPath
		}
		if a.SectionIndex != bEntry.SectionIndex {
			return a.SectionIndex < bEntry.SectionIndex
		}
		return a.ColumnName < bEntry.ColumnName
	})

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
