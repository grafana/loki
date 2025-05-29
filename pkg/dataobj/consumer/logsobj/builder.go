// Package logsobj provides tooling for creating logs-oriented data objects.
package logsobj

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"sort"
	"time"

	"github.com/grafana/dskit/flagext"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// ErrBuilderFull is returned by [Builder.Append] when the buffer is
// full and needs to flush; call [Builder.Flush] to flush it.
var (
	ErrBuilderFull  = errors.New("builder full")
	ErrBuilderEmpty = errors.New("builder empty")
)

// BuilderConfig configures a [Builder].
type BuilderConfig struct {
	// TargetPageSize configures a target size for encoded pages within the data
	// object. TargetPageSize accounts for encoding, but not for compression.
	TargetPageSize flagext.Bytes `yaml:"target_page_size"`

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
}

// RegisterFlagsWithPrefix registers flags with the given prefix.
func (cfg *BuilderConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	_ = cfg.TargetPageSize.Set("2MB")
	_ = cfg.TargetObjectSize.Set("1GB")
	_ = cfg.BufferSize.Set("16MB")         // Page Size * 8
	_ = cfg.TargetSectionSize.Set("128MB") // Target Object Size / 8

	f.Var(&cfg.TargetPageSize, prefix+"target-page-size", "The size of the target page to use for the data object builder.")
	f.Var(&cfg.TargetObjectSize, prefix+"target-object-size", "The size of the target object to use for the data object builder.")
	f.Var(&cfg.TargetSectionSize, prefix+"target-section-size", "Configures a maximum size for sections, for sections that support it.")
	f.Var(&cfg.BufferSize, prefix+"buffer-size", "The size of the buffer to use for sorting logs.")
	f.IntVar(&cfg.SectionStripeMergeLimit, prefix+"section-stripe-merge-limit", 2, "The maximum number of stripes to merge into a section at once. Must be greater than 1.")
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

	return errors.Join(errs...)
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
	streams *streams.Builder
	logs    *logs.Builder

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
func NewBuilder(cfg BuilderConfig) (*Builder, error) {
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
		cfg:     cfg,
		metrics: metrics,

		labelCache: labelCache,

		builder: dataobj.NewBuilder(),
		streams: streams.NewBuilder(metrics.streams, int(cfg.TargetPageSize)),
		logs: logs.NewBuilder(metrics.logs, logs.BuilderOptions{
			PageSizeHint:     int(cfg.TargetPageSize),
			BufferSize:       int(cfg.BufferSize),
			StripeMergeLimit: cfg.SectionStripeMergeLimit,
		}),
	}, nil
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
func (b *Builder) Append(stream logproto.Stream) error {
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

	timer := prometheus.NewTimer(b.metrics.appendTime)
	defer timer.ObserveDuration()

	for _, entry := range stream.Entries {
		sz := int64(len(entry.Line))
		for _, md := range entry.StructuredMetadata {
			sz += int64(len(md.Value))
		}

		streamID := b.streams.Record(ls, entry.Timestamp, sz)

		b.logs.Append(logs.Record{
			StreamID:  streamID,
			Timestamp: entry.Timestamp,
			Metadata:  convertMetadata(entry.StructuredMetadata),
			Line:      []byte(entry.Line),
		})

		// If our logs section has gotten big enough, we want to flush it to the
		// encoder and start a new section.
		if b.logs.EstimatedSize() > int(b.cfg.TargetSectionSize) {
			if err := b.builder.Append(b.logs); err != nil {
				return err
			}
		}
	}

	b.currentSizeEstimate = b.estimatedSize()
	b.state = builderStateDirty
	return nil
}

func (b *Builder) parseLabels(labelString string) (labels.Labels, error) {
	labels, ok := b.labelCache.Get(labelString)
	if ok {
		return labels, nil
	}

	labels, err := syntax.ParseLabels(labelString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse labels: %w", err)
	}
	b.labelCache.Add(labelString, labels)
	return labels, nil
}

// labelsEstimate estimates the size of a set of labels in bytes.
func labelsEstimate(ls labels.Labels) int {
	var (
		keysSize   int
		valuesSize int
	)

	for _, l := range ls {
		keysSize += len(l.Name)
		valuesSize += len(l.Value)
	}

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
	l := make(labels.Labels, 0, len(md))
	for _, label := range md {
		l = append(l, labels.Label{Name: label.Name, Value: label.Value})
	}
	sort.Slice(l, func(i, j int) bool {
		if l[i].Name == l[j].Name {
			return cmp.Compare(l[i].Value, l[j].Value) < 0
		}
		return cmp.Compare(l[i].Name, l[j].Name) < 0
	})
	return l
}

func (b *Builder) estimatedSize() int {
	var size int
	size += b.streams.EstimatedSize()
	size += b.logs.EstimatedSize()
	size += b.builder.Bytes()
	b.metrics.sizeEstimate.Set(float64(size))
	return size
}

type FlushStats struct {
	MinTimestamp time.Time
	MaxTimestamp time.Time
}

// Flush flushes all buffered data to the buffer provided. Calling Flush can result
// in a no-op if there is no buffered data to flush.
//
// [Builder.Reset] is called after a successful Flush to discard any pending data and allow new data to be appended.
func (b *Builder) Flush(output *bytes.Buffer) (FlushStats, error) {
	if b.state == builderStateEmpty {
		return FlushStats{}, ErrBuilderEmpty
	}

	timer := prometheus.NewTimer(b.metrics.buildTime)
	defer timer.ObserveDuration()

	// Appending sections resets them, so we need to load the time range before
	// appending.
	minTime, maxTime := b.streams.TimeRange()

	// Flush sections one more time in case they have data.
	var flushErrors []error

	flushErrors = append(flushErrors, b.builder.Append(b.streams))
	flushErrors = append(flushErrors, b.builder.Append(b.logs))

	if err := errors.Join(flushErrors...); err != nil {
		b.metrics.flushFailures.Inc()
		return FlushStats{}, fmt.Errorf("building object: %w", err)
	}

	sz, err := b.builder.Flush(output)
	if err != nil {
		b.metrics.flushFailures.Inc()
		return FlushStats{}, fmt.Errorf("building object: %w", err)
	}

	b.metrics.builtSize.Observe(float64(sz))

	var (
		// We don't know if output was empty before calling Flush, so we only start
		// reading from where we know writing began.

		objReader = bytes.NewReader(output.Bytes()[output.Len()-int(sz):])
		objLength = sz
	)
	obj, err := dataobj.FromReaderAt(objReader, objLength)
	if err != nil {
		b.metrics.flushFailures.Inc()
		return FlushStats{}, fmt.Errorf("failed to create readable object: %w", err)
	}

	err = b.observeObject(context.Background(), obj)

	b.Reset()
	return FlushStats{MinTimestamp: minTime, MaxTimestamp: maxTime}, err
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
	b.logs.Reset()
	b.streams.Reset()

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
