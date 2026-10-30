package indexobj

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v4/pkg/dataobj"
	"github.com/grafana/loki/v4/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v4/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v4/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v4/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v4/pkg/scratch"
)

// MergeBuilder accumulates aggregated posting and stat entries from
// existing sections for merging. Unlike Builder, which aggregates per-observation,
// MergeBuilder accepts already-aggregated entries and forwards them to
// merge-mode sub-builders.
//
// A MergeBuilder builds the index of one tenant, whose data objects share one
// stream layout. Every stats section records that layout. A MergeBuilder
// built with the zero layout accepts postings but no stats, because it has no
// layout to record.
//
// Methods on MergeBuilder are not goroutine-safe; callers are responsible for
// synchronization.
type MergeBuilder struct {
	cfg     logsobj.BuilderBaseConfig
	metrics *BuilderMetrics
	tenant  string

	currentSizeEstimate int
	builderFull         bool

	builder  *dataobj.Builder
	stats    *stats.Builder
	layout   streams.Layout
	postings *postings.MergeBuilder

	unflushedSizeEstimate int

	state builderState
}

// NewMergeBuilder creates a new [MergeBuilder] for merging pre-aggregated
// posting and stats entries of tenant, whose data objects have layout.
//
// NewMergeBuilder returns an error if the provided config is invalid or
// tenant is empty.
func NewMergeBuilder(cfg logsobj.BuilderBaseConfig, scratchStore scratch.Store, tenant string, layout streams.Layout) (*MergeBuilder, error) {
	if tenant == "" {
		return nil, errors.New("tenant must not be empty")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	metrics := NewBuilderMetrics(nil)
	metrics.ObserveConfig(cfg)

	statsBuilder := stats.NewBuilder(metrics.stats, stats.ColumnarSectionEncoder(int(cfg.TargetPageSize), cfg.MaxPageRows), layout)
	statsBuilder.SetTenant(tenant)
	postingsBuilder := postings.NewMergeBuilder(metrics.postings, int(cfg.TargetPageSize), cfg.MaxPageRows, int(cfg.TargetSectionSize))
	postingsBuilder.SetTenant(tenant)

	return &MergeBuilder{
		cfg:     cfg,
		metrics: metrics,
		tenant:  tenant,

		builder:  dataobj.NewBuilder(scratchStore),
		stats:    statsBuilder,
		layout:   layout,
		postings: postingsBuilder,
	}, nil
}

// checkTenant returns an error if tenantID is not the tenant of b.
func (b *MergeBuilder) checkTenant(tenantID string) error {
	if tenantID != b.tenant {
		return fmt.Errorf("merge builder for tenant %q got an entry of tenant %q", b.tenant, tenantID)
	}
	return nil
}

func (b *MergeBuilder) GetEstimatedSize() int {
	return b.currentSizeEstimate
}

func (b *MergeBuilder) IsFull() bool {
	return b.builderFull
}

// AppendStat records a per-sort-key aggregate for a data object section.
func (b *MergeBuilder) AppendStat(tenantID string, stat stats.Stat) error {
	b.metrics.appendsTotal.Inc()
	if err := b.checkTenant(tenantID); err != nil {
		return err
	}

	if b.layout.StreamOrder() == streams.StreamOrderUnspecified {
		return errors.New("merge builder without a stream layout got a stat")
	}

	tenantStats := b.stats
	preAppendSizeEstimate := tenantStats.EstimatedSize()

	tenantStats.Append(stat)

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
	if err := b.checkTenant(tenantID); err != nil {
		return err
	}

	tenantPostings := b.postings
	preSize := tenantPostings.EstimatedSize()

	if err := tenantPostings.AppendLabelEntry(entry); err != nil {
		return err
	}

	postSize := tenantPostings.EstimatedSize()
	b.unflushedSizeEstimate += postSize - preSize

	if postSize > int(b.cfg.TargetSectionSize) {
		if err := b.builder.Append(tenantPostings); err != nil {
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

// AppendPostingsBloomEntry appends an aggregated bloom posting entry
// directly to the builder.
func (b *MergeBuilder) AppendPostingsBloomEntry(tenantID string, entry postings.BloomEntry) error {
	b.metrics.appendsTotal.Inc()
	if err := b.checkTenant(tenantID); err != nil {
		return err
	}

	tenantPostings := b.postings
	preSize := tenantPostings.EstimatedSize()

	if err := tenantPostings.AppendBloomEntry(entry); err != nil {
		return err
	}

	postSize := tenantPostings.EstimatedSize()
	b.unflushedSizeEstimate += postSize - preSize

	// Cut a section at the target size; see AppendPostingsLabelEntry.
	if postSize > int(b.cfg.TargetSectionSize) {
		if err := b.builder.Append(tenantPostings); err != nil {
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

func (b *MergeBuilder) estimatedSize() int {
	var size int
	size += b.unflushedSizeEstimate
	size += b.builder.Bytes()
	b.metrics.sizeEstimate.Set(float64(size))
	return size
}

// Flush flushes all buffered data and returns the built object.
//
// On success the caller owns the returned [io.Closer] and must close it to
// release the object's backing scratch storage; reads of the object fail once
// it is closed. If an error is returned the closer is always nil.
//
// Flush always resets MergeBuilder.
func (b *MergeBuilder) Flush() (*dataobj.Object, io.Closer, error) {
	if b.state == builderStateEmpty {
		return nil, nil, ErrBuilderEmpty
	}
	defer b.Reset()

	b.metrics.flushTotal.Inc()
	timer := prometheus.NewTimer(b.metrics.buildTime)
	defer timer.ObserveDuration()

	var flushErrors []error

	if b.stats.EstimatedSize() > 0 {
		flushErrors = append(flushErrors, b.builder.Append(b.stats))
	}
	if b.postings.EstimatedSize() > 0 {
		flushErrors = append(flushErrors, b.builder.Append(b.postings))
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

	if err := b.observeObject(context.Background(), obj); err != nil {
		return nil, nil, errors.Join(fmt.Errorf("observing object: %w", err), closer.Close())
	}

	return obj, closer, nil
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
	b.stats.Reset()
	b.postings.Reset()

	b.metrics.sizeEstimate.Set(0)
	b.currentSizeEstimate = 0
	b.unflushedSizeEstimate = 0
	b.builderFull = false
	b.state = builderStateEmpty
}
