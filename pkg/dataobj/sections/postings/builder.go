package postings

import (
	"fmt"
	"sort"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// Builder accumulates [Posting] rows and encodes them into a section via a
// [dataobj.SectionWriter].
//
// Flush writes are done via [Builder.Flush], which encodes all accumulated
// rows and writes them to the provided [dataobj.SectionWriter].
type Builder struct {
	metrics *Metrics
	tenant  string
	encode  SectionEncoder

	rows []Posting
}

// NewBuilder creates a new Builder.
// encode is the [SectionEncoder] to use for encoding rows.
// metrics may be nil to disable instrumentation.
func NewBuilder(metrics *Metrics, encode SectionEncoder) *Builder {
	return &Builder{
		metrics: metrics,
		encode:  encode,
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
// MinTimestamp, MaxTimestamp], encodes them into the provided
// [dataobj.SectionWriter], and returns the number of bytes written.
//
// After a successful flush, the builder is reset.
func (b *Builder) Flush(w dataobj.SectionWriter) (n int64, err error) {
	if len(b.rows) == 0 {
		return 0, nil
	}

	if b.metrics != nil {
		timer := prometheus.NewTimer(b.metrics.encodeSeconds)
		defer timer.ObserveDuration()
	}

	// Sort rows by [Kind, ColumnName, LabelValue, MinTimestamp, MaxTimestamp] (lexicographic).
	sort.SliceStable(b.rows, func(i, j int) bool {
		return comparePostings(b.rows[i], b.rows[j])
	})

	var enc columnar.Encoder
	defer enc.Reset()

	if err := b.encode(b.rows, &enc); err != nil {
		return 0, fmt.Errorf("encoding postings: %w", err)
	}

	enc.SetTenant(b.tenant)
	n, err = enc.Flush(w)
	if err == nil {
		b.Reset()
	}
	return n, err
}
