// Package logsobj provides tooling for creating logs-oriented data objects.
package logsobj

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/facette/natsort"
	"github.com/grafana/dskit/flagext"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/scratch"
)

// ErrBuilderFull is returned by [Builder.Append] when the buffer is
// full and needs to flush; call [Builder.Flush] to flush it.
var (
	ErrBuilderFull  = errors.New("builder full")
	ErrBuilderEmpty = errors.New("builder empty")
)

const (
	// Constants for the sort order configuration
	sortStreamASC     = "stream-asc"
	sortTimestampDESC = "timestamp-desc"
)

// BuilderConfig configures a [Builder].
type BuilderConfig struct {
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

	// DataobjSortOrder defines the order in which the rows of the logs sections are sorted.
	// They can either be sorted by [streamID ASC, timestamp DESC] or [timestamp DESC, streamID ASC].
	DataobjSortOrder string `yaml:"dataobj_sort_order" doc:"hidden"`
}

// RegisterFlagsWithPrefix registers flags with the given prefix.
func (cfg *BuilderConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	_ = cfg.TargetPageSize.Set("2MB")
	_ = cfg.TargetObjectSize.Set("1GB")
	_ = cfg.BufferSize.Set("16MB")
	_ = cfg.TargetSectionSize.Set("128MB")

	f.Var(&cfg.TargetPageSize, prefix+"target-page-size", "The target maximum amount of uncompressed data to hold in data pages (for columnar sections). Uncompressed size is used for consistent I/O and planning.")
	f.IntVar(&cfg.MaxPageRows, prefix+"max-page-rows", 0, "The maximum row count for pages to use for the data object builder. A value of 0 means no limit.")
	f.Var(&cfg.TargetObjectSize, prefix+"target-builder-memory-limit", "The target maximum size of the encoded object and all of its encoded sections (after compression), to limit memory usage of a builder.")
	f.Var(&cfg.TargetSectionSize, prefix+"target-section-size", "The target maximum amount of uncompressed data to hold in sections, for sections that support being limited by size. Uncompressed size is used for consistent I/O and planning.")
	f.Var(&cfg.BufferSize, prefix+"buffer-size", "The size of logs to buffer in memory before adding into columnar builders, used to reduce CPU load of sorting.")
	f.IntVar(&cfg.SectionStripeMergeLimit, prefix+"section-stripe-merge-limit", 2, "The maximum number of log section stripes to merge into a section at once. Must be greater than 1.")
	f.StringVar(&cfg.DataobjSortOrder, prefix+"dataobj-sort-order", sortStreamASC, "The desired sort order of the logs section. Can either be `stream-asc` (order by streamID ascending and timestamp descending) or `timestamp-desc` (order by timestamp descending and streamID ascending).")
}

// Validate validates the BuilderConfig.
func (cfg *BuilderConfig) Validate() error {
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

	if cfg.DataobjSortOrder == "" {
		cfg.DataobjSortOrder = sortStreamASC // default to [streamID ASC, timestamp DESC] sorting
	}
	if !(cfg.DataobjSortOrder == sortStreamASC || cfg.DataobjSortOrder == sortTimestampDESC) {
		errs = append(errs, fmt.Errorf("invalid dataobj sort order. must be one of `stream-asc` or `timestamp-desc`, got: %s", cfg.DataobjSortOrder))
	}

	return errors.Join(errs...)
}

var sortOrderMapping = map[string]logs.SortOrder{
	sortStreamASC:     logs.SortStreamASC,
	sortTimestampDESC: logs.SortTimestampDESC,
}

func parseSortOrder(s string) logs.SortOrder {
	val, _ := sortOrderMapping[s]
	return val
}

// A Builder constructs a logs-oriented data object from a set of incoming
// log data. Log data is appended by calling [LogBuilder.Append]. A complete
// data object is constructed by by calling [LogBuilder.Flush].
//
// Methods on Builder are not goroutine-safe; callers are responsible for
// synchronization.
type Builder struct {
	cfg     BuilderConfig
	metrics *builderMetrics

	labelCache *lru.Cache[string, labels.Labels]

	currentSizeEstimate int

	builder *dataobj.Builder // Inner builder for accumulating sections.
	streams map[string]*streams.Builder
	logs    map[string]*logs.Builder

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
func NewBuilder(cfg BuilderConfig, scratchStore scratch.Store) (*Builder, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	labelCache, err := lru.New[string, labels.Labels](5000)
	if err != nil {
		return nil, fmt.Errorf("failed to create LRU cache: %w", err)
	}

	metrics := newBuilderMetrics()
	metrics.ObserveConfig(cfg)

	return &Builder{
		cfg:        cfg,
		metrics:    metrics,
		labelCache: labelCache,
		builder:    dataobj.NewBuilder(scratchStore),
		streams:    make(map[string]*streams.Builder),
		logs:       make(map[string]*logs.Builder),
	}, nil
}

// initBuilder initializes the builders for the tenant.
func (b *Builder) initBuilder(tenant string) {
	if _, ok := b.streams[tenant]; !ok {
		sb := streams.NewBuilder(b.metrics.streams, int(b.cfg.TargetPageSize), b.cfg.MaxPageRows)
		sb.SetTenant(tenant)
		b.streams[tenant] = sb
	}
	if _, ok := b.logs[tenant]; !ok {
		lb := logs.NewBuilder(b.metrics.logs, logs.BuilderOptions{
			PageSizeHint:     int(b.cfg.TargetPageSize),
			PageMaxRowCount:  b.cfg.MaxPageRows,
			BufferSize:       int(b.cfg.BufferSize),
			StripeMergeLimit: b.cfg.SectionStripeMergeLimit,
			SortOrder:        parseSortOrder(b.cfg.DataobjSortOrder),
		})
		lb.SetTenant(tenant)
		b.logs[tenant] = lb
	}
}

func (b *Builder) GetEstimatedSize() int {
	return b.currentSizeEstimate
}

// Append buffers a stream to be written to a data object. Append returns an
// error if the stream labels cannot be parsed or [ErrBuilderFull] if the
// builder is full.
//
// Once a Builder is full, call [Builder.Flush] to flush the buffered data,
// then call Append again with the same entry.
func (b *Builder) Append(tenant string, stream logproto.Stream) error {
	ls, err := b.parseLabels(stream.Labels)
	if err != nil {
		return err
	}

	// Check whether the buffer is full before a stream can be appended; this is
	// tends to overestimate, but we may still go over our target size.
	//
	// Since this check only happens after the first call to Append,
	// b.currentSizeEstimate will always be updated to reflect the size following
	// the previous append.
	if b.state != builderStateEmpty && b.currentSizeEstimate+labelsEstimate(ls)+streamSizeEstimate(stream) > int(b.cfg.TargetObjectSize) {
		return ErrBuilderFull
	}

	b.initBuilder(tenant)
	sb, lb := b.streams[tenant], b.logs[tenant]

	timer := prometheus.NewTimer(b.metrics.appendTime)
	defer timer.ObserveDuration()

	for _, entry := range stream.Entries {
		sz := int64(len(entry.Line))
		for _, md := range entry.StructuredMetadata {
			sz += int64(len(md.Value))
		}

		streamID := sb.Record(ls, entry.Timestamp, sz)

		lb.Append(logs.Record{
			StreamID:  streamID,
			Timestamp: entry.Timestamp,
			Metadata:  convertMetadata(entry.StructuredMetadata),
			Line:      []byte(entry.Line),
		})

		// If our logs section has gotten big enough, we want to flush it to the
		// encoder and start a new section.
		if lb.UncompressedSize() > int(b.cfg.TargetSectionSize) {
			if err := b.builder.Append(lb); err != nil {
				return err
			}
			// We need to set the tenant again after flushing because the builder is reset.
			lb.SetTenant(tenant)
		}
	}

	b.currentSizeEstimate = b.estimatedSize()
	b.state = builderStateDirty
	return nil
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

// labelsEstimate estimates the size of a set of labels in bytes.
func labelsEstimate(ls labels.Labels) int {
	var (
		keysSize   int
		valuesSize int
	)

	ls.Range(func(l labels.Label) {
		keysSize += len(l.Name)
		valuesSize += len(l.Value)
	})

	// Keys are stored as columns directly, while values get compressed. We'll
	// underestimate a 2x compression ratio.
	return keysSize + valuesSize/2
}

// streamSizeEstimate estimates the size of a stream in bytes.
func streamSizeEstimate(stream logproto.Stream) int {
	var size int
	for _, entry := range stream.Entries {
		// We only check the size of the line and metadata. Timestamps and IDs
		// encode so well that they're unlikely to make a singificant impact on our
		// size estimate.
		size += len(entry.Line) / 2 // Line with 2x compression ratio
		for _, md := range entry.StructuredMetadata {
			size += len(md.Name) + len(md.Value)/2
		}
	}
	return size
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
	b.metrics.sizeEstimate.Set(float64(size))
	return size
}

// TimeRanges returns the time ranges for each tenant.
func (b *Builder) TimeRanges() []multitenancy.TimeRange {
	var timeRanges []multitenancy.TimeRange
	for _, sb := range b.streams {
		minTime, maxTime := sb.TimeRange()
		timeRanges = append(timeRanges, multitenancy.TimeRange{
			Tenant:  sb.Tenant(),
			MinTime: minTime,
			MaxTime: maxTime,
		})
	}
	return timeRanges
}

// Flush flushes all buffered data to the buffer provided. Calling Flush can result
// in a no-op if there is no buffered data to flush.
//
// [Builder.Reset] is called after a successful Flush to discard any pending
// data and allow new data to be appended.
func (b *Builder) Flush() (*dataobj.Object, io.Closer, error) {
	if b.state == builderStateEmpty {
		return nil, nil, ErrBuilderEmpty
	}

	timer := prometheus.NewTimer(b.metrics.buildTime)
	defer timer.ObserveDuration()

	// Flush sections one more time in case they have data.
	var flushErrors []error

	for _, sb := range b.streams {
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

	err = b.observeObject(context.Background(), obj)

	b.Reset()
	return obj, closer, err
}

// CopyAndSort takes an existing [dataobj.Object] and rewrites the logs sections so the logs are sorted object-wide.
// The order of the sections is deterministic. For each tenant, first come the streams sections in the order of the old object
// and second come the new, rewritten logs sections. Tenants are sorted in natural order.
func (b *Builder) CopyAndSort(obj *dataobj.Object) (*dataobj.Object, io.Closer, error) {
	dur := prometheus.NewTimer(b.metrics.sortDurationSeconds)
	defer dur.ObserveDuration()

	ctx := context.Background()
	sort := parseSortOrder(b.cfg.DataobjSortOrder)

	sb := streams.NewBuilder(b.metrics.streams, int(b.cfg.TargetPageSize), b.cfg.MaxPageRows)
	lb := logs.NewBuilder(b.metrics.logs, logs.BuilderOptions{
		PageSizeHint:     int(b.cfg.TargetPageSize),
		PageMaxRowCount:  b.cfg.MaxPageRows,
		BufferSize:       int(b.cfg.BufferSize),
		StripeMergeLimit: b.cfg.SectionStripeMergeLimit,
		AppendStrategy:   logs.AppendOrdered,
		SortOrder:        sort,
	})

	// Sort the set of tenants so the new object has a deterministic order of sections.
	tenants := obj.Tenants()
	natsort.Sort(tenants)

	for _, tenant := range tenants {
		for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool { return streams.CheckSection(s) && s.Tenant == tenant }) {
			sb.Reset()
			sb.SetTenant(sec.Tenant)
			// Copy section into new builder. This is *very* inefficient at the moment!
			// TODO(chaudum): Create implementation of SectionBuilder interface that can copy entire ranges from a SectionReader.
			section, err := streams.Open(ctx, sec)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to open streams section: %w", err)
			}
			iter := streams.IterSection(ctx, section)
			for res := range iter {
				val, err := res.Value()
				if err != nil {
					return nil, nil, err
				}
				sb.AppendValue(val)
			}
			if err := b.builder.Append(sb); err != nil {
				return nil, nil, err
			}
		}

		var sections []*dataobj.Section
		for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool { return logs.CheckSection(s) && s.Tenant == tenant }) {
			sections = append(sections, sec)
		}

		if len(sections) == 0 {
			return nil, nil, fmt.Errorf("no logs sections found for tenant: %v", tenant)
		}

		// TODO(chaudum): Handle special case len(sections) == 1

		lb.Reset()
		lb.SetTenant(tenant)

		iter, err := sortMergeIterator(ctx, sections, sort)
		if err != nil {
			return nil, nil, fmt.Errorf("creating sort iterator: %w", err)
		}

		for rec := range iter {
			val, err := rec.Value()
			if err != nil {
				return nil, nil, err
			}

			lb.Append(val)

			// If our logs section has gotten big enough, we want to flush it to the encoder and start a new section.
			if lb.UncompressedSize() > int(b.cfg.TargetSectionSize) {
				if err := b.builder.Append(lb); err != nil {
					return nil, nil, err
				}
				lb.Reset()
				lb.SetTenant(tenant)
			}
		}

		// Append the final section with the remaining logs
		if err := b.builder.Append(lb); err != nil {
			return nil, nil, err
		}
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

	b.metrics.sizeEstimate.Set(0)
	b.currentSizeEstimate = 0
	b.state = builderStateEmpty
}

// RegisterMetrics registers metrics about builder to report to reg. All
// metrics will have a tenant label set to the tenant ID of the Builder.
//
// If multiple Builders for the same tenant are running in the same process,
// reg must contain additional labels to differentiate between them.
func (b *Builder) RegisterMetrics(reg prometheus.Registerer) error {
	return b.metrics.Register(reg)
}

// UnregisterMetrics unregisters metrics about builder from reg.
func (b *Builder) UnregisterMetrics(reg prometheus.Registerer) {
	b.metrics.Unregister(reg)
}
