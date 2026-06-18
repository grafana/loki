package stats

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// Builder accumulates [Stat] rows and encodes them into a section via a
// [dataobj.SectionWriter].
//
// Flush writes are done via [Builder.Flush], which encodes all accumulated
// rows and writes them to the provided [dataobj.SectionWriter].
type Builder struct {
	metrics *Metrics
	tenant  string
	encode  SectionEncoder

	rows []Stat
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

// Compare reports the canonical sort order of two [Stat] rows. It returns a
// negative value if a sorts before b, a positive value if a sorts after b, and
// zero if they share the full key.
//
// Both rows must share the same SortSchema.
func Compare(a, b Stat) int {
	// Iterates the SortSchema with [strings.SplitSeq] so the function does not
	// allocate per comparison; the flush sort invokes it O(n log n) times.
	for key := range strings.SplitSeq(a.SortSchema, ",") {
		if va, vb := a.Labels[key], b.Labels[key]; va != vb {
			return strings.Compare(va, vb)
		}
	}
	if a.MinTimestamp != b.MinTimestamp {
		return cmp.Compare(a.MinTimestamp, b.MinTimestamp)
	}
	if a.MaxTimestamp != b.MaxTimestamp {
		return cmp.Compare(a.MaxTimestamp, b.MaxTimestamp)
	}
	if a.ObjectPath != b.ObjectPath {
		return strings.Compare(a.ObjectPath, b.ObjectPath)
	}
	return cmp.Compare(a.SectionIndex, b.SectionIndex)
}

// Flush sorts the accumulated rows with [Compare], encodes them via the
// [SectionEncoder], and writes the result to the provided
// [dataobj.SectionWriter].
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

	slices.SortStableFunc(b.rows, Compare)

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
