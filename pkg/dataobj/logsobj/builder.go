// Package logsobj provides tooling for creating logs-oriented data objects.
package logsobj

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/facette/natsort"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/scratch"
)

// ErrBuilderEmpty is returned by the flush methods of [Builder] when there is
// no buffered data to flush.
var (
	ErrBuilderEmpty = errors.New("builder empty")

	// ErrFlushDropDuplicates is returned by [Builder.Flush] when DropDuplicates is set.
	ErrFlushDropDuplicates = errors.New("flush does not support DropDuplicates, use FlushAndSort")
)

// BuilderBaseConfig configures a data object builder.
type BuilderBaseConfig struct {
	// TargetPageSize configures a target size for encoded pages within the data
	// object. TargetPageSize accounts for encoding, but not for compression.
	TargetPageSize flagext.Bytes `yaml:"target_page_size"`

	// MaxPageRows configures a maximum row count for encoded pages within the data
	// object. If set to 0 or negative number, the page size will not be limited by a
	// row count.
	MaxPageRows int `yaml:"max_page_rows"`

	// TODO(rfratto): We need an additional parameter for TargetMetadataSize, as
	// metadata payloads can't be split and must be downloaded in a single
	// request.
	//
	// At the moment, we don't have a good mechanism for implementing a metadata
	// size limit (we need to support some form of section splitting or column
	// combinations), so the option is omitted for now.

	// TargetObjectSize configures a target size for data objects.
	TargetObjectSize flagext.Bytes `yaml:"target_object_size"`

	// TargetSectionSize configures the maximum size of data in a section. Sections
	// which support this parameter will place overflow data into new sections of
	// the same type.
	TargetSectionSize flagext.Bytes `yaml:"target_section_size"`

	// BufferSize configures the size of the buffer used to accumulate
	// uncompressed logs in memory prior to sorting.
	BufferSize flagext.Bytes `yaml:"buffer_size"`

	// SectionStripeMergeLimit configures the number of stripes to merge at once when
	// flushing stripes into a section. MergeSize must be larger than 1. Lower
	// values of MergeSize trade off lower memory overhead for higher time spent
	// merging.
	SectionStripeMergeLimit int `yaml:"section_stripe_merge_limit"`

	// EstimatedCompressionRatio is the expected compression ratio for log data,
	// used to approximate compressed output size from uncompressed buffered
	// records. This only takes effect when using the AppendOrdered strategy.
	// Higher values allow more data to accumulate before the builder reports
	// full, producing larger objects. Set to either 0 or 1 to disable.
	EstimatedCompressionRatio int `yaml:"estimated_compression_ratio"`

	// DropDuplicates indicates whether duplicate Log records should be dropped or retained during FlushAndSort.
	// Log records are considered duplicates if they have the same stream ID, timestamp, metadata, and line bytes.
	// Flush returns an error if DropDuplicates is set.
	DropDuplicates bool `yaml:"-"`
}

// RegisterFlagsWithPrefix registers flags with the given prefix.
func (cfg *BuilderBaseConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.Var(&cfg.TargetPageSize, prefix+"target-page-size", "The target maximum amount of uncompressed data to hold in data pages (for columnar sections). Uncompressed size is used for consistent I/O and planning.")
	f.IntVar(&cfg.MaxPageRows, prefix+"max-page-rows", 10000, "The maximum row count for pages to use for the data object builder. A value of 0 means no limit.")
	f.Var(&cfg.TargetObjectSize, prefix+"target-builder-memory-limit", "The target maximum size of the encoded object and all of its encoded sections (after compression), to limit memory usage of a builder.")
	f.Var(&cfg.TargetSectionSize, prefix+"target-section-size", "The target maximum amount of uncompressed data to hold in sections, for sections that support being limited by size. Uncompressed size is used for consistent I/O and planning.")
	f.Var(&cfg.BufferSize, prefix+"buffer-size", "The size of logs to buffer in memory before adding into columnar builders, used to reduce CPU load of sorting.")
	f.IntVar(&cfg.SectionStripeMergeLimit, prefix+"section-stripe-merge-limit", 2, "The maximum number of dataobj section stripes to merge into a section at once. Must be greater than 1.")
	f.IntVar(&cfg.EstimatedCompressionRatio, prefix+"estimated-compression-ratio", 8, "Expected compression ratio for log data, used to estimate compressed output size from uncompressed buffered records. Only takes effect with ordered append. Set to either 0 or 1 to disable.")
}

// Validate validates the BuilderBaseConfig.
func (cfg *BuilderBaseConfig) Validate() error {
	var errs []error

	if cfg.TargetPageSize <= 0 {
		errs = append(errs, errors.New("TargetPageSize must be greater than 0"))
	} else if cfg.TargetPageSize >= cfg.TargetObjectSize {
		errs = append(errs, errors.New("TargetPageSize must be less than TargetObjectSize"))
	}

	if cfg.TargetObjectSize <= 0 {
		errs = append(errs, errors.New("TargetObjectSize must be greater than 0"))
	}

	if cfg.BufferSize <= 0 {
		errs = append(errs, errors.New("BufferSize must be greater than 0"))
	}

	if cfg.TargetSectionSize <= 0 || cfg.TargetSectionSize > cfg.TargetObjectSize {
		errs = append(errs, errors.New("SectionSize must be greater than 0 and less than or equal to TargetObjectSize"))
	}

	if cfg.SectionStripeMergeLimit < 2 {
		errs = append(errs, errors.New("LogsMergeStripesMax must be greater than 1"))
	}

	return errors.Join(errs...)
}

// TenantOverrides provides per-tenant configuration for the Builder.
type TenantOverrides interface {
	SortSchemaLabels(tenant string) []string

	// DataObjShardLayout returns the shard layout for the streams of the tenant in a new object.
	DataObjShardLayout(tenant string) streams.ShardLayout
}

// NoTenantOverrides is a [TenantOverrides] that overrides nothing. Every tenant has no sort schema
// labels and the default shard layout.
type NoTenantOverrides struct{}

// SortSchemaLabels returns no schema labels.
func (NoTenantOverrides) SortSchemaLabels(string) []string { return nil }

// DataObjShardLayout returns the default shard layout.
func (NoTenantOverrides) DataObjShardLayout(string) streams.ShardLayout {
	return streams.DefaultShardLayout()
}

var errNilOverrides = errors.New("tenant overrides must not be nil")

// A Builder constructs a logs-oriented data object from a set of incoming
// log data. [Builder.Append] and [Builder.AppendRecord] add log data.
// [Builder.FlushAndSort] sorts the data object-wide. [Builder.Flush]
// requires the caller to append records in stream key order.
//
// Methods on Builder are not goroutine-safe; callers are responsible for
// synchronization.
type Builder struct {
	cfg       BuilderBaseConfig
	metrics   *BuilderMetrics
	overrides TenantOverrides
	logger    log.Logger
	scratch   scratch.Store

	labelCache *lru.Cache[string, labels.Labels]

	// layouts holds the layout of each tenant in the current object. Reset
	// clears it, so a limits change applies from the next object.
	layouts map[string]streams.Layout

	// lastTarget is the target of the last appended stream, or nil. Callers
	// append many records of one stream in a row, so the builder resolves a
	// target once per run of a stream, not once per record.
	//
	// Reset sets it to nil, because it holds values from the layout of the
	// current object and pointers to its streams and logs builders.
	lastTarget *streamTarget

	// sortLogsMetrics and sortStreamsMetrics are never registered. They absorb
	// the section metrics of the sort in FlushAndSort, which would otherwise
	// count data that metrics already observed during the build.
	sortLogsMetrics    *logs.Metrics
	sortStreamsMetrics *streams.Metrics

	currentSizeEstimate int

	builder *dataobj.Builder // Inner builder for accumulating sections.
	streams map[string]*streams.Builder
	logs    map[string]*logs.Builder

	// earliestRecordTime tracks the timestamp of the earliest record appended
	// to the builder. It is required for the metastore index.
	earliestRecordTime time.Time

	state builderState
}

type builderState int

const (
	// builderStateEmpty indicates the builder is empty and ready to accept new data.
	builderStateEmpty builderState = iota

	// builderStateDirty indicates the builder has been modified since the last flush.
	builderStateDirty
)

// NewBuilder creates a new [Builder] which stores log-oriented data objects.
//
// NewBuilder returns an error if the provided config is invalid.
//
// NewBuilder also returns an error if overrides is nil, so a caller cannot forget to set it. Use
// [NoTenantOverrides] to give every tenant the default layout.
//
// NewBuilder passes opts to the [dataobj.Builder] that writes the objects.
func NewBuilder(cfg BuilderBaseConfig, scratchStore scratch.Store, metrics *BuilderMetrics, logger log.Logger, overrides TenantOverrides, opts ...dataobj.BuilderOption) (*Builder, error) {
	if overrides == nil {
		return nil, errNilOverrides
	}

	labelCache, err := lru.New[string, labels.Labels](5000)
	if err != nil {
		return nil, fmt.Errorf("failed to create LRU cache: %w", err)
	}

	return &Builder{
		cfg:        cfg,
		metrics:    metrics,
		logger:     logger,
		scratch:    scratchStore,
		overrides:  overrides,
		labelCache: labelCache,
		builder:    dataobj.NewBuilder(scratchStore, append([]dataobj.BuilderOption{dataobj.WithPrioritySections(streams.SectionType())}, opts...)...),
		streams:    make(map[string]*streams.Builder),
		logs:       make(map[string]*logs.Builder),
		layouts:    make(map[string]streams.Layout),

		sortLogsMetrics:    logs.NewMetrics(),
		sortStreamsMetrics: streams.NewMetrics(),
	}, nil
}

// layoutFor returns the layout of tenant in the current object.
//
// layoutFor reads the overrides only once per tenant per object. Every
// section and stream of an object must use the same layout, and the
// overrides can change while the object is open.
func (b *Builder) layoutFor(tenant string) streams.Layout {
	if layout, ok := b.layouts[tenant]; ok {
		return layout
	}
	schemaLabels := b.overrides.SortSchemaLabels(tenant)
	shardLayout := b.overrides.DataObjShardLayout(tenant)
	layout := streams.NewLayout(shardLayout, schemaLabels)
	b.layouts[tenant] = layout
	return layout
}

// buildersFor initializes the builders for the tenant.
func (b *Builder) buildersFor(tenant string, layout streams.Layout) (*streams.Builder, *logs.Builder) {
	if _, ok := b.streams[tenant]; !ok {
		sb := streams.NewBuilder(b.metrics.streams, int(b.cfg.TargetPageSize), b.cfg.MaxPageRows, layout.ShardLayout())
		sb.SetTenant(tenant)
		b.streams[tenant] = sb
	}
	if _, ok := b.logs[tenant]; !ok {
		// TODO(ashwanth): SortSchemaASC does not support AppendUnordered.
		// It cannot merge stripes with schema ordering. Force AppendOrdered
		// until stripe merging can use schema keys.
		lb := logs.NewBuilder(b.metrics.logs, logs.BuilderOptions{
			PageSizeHint:              int(b.cfg.TargetPageSize),
			PageMaxRowCount:           b.cfg.MaxPageRows,
			BufferSize:                int(b.cfg.BufferSize),
			StripeMergeLimit:          b.cfg.SectionStripeMergeLimit,
			AppendStrategy:            logs.AppendOrdered,
			EstimatedCompressionRatio: b.cfg.EstimatedCompressionRatio,
			SortOrder:                 logs.SortSchemaASC,
			DropDuplicates:            b.cfg.DropDuplicates,
		})
		lb.SetTenant(tenant)
		b.logs[tenant] = lb
	}

	return b.streams[tenant], b.logs[tenant]
}

func (b *Builder) GetEarliestRecordTime() time.Time {
	return b.earliestRecordTime
}

func (b *Builder) GetEstimatedSize() int {
	return b.currentSizeEstimate
}

func (b *Builder) IsFull() bool {
	return b.currentSizeEstimate > int(b.cfg.TargetObjectSize)
}

// Append buffers a stream to be written to a data object.
// Callers are expected to poll [Builder.IsFull] before appending and
// to flush the builder once it reports full. Appending entries to a full
// builder is permitted.
func (b *Builder) Append(tenant string, stream logproto.Stream, recTime time.Time) error {
	b.metrics.appends.Inc()
	timer := prometheus.NewTimer(b.metrics.appendTime)
	defer timer.ObserveDuration()

	ls, err := b.parseLabels(stream.Labels)
	if err != nil {
		return err
	}

	t, err := b.targetFor(tenant, ls)
	if err != nil {
		return err
	}
	for _, entry := range stream.Entries {
		rec := logs.Record{
			Timestamp: entry.Timestamp,
			Metadata:  convertMetadata(entry.StructuredMetadata),
			Line:      []byte(entry.Line),
		}
		if err := b.appendTo(t, rec); err != nil {
			return err
		}
	}
	b.markAppended(recTime)
	return nil
}

// AppendRecord buffers a pre-existing log record to be written to a data object.
// Callers are expected to poll [Builder.IsFull] before appending and
// to flush the builder once it reports full. Appending entries to a full
// builder is permitted.
// The SchemaKey, ShardBucket, StreamHash, and StreamID fields of the given
// record are ignored and re-calculated.
func (b *Builder) AppendRecord(tenant string, ls labels.Labels, record logs.Record, ingestionTime time.Time) error {
	b.metrics.appends.Inc()
	timer := prometheus.NewTimer(b.metrics.appendTime)
	defer timer.ObserveDuration()

	t, err := b.targetFor(tenant, ls)
	if err != nil {
		return err
	}
	if err := b.appendTo(t, record); err != nil {
		return err
	}
	b.markAppended(ingestionTime)
	return nil
}

// streamTarget is where the records of one stream go in the current object.
type streamTarget struct {
	tenant string
	labels labels.Labels

	// schemaKey, hash and shard are the same for every record of the stream.
	schemaKey string
	hash      uint64
	shard     uint32

	streams *streams.Builder
	logs    *logs.Builder
}

// targetFor returns the target of the stream with labels ls of tenant. It
// returns the last target again when tenant and ls match it.
func (b *Builder) targetFor(tenant string, ls labels.Labels) (*streamTarget, error) {
	if t := b.lastTarget; t != nil && t.tenant == tenant && labels.Equal(t.labels, ls) {
		return t, nil
	}

	layout := b.layoutFor(tenant)
	schemaKey, err := ComputeSchemaKey(ls, layout.SchemaLabels())
	if err != nil {
		return nil, fmt.Errorf("compute schema key for tenant %s: %w", tenant, err)
	}
	hash := labels.StableHash(ls)
	sb, lb := b.buildersFor(tenant, layout)

	b.lastTarget = &streamTarget{
		tenant:    tenant,
		labels:    ls,
		schemaKey: schemaKey,
		hash:      hash,
		shard:     layout.ShardLayout().BucketFromHash(hash),
		streams:   sb,
		logs:      lb,
	}
	return b.lastTarget, nil
}

// appendTo appends rec to the logs builder of t and records its stream. It
// replaces the stream fields of rec with the values of t.
//
// appendTo flushes the logs builder of t as a section when it exceeds
// TargetSectionSize.
func (b *Builder) appendTo(t *streamTarget, rec logs.Record) error {
	rec.SchemaKey = t.schemaKey
	rec.ShardBucket = t.shard
	rec.StreamHash = t.hash
	rec.StreamID = t.streams.Record(t.labels, rec.Timestamp, rec.UncompressedSize())

	t.logs.Append(rec)

	if t.logs.UncompressedSize() > int(b.cfg.TargetSectionSize) {
		if err := b.builder.Append(t.logs); err != nil {
			return err
		}
		// We need to set the tenant again after flushing because the builder is reset.
		t.logs.SetTenant(t.tenant)
	}
	return nil
}

// markAppended updates the earliest record time, the size estimate and the
// state after an append of records received at recordTime.
func (b *Builder) markAppended(recordTime time.Time) {
	if b.earliestRecordTime.IsZero() || recordTime.Before(b.earliestRecordTime) {
		b.earliestRecordTime = recordTime
	}
	b.currentSizeEstimate = b.estimatedSize()
	b.state = builderStateDirty
}

func (b *Builder) parseLabels(labelString string) (labels.Labels, error) {
	cached, ok := b.labelCache.Get(labelString)
	if ok {
		return cached, nil
	}

	parsed, err := syntax.ParseLabels(labelString)
	if err != nil {
		return labels.EmptyLabels(), fmt.Errorf("failed to parse labels: %w", err)
	}
	b.labelCache.Add(labelString, parsed)
	return parsed, nil
}

func convertMetadata(md push.LabelsAdapter) labels.Labels {
	l := labels.NewScratchBuilder(len(md))

	for _, label := range md {
		l.Add(label.Name, label.Value)
	}

	l.Sort()
	return l.Labels()
}

func (b *Builder) estimatedSize() int {
	var size int
	for _, sb := range b.streams {
		size += sb.EstimatedSize()
	}
	for _, lb := range b.logs {
		size += lb.EstimatedSize()
	}
	size += b.builder.Bytes()
	return size
}

// TimeRanges returns the time ranges for each tenant.
func (b *Builder) TimeRanges() []dataobj.TimeRange {
	var timeRanges []dataobj.TimeRange
	for _, sb := range b.streams {
		minTime, maxTime := sb.TimeRange()
		timeRanges = append(timeRanges, dataobj.TimeRange{
			Tenant:  sb.Tenant(),
			MinTime: minTime,
			MaxTime: maxTime,
		})
	}
	return timeRanges
}

// Flush builds a data object from all buffered data, as appended, and records
// each tenant's layout in its streams section. Calling Flush on an empty
// builder returns [ErrBuilderEmpty].
//
// The caller must append records in stream key order, so stream IDs and rows
// already follow the layout. The recorded layout states this order, and
// readers trust it without a check. Use [Builder.FlushAndSort] otherwise.
//
// On success the caller owns the returned [io.Closer] and must close it to
// release the object's backing scratch storage; reads of the object fail once
// it is closed. If an error is returned the closer is always nil.
//
// Flush returns [ErrFlushDropDuplicates] if DropDuplicates is set and the
// builder is not empty. The logs sections drop duplicates after the streams
// section counted them, so the stream stats would be wrong. Use
// [Builder.FlushAndSort] instead.
//
// Flush always resets Builder.
func (b *Builder) Flush() (*dataobj.Object, io.Closer, error) {
	if b.cfg.DropDuplicates && b.state != builderStateEmpty {
		b.Reset()
		b.metrics.flushFailures.Inc()
		return nil, nil, ErrFlushDropDuplicates
	}
	return b.flush(true)
}

// flushWithoutLayout builds a data object from all buffered data, as appended, without
// a layout. Its output is only the input to the sort in FlushAndSort.
//
// Each logs section is sorted, but stream IDs follow the order in which
// streams were first appended, and sections can overlap. Readers that need a
// layout reject the object, so it is not valid to store.
func (b *Builder) flushWithoutLayout() (*dataobj.Object, io.Closer, error) {
	return b.flush(false)
}

// flush builds the object and always resets Builder. If recordLayouts is
// true, each streams section records the layout of its tenant.
func (b *Builder) flush(recordLayouts bool) (*dataobj.Object, io.Closer, error) {
	if b.state == builderStateEmpty {
		return nil, nil, ErrBuilderEmpty
	}
	defer b.Reset()

	timer := prometheus.NewTimer(b.metrics.buildTime)
	defer timer.ObserveDuration()

	// Flush sections one more time in case they have data.
	var flushErrors []error

	for tenant, sb := range b.streams {
		if recordLayouts {
			layout, ok := b.layouts[tenant]
			if !ok {
				flushErrors = append(flushErrors, fmt.Errorf("no layout for tenant %s", tenant))
				continue
			}
			sb.SetLayout(layout)
		}
		flushErrors = append(flushErrors, b.builder.Append(sb))
	}
	for _, lb := range b.logs {
		flushErrors = append(flushErrors, b.builder.Append(lb))
	}

	if err := errors.Join(flushErrors...); err != nil {
		b.metrics.flushFailures.Inc()
		return nil, nil, fmt.Errorf("building object: %w", err)
	}

	obj, closer, err := b.builder.Flush()
	if err != nil {
		b.metrics.flushFailures.Inc()
		return nil, nil, fmt.Errorf("building object: %w", err)
	}

	b.metrics.builtSize.Observe(float64(obj.Size()))

	if err := b.observeObject(context.Background(), obj); err != nil {
		return nil, nil, errors.Join(fmt.Errorf("observing object: %w", err), closer.Close())
	}

	return obj, closer, nil
}

// FlushAndSort builds a data object from all buffered data and sorts it
// object-wide. Calling FlushAndSort on an empty builder returns
// [ErrBuilderEmpty].
//
// The object holds the streams section of each tenant first, then the logs
// sections of each tenant. Both groups are in natural tenant order. Stream
// IDs follow stream key order. Logs rows are ordered by stream ID, then by
// timestamp descending.
//
// The sort uses the layout that the builder used for each tenant, so it never
// reads the overrides. Every section already follows that layout, so the sort
// is a k-way merge of the sections.
//
// The caller owns the returned [io.Closer], as for [Builder.Flush].
// FlushAndSort always resets Builder.
func (b *Builder) FlushAndSort(ctx context.Context) (*dataobj.Object, io.Closer, error) {
	layouts := maps.Clone(b.layouts)
	built, builtCloser, err := b.flushWithoutLayout()
	if err != nil {
		return nil, nil, err
	}

	// A failure to release the built object leaks scratch storage, but the
	// sorted object is still valid. So the failure is counted and logged, and
	// it does not fail the flush.
	defer func() {
		if err := builtCloser.Close(); err != nil {
			b.metrics.releaseFailures.Inc()
			level.Warn(b.logger).Log("msg", "failed to release built data object", "err", err)
		}
	}()

	timer := prometheus.NewTimer(b.metrics.sortTime)
	sorted, sortedCloser, err := b.sort(ctx, built, layouts)
	timer.ObserveDuration()
	if err != nil {
		b.metrics.flushFailures.Inc()
		// The caller never closes an object returned with an error, so release
		// it here. err already reports the failure, so a close error adds
		// nothing.
		if sortedCloser != nil {
			_ = sortedCloser.Close()
		}
		return nil, nil, fmt.Errorf("sorting object: %w", err)
	}
	return sorted, sortedCloser, nil
}

// sort writes obj sorted object-wide to b.builder and flushes it. layouts must
// hold the layout of every tenant in obj, and every logs section of obj must
// follow the layout of its tenant. obj must stay open until sort returns.
//
// sort always resets Builder.
func (b *Builder) sort(ctx context.Context, obj *dataobj.Object, layouts map[string]streams.Layout) (*dataobj.Object, io.Closer, error) {
	defer b.Reset()

	// Sort the set of tenants so the new object has a deterministic order of sections.
	tenants := obj.Tenants()
	natsort.Sort(tenants)

	for _, tenant := range tenants {
		// This method is CPU intensive, so it checks for cancellation between
		// tenants and while it copies records.
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}

		layout, ok := layouts[tenant]
		if !ok {
			return nil, nil, fmt.Errorf("no layout for tenant %s", tenant)
		}

		var sections []*dataobj.Section
		for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool { return logs.CheckSection(s) && s.Tenant == tenant }) {
			sections = append(sections, sec)
		}
		if len(sections) == 0 {
			return nil, nil, fmt.Errorf("no logs sections found for tenant: %v", tenant)
		}

		var streamSections []*dataobj.Section
		for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool {
			return streams.CheckSection(s) && s.Tenant == tenant
		}) {
			streamSections = append(streamSections, sec)
		}
		if len(streamSections) == 0 {
			return nil, nil, fmt.Errorf("no streams sections found for tenant: %v", tenant)
		} else if len(streamSections) > 1 {
			return nil, nil, fmt.Errorf("multiple streams sections found for tenant: %v", tenant)
		}

		streamsSection, err := streams.Open(ctx, streamSections[0])
		if err != nil {
			return nil, nil, fmt.Errorf("opening streams section for tenant %s: %w", tenant, err)
		}

		numStreams := streamsSection.NumRows()
		streamIter, remappedStreams, err := sortAndRemapStreams(streamsSectionIter(ctx, streamsSection), tenant, layout.SchemaLabels(), layout.ShardLayout(), numStreams)
		if err != nil {
			return nil, nil, fmt.Errorf("building stream ID remap for tenant %s: %w", tenant, err)
		}

		logsIter, err := mergeAndRemapLogsIter(ctx, sections, remappedStreams)
		if err != nil {
			return nil, nil, fmt.Errorf("creating sort iterator for tenant %s: %w", tenant, err)
		}

		// drainLogsIter drops duplicates itself, so lb must not drop any.
		// Otherwise the stream totals would count records that lb drops.
		lb := logs.NewBuilder(b.sortLogsMetrics, logs.BuilderOptions{
			PageSizeHint:     int(b.cfg.TargetPageSize),
			PageMaxRowCount:  b.cfg.MaxPageRows,
			BufferSize:       int(b.cfg.BufferSize),
			StripeMergeLimit: b.cfg.SectionStripeMergeLimit,
			AppendStrategy:   logs.AppendOrdered,
			SortOrder:        logs.SortSchemaASC,
			DropDuplicates:   false,
		})
		lb.SetTenant(tenant)

		totals, err := b.drainLogsIter(ctx, logsIter, lb, tenant, numStreams)
		if err != nil {
			return nil, nil, err
		}

		sb := streams.NewBuilder(b.sortStreamsMetrics, int(b.cfg.TargetPageSize), b.cfg.MaxPageRows, layout.ShardLayout())
		sb.SetLayout(layout)
		if err := b.buildStreamSection(ctx, tenant, streamIter, totals, sb); err != nil {
			return nil, nil, err
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return b.builder.Flush()
}

func (b *Builder) observeObject(ctx context.Context, obj *dataobj.Object) error {
	var errs []error

	errs = append(errs, b.metrics.dataobj.Observe(obj))

	for _, sec := range obj.Sections() {
		switch {
		case streams.CheckSection(sec):
			streamSection, err := streams.Open(context.Background(), sec)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			errs = append(errs, b.metrics.streams.Observe(ctx, streamSection))

		case logs.CheckSection(sec):
			logsSection, err := logs.Open(context.Background(), sec)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			errs = append(errs, b.metrics.logs.Observe(ctx, logsSection))
		}
	}

	return errors.Join(errs...)
}

// Reset discards pending data and resets the builder to an empty state.
func (b *Builder) Reset() {
	b.builder.Reset()

	// We currently discard all sub builders to be reclaimed by garbage
	// collection, instead of pooling them. If we pooled them, what would
	// happen is all builders would eventually reach the maximum size over
	// time, even if the tenant had a small amount of data, and we would OOM.
	// To be able to reuse the builders, we need to pool them by their size,
	// and ensure that we have different buckets of different sized builders
	// relative to our memory limit. Maybe we will consider this in future.
	clear(b.logs)
	clear(b.streams)
	clear(b.layouts)
	b.lastTarget = nil

	b.earliestRecordTime = time.Time{}
	b.currentSizeEstimate = 0
	b.state = builderStateEmpty
}

// drainLogsIter consumes iter, appending each record to lb and flushing
// completed sections to b.builder whenever the section size target is exceeded.
// It appends the final (possibly partial) section after the iterator is exhausted.
//
// If DropDuplicates is set, drainLogsIter skips duplicate records. iter must
// yield the records of each stream and timestamp together, as
// [logs.DuplicateTracker] requires.
//
// drainLogsIter returns the totals of the appended records. The stream IDs of
// the records must be in [1, numStreams].
func (b *Builder) drainLogsIter(ctx context.Context, iter result.Seq[logs.Record], lb *logs.Builder, tenant string, numStreams int) (streamTotals, error) {
	var (
		totals     = newStreamTotals(numStreams)
		duplicates logs.DuplicateTracker
	)
	for rec := range iter {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		val, err := rec.Value()
		if err != nil {
			return nil, err
		}
		if b.cfg.DropDuplicates && duplicates.IsDuplicate(val) {
			continue
		}
		totals.add(val)
		lb.Append(val)
		if lb.UncompressedSize() > int(b.cfg.TargetSectionSize) {
			if err := b.builder.Append(lb); err != nil {
				return nil, err
			}
			lb.Reset()
			lb.SetTenant(tenant)
		}
	}
	return totals, b.builder.Append(lb)
}

// buildStreamSection consumes an iterator, appending each stream to the provided
// streams.Builder, and adds it to the sections accumulator. sb must be empty.
//
// The rows and uncompressed size of each stream come from totals, not from
// iter, because the sort can drop duplicates that iter still counts.
func (b *Builder) buildStreamSection(ctx context.Context, tenant string, iter result.Seq[streams.Stream], totals streamTotals, sb *streams.Builder) error {
	sb.SetTenant(tenant)

	for res := range iter {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		stream, err := res.Value()
		if err != nil {
			return err
		}
		totals.apply(&stream)
		sb.AppendValue(stream)
	}
	return b.builder.Append(sb)
}

// streamTotals holds the rows and uncompressed size of the records of each
// stream. The stream ID is the index, so index 0 is unused.
type streamTotals []streamTotal

type streamTotal struct {
	rows int
	size int64
}

func newStreamTotals(numStreams int) streamTotals {
	return make(streamTotals, numStreams+1)
}

func (t streamTotals) add(rec logs.Record) {
	t[rec.StreamID].rows++
	t[rec.StreamID].size += rec.UncompressedSize()
}

// apply sets the rows and uncompressed size of s to the totals of its stream.
func (t streamTotals) apply(s *streams.Stream) {
	s.Rows = t[s.ID].rows
	s.UncompressedSize = t[s.ID].size
}

type mappedStream struct {
	stream  streams.Stream
	mapping rankedSortKey
}

// sortAndRemapStreams orders the streams by the globally stable stream order
// and reassigns stream IDs in that order. It returns an iterator over the
// remapped streams and a mapping from old stream IDs to new stream IDs.
//
// Stream IDs are originally assigned in the order streams are first recorded.
// After ordering, streams are clustered by [shard_bucket, sort-schema, hash],
// but their original IDs may no longer be monotonic in that order.
//
// Reassigning IDs in stream-order makes the persisted sort metadata
// [streamID ASC, timestamp DESC] match the physical row order and improves
// compression and pruning at query time.
//
// Log records must also be remapped to keep their stream references valid.
func sortAndRemapStreams(iter result.Seq[streams.Stream], tenant string, schemaLabels []string, shardLayout streams.ShardLayout, numStreams int) (result.Seq[streams.Stream], []rankedSortKey, error) {
	allStreams := make([]mappedStream, 0, numStreams)
	lookup := make([]rankedSortKey, numStreams+1)

	for res := range iter {
		stream, err := res.Value()
		if err != nil {
			return nil, nil, err
		}
		r, err := emptyRankedSortKey(stream.Labels, schemaLabels, shardLayout)
		if err != nil {
			return nil, nil, err
		}
		allStreams = append(allStreams, mappedStream{
			stream:  stream,
			mapping: r,
		})
	}

	slices.SortFunc(allStreams, func(a, b mappedStream) int {
		return streams.CompareSortKey(a.mapping.SortKey, b.mapping.SortKey)
	})

	for i := range allStreams {
		oldID := allStreams[i].stream.ID
		newID := int64(i + 1)

		if oldID <= 0 || oldID > int64(numStreams) {
			return nil, nil, fmt.Errorf("stream id %d out of range for tenant %s with %d streams", oldID, tenant, numStreams)
		}
		if prev := lookup[oldID]; prev.rank != 0 {
			return nil, nil, fmt.Errorf("duplicate stream id for tenant %s: old id %d maps to both %d and %d", tenant, oldID, prev.rank, newID)
		}

		allStreams[i].mapping.rank = newID
		lookup[oldID] = allStreams[i].mapping

		// Remap to the new stream ID.
		allStreams[i].stream.ID = newID
	}

	return result.Iter(func(yield func(streams.Stream) bool) error {
		for _, entry := range allStreams {
			if !yield(entry.stream) {
				return nil
			}
		}
		return nil
	}), lookup, nil
}

func streamsSectionIter(ctx context.Context, section *streams.Section) result.Seq[streams.Stream] {
	return result.Iter(func(yield func(streams.Stream) bool) error {
		for res := range streams.IterSection(ctx, section) {
			stream, err := res.Value()
			if err != nil {
				return err
			}
			if !yield(stream) {
				return nil
			}
		}
		return nil
	})
}

// ComputeSchemaKey builds a composite sort key from stream labels using FQN entries.
// Each FQN must be "label:<name>" — validation.SortSchema.Validate() enforces this.
func ComputeSchemaKey(ls labels.Labels, schemaLabels []string) (string, error) {
	if len(schemaLabels) == 0 {
		return "", nil
	}
	resolveLabel := func(fqn string) (string, error) {
		typ, name, ok := strings.Cut(fqn, ":")
		if !ok || typ != "label" {
			return "", fmt.Errorf("ComputeSortKey: unexpected FQN %q — only \"label:<name>\" is supported", fqn)
		}
		return ls.Get(name), nil
	}
	if len(schemaLabels) == 1 {
		return resolveLabel(schemaLabels[0])
	}
	values := make([]string, len(schemaLabels))
	for i, fqn := range schemaLabels {
		s, err := resolveLabel(fqn)
		if err != nil {
			return "", err
		}
		values[i] = s
	}
	return streams.EncodeSchemaKey(values), nil
}

// StreamSortPrefix returns the sort prefix of the stream with labels ls in
// objects of layout.
//
// It must return the prefix that the index calculator records for the stream
// in its stats rows. The compaction planner cuts tasks by the recorded
// prefixes, and workers filter streams by this one. If they differ, a task can
// miss the records of a stream.
func StreamSortPrefix(ls labels.Labels, layout streams.Layout) (streams.SortPrefix, error) {
	schemaKey, err := ComputeSchemaKey(ls, layout.SchemaLabels())
	if err != nil {
		return streams.SortPrefix{}, err
	}
	return layout.ShardLayout().NewSortKey(ls, schemaKey).Prefix(), nil
}
