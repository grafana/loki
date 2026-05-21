package indexobj

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/scratch"
)

// MergeBuilder accumulates pre-aggregated posting and stats entries from
// existing sections for merging. Unlike Builder, which aggregates per-observation,
// MergeBuilder accepts already-aggregated entries and forwards them to
// merge-mode sub-builders.
//
// Methods on MergeBuilder are not goroutine-safe; callers are responsible for
// synchronization.
type MergeBuilder struct {
	cfg     logsobj.BuilderBaseConfig
	metrics *builderMetrics

	currentSizeEstimate int
	builderFull         bool

	builder  *dataobj.Builder                  // Inner builder for accumulating sections.
	stats    map[string]*stats.Builder         // The key is the TenantID.
	postings map[string]*postings.MergeBuilder // The key is the TenantID.

	unflushedSizeEstimate int

	state builderState
}

// NewMergeBuilder creates a new [MergeBuilder] for merging pre-aggregated
// posting and stats entries.
//
// NewMergeBuilder returns an error if the provided config is invalid.
func NewMergeBuilder(cfg logsobj.BuilderBaseConfig, scratchStore scratch.Store) (*MergeBuilder, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	metrics := newBuilderMetrics()
	metrics.ObserveConfig(cfg)

	return &MergeBuilder{
		cfg:     cfg,
		metrics: metrics,

		builder:  dataobj.NewBuilder(scratchStore),
		stats:    make(map[string]*stats.Builder),
		postings: make(map[string]*postings.MergeBuilder),
	}, nil
}

func (b *MergeBuilder) GetEstimatedSize() int {
	return b.currentSizeEstimate
}

func (b *MergeBuilder) IsFull() bool {
	return b.builderFull
}

func (b *MergeBuilder) getStatsBuilderForTenant(tenantID string) *stats.Builder {
	if _, ok := b.stats[tenantID]; !ok {
		sb := stats.NewBuilder(b.metrics.stats, stats.ColumnarSectionEncoder(int(b.cfg.TargetPageSize), b.cfg.MaxPageRows))
		sb.SetTenant(tenantID)
		b.stats[tenantID] = sb
	}
	return b.stats[tenantID]
}

func (b *MergeBuilder) getPostingsMergeBuilderForTenant(tenantID string) *postings.MergeBuilder {
	if _, ok := b.postings[tenantID]; !ok {
		pmb := postings.NewMergeBuilder(b.metrics.postings, int(b.cfg.TargetPageSize), b.cfg.MaxPageRows)
		pmb.SetTenant(tenantID)
		b.postings[tenantID] = pmb
	}
	return b.postings[tenantID]
}

// AppendStat records a per-sort-key aggregate for a data object section.
func (b *MergeBuilder) AppendStat(tenantID, objectPath string, sectionIdx int64,
	sortSchema string, labels map[string]string, minTs, maxTs time.Time, rows int, uncompressedSize int64) error {
	b.metrics.appendsTotal.Inc()

	tenantStats := b.getStatsBuilderForTenant(tenantID)
	preAppendSizeEstimate := tenantStats.EstimatedSize()

	tenantStats.Append(stats.Stat{
		ObjectPath:       objectPath,
		SectionIndex:     sectionIdx,
		SortSchema:       sortSchema,
		Labels:           labels,
		MinTimestamp:     minTs.UnixNano(),
		MaxTimestamp:     maxTs.UnixNano(),
		RowCount:         int64(rows),
		UncompressedSize: uncompressedSize,
	})

	postAppendSizeEstimate := tenantStats.EstimatedSize()
	b.unflushedSizeEstimate += postAppendSizeEstimate - preAppendSizeEstimate

	if postAppendSizeEstimate > int(b.cfg.TargetSectionSize) {
		if err := b.builder.Append(tenantStats); err != nil {
			return err
		}
	}

	b.currentSizeEstimate = b.estimatedSize()
	b.state = builderStateDirty
	if b.currentSizeEstimate > int(b.cfg.TargetObjectSize) {
		b.builderFull = true
	}

	return nil
}

// AppendPostingsLabelEntry appends an aggregated label posting entry
// directly to the builder.
func (b *MergeBuilder) AppendPostingsLabelEntry(tenantID string, entry postings.LabelEntry) error {
	b.metrics.appendsTotal.Inc()

	tenantPostings := b.getPostingsMergeBuilderForTenant(tenantID)
	preSize := tenantPostings.EstimatedSize()

	if err := tenantPostings.AppendLabelEntry(entry); err != nil {
		return err
	}

	postSize := tenantPostings.EstimatedSize()
	b.unflushedSizeEstimate += postSize - preSize
	b.currentSizeEstimate += postSize - preSize
	b.state = builderStateDirty
	if b.currentSizeEstimate > int(b.cfg.TargetObjectSize) {
		b.builderFull = true
	}
	return nil
}

// AppendPostingsBloomEntry appends an aggregated bloom posting entry
// directly to the builder.
func (b *MergeBuilder) AppendPostingsBloomEntry(tenantID string, entry postings.BloomEntry) error {
	b.metrics.appendsTotal.Inc()

	tenantPostings := b.getPostingsMergeBuilderForTenant(tenantID)
	preSize := tenantPostings.EstimatedSize()

	if err := tenantPostings.AppendBloomEntry(entry); err != nil {
		return err
	}

	postSize := tenantPostings.EstimatedSize()
	b.unflushedSizeEstimate += postSize - preSize
	b.currentSizeEstimate += postSize - preSize
	b.state = builderStateDirty
	if b.currentSizeEstimate > int(b.cfg.TargetObjectSize) {
		b.builderFull = true
	}
	return nil
}

func (b *MergeBuilder) estimatedSize() int {
	var size int
	size += b.unflushedSizeEstimate
	size += b.builder.Bytes()
	b.metrics.sizeEstimate.Set(float64(size))
	return size
}

// Flush flushes all buffered data and returns the built object.
//
// [MergeBuilder.Reset] is called after a successful Flush to discard any pending
// data and allow new data to be appended.
func (b *MergeBuilder) Flush() (*dataobj.Object, io.Closer, error) {
	if b.state == builderStateEmpty {
		return nil, nil, ErrBuilderEmpty
	}

	b.metrics.flushTotal.Inc()
	timer := prometheus.NewTimer(b.metrics.buildTime)
	defer timer.ObserveDuration()

	var flushErrors []error

	for _, tenantStats := range b.stats {
		if tenantStats.EstimatedSize() > 0 {
			flushErrors = append(flushErrors, b.builder.Append(tenantStats))
		}
	}
	for _, tenantPostings := range b.postings {
		if tenantPostings.EstimatedSize() > 0 {
			flushErrors = append(flushErrors, b.builder.Append(tenantPostings))
		}
	}

	if err := errors.Join(flushErrors...); err != nil {
		b.metrics.flushFailures.Inc()
		return nil, nil, fmt.Errorf("building object: %w", err)
	}

	obj, closer, err := b.builder.Flush()
	if err != nil {
		b.metrics.flushFailures.Inc()
		return nil, nil, fmt.Errorf("flushing object: %w", err)
	}

	b.metrics.builtSize.Observe(float64(obj.Size()))

	err = b.observeObject(context.Background(), obj)

	b.Reset()
	return obj, closer, err
}

func (b *MergeBuilder) observeObject(ctx context.Context, obj *dataobj.Object) error {
	var errs []error

	errs = append(errs, b.metrics.dataobj.Observe(obj))

	for _, sec := range obj.Sections() {
		switch {
		case postings.CheckSection(sec):
			postingSection, err := postings.Open(ctx, sec)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			errs = append(errs, b.metrics.postings.Observe(ctx, postingSection))
		case stats.CheckSection(sec):
			statSection, err := stats.Open(ctx, sec)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			errs = append(errs, b.metrics.stats.Observe(ctx, statSection))
		}
	}

	return errors.Join(errs...)
}

// Reset discards pending data and resets the builder to an empty state.
func (b *MergeBuilder) Reset() {
	b.builder.Reset()
	b.stats = make(map[string]*stats.Builder)
	b.postings = make(map[string]*postings.MergeBuilder)

	b.metrics.sizeEstimate.Set(0)
	b.currentSizeEstimate = 0
	b.unflushedSizeEstimate = 0
	b.builderFull = false
	b.state = builderStateEmpty
}

// RegisterMetrics registers metrics about builder to report to reg. All
// metrics will have a tenant label set to the tenant ID of the Builder.
//
// If multiple Builders for the same tenant are running in the same process,
// reg must contain additional labels to differentiate between them.
func (b *MergeBuilder) RegisterMetrics(reg prometheus.Registerer) error {
	return b.metrics.Register(reg)
}

// UnregisterMetrics unregisters metrics about builder from reg.
func (b *MergeBuilder) UnregisterMetrics(reg prometheus.Registerer) {
	b.metrics.Unregister(reg)
}
