package indexobj

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// A Builder constructs a logs-oriented data object from a set of incoming
// log data. Log data is appended by calling [LogBuilder.Append]. A complete
// data object is constructed by by calling [LogBuilder.Flush].
//
// Methods on Builder are not goroutine-safe; callers are responsible for
// synchronization.
type MultiTenantBuilder struct {
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

// NewBuilder creates a new [Builder] which stores log-oriented data objects.
//
// NewBuilder returns an error if the provided config is invalid.
func NewMultiTenantBuilder(cfg BuilderConfig) (*MultiTenantBuilder, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	labelCache, err := lru.New[string, labels.Labels](5000)
	if err != nil {
		return nil, fmt.Errorf("failed to create LRU cache: %w", err)
	}

	metrics := newBuilderMetrics()
	metrics.ObserveConfig(cfg)

	return &MultiTenantBuilder{
		cfg:     cfg,
		metrics: metrics,

		labelCache: labelCache,

		builder:       dataobj.NewBuilder(),
		streams:       streams.NewBuilder("", metrics.streams, int(cfg.TargetPageSize)),
		pointers:      pointers.NewBuilder(metrics.pointers, int(cfg.TargetPageSize)),
		indexPointers: indexpointers.NewBuilder(metrics.indexPointers, int(cfg.TargetPageSize)),
	}, nil
}

func (b *MultiTenantBuilder) GetEstimatedSize() int {
	return b.currentSizeEstimate
}

func (b *MultiTenantBuilder) AppendIndexPointer(path string, startTs time.Time, endTs time.Time) error {
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
func (b *MultiTenantBuilder) AppendStream(stream streams.Stream) (int64, error) {
	b.metrics.appendsTotal.Inc()

	newEntrySize := labelsEstimate(stream.Labels) + 2

	if b.state != builderStateEmpty && b.currentSizeEstimate+newEntrySize > int(b.cfg.TargetObjectSize) {
		return 0, ErrBuilderFull
	}

	timer := prometheus.NewTimer(b.metrics.appendTime)
	defer timer.ObserveDuration()

	// ts, ok := b.streams[]

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

// RecordStreamRef records a reference to a stream from another object, as the stream IDs will be different between objects.
func (b *MultiTenantBuilder) RecordStreamRef(path string, streamIDInObject int64, streamID int64) {
	b.pointers.RecordStreamRef(path, streamIDInObject, streamID)
}

// Append buffers a stream to be written to a data object. Append returns an
// error if the stream labels cannot be parsed or [ErrBuilderFull] if the
// builder is full.
//
// Once a Builder is full, call [Builder.Flush] to flush the buffered data,
// then call Append again with the same entry.
func (b *MultiTenantBuilder) ObserveLogLine(path string, section int64, streamIDInObject int64, ts time.Time, uncompressedSize int64) error {
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

	b.pointers.ObserveStream(path, section, streamIDInObject, ts, uncompressedSize)

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
func (b *MultiTenantBuilder) AppendColumnIndex(path string, section int64, columnName string, columnIndex int64, valuesBloom []byte) error {
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

func (b *MultiTenantBuilder) estimatedSize() int {
	var size int
	size += b.streams.EstimatedSize()
	size += b.pointers.EstimatedSize()
	size += b.indexPointers.EstimatedSize()
	size += b.builder.Bytes()
	b.metrics.sizeEstimate.Set(float64(size))
	return size
}

// Flush flushes all buffered data to the buffer provided. Calling Flush can result
// in a no-op if there is no buffered data to flush.
//
// [Builder.Reset] is called after a successful Flush to discard any pending data and allow new data to be appended.
func (b *MultiTenantBuilder) Flush(output *bytes.Buffer) (FlushStats, error) {
	if b.state == builderStateEmpty {
		return FlushStats{}, ErrBuilderEmpty
	}

	b.metrics.flushTotal.Inc()
	timer := prometheus.NewTimer(b.metrics.buildTime)
	defer timer.ObserveDuration()

	// Appending sections resets them, so we need to load the time range before
	// appending.
	minTime, maxTime := b.streams.TimeRange()

	// Flush sections one more time in case they have data.
	var flushErrors []error

	flushErrors = append(flushErrors, b.builder.Append(b.streams))
	flushErrors = append(flushErrors, b.builder.Append(b.pointers))
	flushErrors = append(flushErrors, b.builder.Append(b.indexPointers))

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

func (b *MultiTenantBuilder) observeObject(ctx context.Context, obj *dataobj.Object) error {
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
func (b *MultiTenantBuilder) Reset() {
	b.builder.Reset()
	b.streams.Reset()
	b.pointers.Reset()
	b.indexPointers.Reset()

	//b.metrics.sizeEstimate.Set(0)
	b.currentSizeEstimate = 0
	b.state = builderStateEmpty
}

// RegisterMetrics registers metrics about builder to report to reg. All
// metrics will have a tenant label set to the tenant ID of the Builder.
//
// If multiple Builders for the same tenant are running in the same process,
// reg must contain additional labels to differentiate between them.
func (b *MultiTenantBuilder) RegisterMetrics(reg prometheus.Registerer) error {
	return b.metrics.Register(reg)
}

// UnregisterMetrics unregisters metrics about builder from reg.
func (b *MultiTenantBuilder) UnregisterMetrics(reg prometheus.Registerer) {
	b.metrics.Unregister(reg)
}
