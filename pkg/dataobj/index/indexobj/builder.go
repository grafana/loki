// Package indexobj provides tooling for creating index-oriented data objects.
package indexobj

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/grafana/dskit/flagext"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/scratch"
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
	_ = cfg.TargetPageSize.Set("128KB")
	_ = cfg.TargetObjectSize.Set("64MB")
	_ = cfg.BufferSize.Set("2MB")
	_ = cfg.TargetSectionSize.Set("16MB")

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

	builder       *dataobj.Builder // Inner builder for accumulating sections.
	streams       *streams.Builder
	pointers      *pointers.Builder
	indexPointers *indexpointers.Builder

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
		cfg:     cfg,
		metrics: metrics,

		labelCache: labelCache,

		builder:       dataobj.NewBuilder(scratchStore),
		streams:       streams.NewBuilder(metrics.streams, int(cfg.TargetPageSize)),
		pointers:      pointers.NewBuilder(metrics.pointers, int(cfg.TargetPageSize)),
		indexPointers: indexpointers.NewBuilder(metrics.indexPointers, int(cfg.TargetPageSize)),
	}, nil
}

func (b *Builder) GetEstimatedSize() int {
	return b.currentSizeEstimate
}

func (b *Builder) AppendIndexPointer(path string, startTs time.Time, endTs time.Time) error {
	b.metrics.appendsTotal.Inc()
	newEntrySize := len(path) + 1 + 1 // path, startTs, endTs

	if b.state != builderStateEmpty && b.currentSizeEstimate+newEntrySize > int(b.cfg.TargetObjectSize) {
		return ErrBuilderFull
	}

	timer := prometheus.NewTimer(b.metrics.appendTime)
	defer timer.ObserveDuration()

	b.indexPointers.Append(path, startTs, endTs)

	if b.indexPointers.EstimatedSize() > int(b.cfg.TargetSectionSize) {
		if err := b.builder.Append(b.indexPointers); err != nil {
			return err
		}
	}

	b.currentSizeEstimate = b.estimatedSize()
	b.state = builderStateDirty

	return nil
}

// AppendStream appends a stream to the object's stream section, returning the stream ID within this object.
func (b *Builder) AppendStream(stream streams.Stream) (int64, error) {
	b.metrics.appendsTotal.Inc()

	newEntrySize := labelsEstimate(stream.Labels) + 2

	if b.state != builderStateEmpty && b.currentSizeEstimate+newEntrySize > int(b.cfg.TargetObjectSize) {
		return 0, ErrBuilderFull
	}

	timer := prometheus.NewTimer(b.metrics.appendTime)
	defer timer.ObserveDuration()

	// Record the stream in the stream section.
	// Once to capture the min timestamp and uncompressed size, again to record the max timestamp.
	streamID := b.streams.Record(stream.Labels, stream.MinTimestamp, stream.UncompressedSize)
	_ = b.streams.Record(stream.Labels, stream.MaxTimestamp, 0)

	// If our logs section has gotten big enough, we want to flush it to the
	// encoder and start a new section.
	if b.pointers.EstimatedSize() > int(b.cfg.TargetSectionSize) {
		if err := b.builder.Append(b.pointers); err != nil {
			b.metrics.appendFailures.Inc()
			return 0, err
		}
	}

	b.currentSizeEstimate = b.estimatedSize()
	b.state = builderStateDirty

	return streamID, nil
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

// Append buffers a stream to be written to a data object. Append returns an
// error if the stream labels cannot be parsed or [ErrBuilderFull] if the
// builder is full.
//
// Once a Builder is full, call [Builder.Flush] to flush the buffered data,
// then call Append again with the same entry.
func (b *Builder) ObserveLogLine(path string, section int64, streamIDInObject int64, streamIDInIndex int64, ts time.Time, uncompressedSize int64) error {
	// Check whether the buffer is full before a stream can be appended; this is
	// tends to overestimate, but we may still go over our target size.
	//
	// Since this check only happens after the first call to Append,
	// b.currentSizeEstimate will always be updated to reflect the size following
	// the previous append.

	newEntrySize := 4 // ints and times compress well so we just need to make an estimate.

	if b.state != builderStateEmpty && b.currentSizeEstimate+newEntrySize > int(b.cfg.TargetObjectSize) {
		return ErrBuilderFull
	}

	timer := prometheus.NewTimer(b.metrics.appendTime)
	defer timer.ObserveDuration()

	b.pointers.ObserveStream(path, section, streamIDInObject, streamIDInIndex, ts, uncompressedSize)

	// If our logs section has gotten big enough, we want to flush it to the
	// encoder and start a new section.
	if b.pointers.EstimatedSize() > int(b.cfg.TargetSectionSize) {
		if err := b.builder.Append(b.pointers); err != nil {
			return err
		}
	}

	b.currentSizeEstimate = b.estimatedSize()
	b.state = builderStateDirty
	return nil
}

// Append buffers a stream to be written to a data object. Append returns an
// error if the stream labels cannot be parsed or [ErrBuilderFull] if the
// builder is full.
//
// Once a Builder is full, call [Builder.Flush] to flush the buffered data,
// then call Append again with the same entry.
func (b *Builder) AppendColumnIndex(path string, section int64, columnName string, columnIndex int64, valuesBloom []byte) error {
	// Check whether the buffer is full before a stream can be appended; this is
	// tends to overestimate, but we may still go over our target size.
	//
	// Since this check only happens after the first call to Append,
	// b.currentSizeEstimate will always be updated to reflect the size following
	// the previous append.

	newEntrySize := len(columnName) + 1 + 1 + len(valuesBloom) + 1

	if b.state != builderStateEmpty && b.currentSizeEstimate+newEntrySize > int(b.cfg.TargetObjectSize) {
		return ErrBuilderFull
	}

	timer := prometheus.NewTimer(b.metrics.appendTime)
	defer timer.ObserveDuration()

	b.pointers.RecordColumnIndex(path, section, columnName, columnIndex, valuesBloom)

	// If our logs section has gotten big enough, we want to flush it to the
	// encoder and start a new section.
	if b.pointers.EstimatedSize() > int(b.cfg.TargetSectionSize) {
		if err := b.builder.Append(b.pointers); err != nil {
			return err
		}
	}

	b.currentSizeEstimate = b.estimatedSize()
	b.state = builderStateDirty
	return nil
}

func (b *Builder) estimatedSize() int {
	var size int
	size += b.streams.EstimatedSize()
	size += b.pointers.EstimatedSize()
	size += b.indexPointers.EstimatedSize()
	size += b.builder.Bytes()
	b.metrics.sizeEstimate.Set(float64(size))
	return size
}

// TimeRange returns the time range of the data in the builder.
func (b *Builder) TimeRange() (minTime, maxTime time.Time) {
	return b.streams.TimeRange()
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

	b.metrics.flushTotal.Inc()
	timer := prometheus.NewTimer(b.metrics.buildTime)
	defer timer.ObserveDuration()

	// Flush sections one more time in case they have data.
	var flushErrors []error

	flushErrors = append(flushErrors, b.builder.Append(b.streams))
	flushErrors = append(flushErrors, b.builder.Append(b.pointers))
	flushErrors = append(flushErrors, b.builder.Append(b.indexPointers))

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

func (b *Builder) observeObject(ctx context.Context, obj *dataobj.Object) error {
	var errs []error

	errs = append(errs, b.metrics.dataobj.Observe(obj))

	for _, sec := range obj.Sections() {
		switch {
		case indexpointers.CheckSection(sec):
			indexPointerSection, err := indexpointers.Open(ctx, sec)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			errs = append(errs, b.metrics.indexPointers.Observe(ctx, indexPointerSection))
		case pointers.CheckSection(sec):
			pointerSection, err := pointers.Open(context.Background(), sec)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			errs = append(errs, b.metrics.pointers.Observe(ctx, pointerSection))
		case streams.CheckSection(sec):
			streamSection, err := streams.Open(context.Background(), sec)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			errs = append(errs, b.metrics.streams.Observe(ctx, streamSection))
		}
	}

	return errors.Join(errs...)
}

// Reset discards pending data and resets the builder to an empty state.
func (b *Builder) Reset() {
	b.builder.Reset()
	b.streams.Reset()
	b.pointers.Reset()
	b.indexPointers.Reset()

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
