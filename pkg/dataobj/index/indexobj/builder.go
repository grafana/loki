// Package indexobj provides tooling for creating index-oriented data objects.
package indexobj

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
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

// A Builder constructs a logs-oriented data object from a set of incoming
// log data. Log data is appended by calling [LogBuilder.Append]. A complete
// data object is constructed by by calling [LogBuilder.Flush].
//
// Methods on Builder are not goroutine-safe; callers are responsible for
// synchronization.
type Builder struct {
	cfg     logsobj.BuilderBaseConfig
	metrics *builderMetrics

	labelCache *lru.Cache[string, labels.Labels]

	currentSizeEstimate int
	builderFull         bool

	builder       *dataobj.Builder                  // Inner builder for accumulating sections.
	streams       map[string]*streams.Builder       // The key is the TenantID.
	pointers      map[string]*pointers.Builder      // The key is the TenantID.
	indexPointers map[string]*indexpointers.Builder // The key is the TenantID.

	// Optimization to avoid recalculating the size by asking all tenants for their estimated size.
	unflushedSizeEstimate int

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
func NewBuilder(cfg logsobj.BuilderBaseConfig, scratchStore scratch.Store) (*Builder, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	labelCache, err := lru.New[string, labels.Labels](5000)
	if err != nil {
		return nil, fmt.Errorf("failed to create LRU cache: %w", err)
	}

	// TODO: Add metrics for number of tenants in each object
	metrics := newBuilderMetrics()
	metrics.ObserveConfig(cfg)

	return &Builder{
		cfg:     cfg,
		metrics: metrics,

		labelCache: labelCache,

		builder:       dataobj.NewBuilder(scratchStore),
		streams:       make(map[string]*streams.Builder),
		pointers:      make(map[string]*pointers.Builder),
		indexPointers: make(map[string]*indexpointers.Builder),
	}, nil
}

func (b *Builder) GetEstimatedSize() int {
	return b.currentSizeEstimate
}

func (b *Builder) IsFull() bool {
	return b.builderFull
}

func (b *Builder) getIndexPointerBuilderForTenant(tenantID string) *indexpointers.Builder {
	tenantIndexPointers, ok := b.indexPointers[tenantID]
	if ok {
		return tenantIndexPointers
	}

	tenantIndexPointers = indexpointers.NewBuilder(b.metrics.indexPointers, int(b.cfg.TargetPageSize), b.cfg.MaxPageRows)
	tenantIndexPointers.SetTenant(tenantID)

	b.indexPointers[tenantID] = tenantIndexPointers

	return tenantIndexPointers
}

func (b *Builder) AppendIndexPointer(tenantID string, path string, startTs time.Time, endTs time.Time) error {
	b.metrics.appendsTotal.Inc()
	newEntrySize := len(path) + 1 + 1 // path, startTs, endTs

	if b.state != builderStateEmpty && b.currentSizeEstimate+newEntrySize > int(b.cfg.TargetObjectSize) {
		b.builderFull = true
	}

	timer := prometheus.NewTimer(b.metrics.appendTime)
	defer timer.ObserveDuration()

	tenantIndexPointers := b.getIndexPointerBuilderForTenant(tenantID)
	preAppendSizeEstimate := tenantIndexPointers.EstimatedSize()

	tenantIndexPointers.Append(path, startTs, endTs)

	postAppendSizeEstimate := tenantIndexPointers.EstimatedSize()
	b.unflushedSizeEstimate += postAppendSizeEstimate - preAppendSizeEstimate

	if postAppendSizeEstimate > int(b.cfg.TargetSectionSize) {
		if err := b.builder.Append(tenantIndexPointers); err != nil {
			return err
		}
	}

	b.currentSizeEstimate = b.estimatedSize()
	b.state = builderStateDirty

	return nil
}

func (b *Builder) getStreamsBuilderForTenant(tenantID string) *streams.Builder {
	tenantStreams, ok := b.streams[tenantID]
	if ok {
		return tenantStreams
	}

	tenantStreams = streams.NewBuilder(b.metrics.streams, int(b.cfg.TargetPageSize), b.cfg.MaxPageRows)
	tenantStreams.SetTenant(tenantID)

	b.streams[tenantID] = tenantStreams

	return tenantStreams
}

// AppendStream appends a stream to the object's stream section, returning the stream ID within this object.
func (b *Builder) AppendStream(tenantID string, stream streams.Stream) (int64, error) {
	b.metrics.appendsTotal.Inc()

	newEntrySize := labelsEstimate(stream.Labels) + 2

	if b.state != builderStateEmpty && b.currentSizeEstimate+newEntrySize > int(b.cfg.TargetObjectSize) {
		b.builderFull = true
	}

	timer := prometheus.NewTimer(b.metrics.appendTime)
	defer timer.ObserveDuration()

	tenantStreams := b.getStreamsBuilderForTenant(tenantID)
	preAppendSizeEstimate := tenantStreams.EstimatedSize()

	// Record the stream in the stream section.
	// Once to capture the min timestamp and uncompressed size, again to record the max timestamp.
	streamID := tenantStreams.Record(stream.Labels, stream.MinTimestamp, stream.UncompressedSize)
	_ = tenantStreams.Record(stream.Labels, stream.MaxTimestamp, 0)

	postAppendSizeEstimate := tenantStreams.EstimatedSize()
	b.unflushedSizeEstimate += postAppendSizeEstimate - preAppendSizeEstimate

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

func (b *Builder) getPointersBuilderForTenant(tenantID string) *pointers.Builder {
	tenantPointers, ok := b.pointers[tenantID]
	if ok {
		return tenantPointers
	}

	tenantPointers = pointers.NewBuilder(b.metrics.pointers, int(b.cfg.TargetPageSize), b.cfg.MaxPageRows)
	tenantPointers.SetTenant(tenantID)

	b.pointers[tenantID] = tenantPointers

	return tenantPointers
}

// Append buffers a stream to be written to a data object. Append returns an
// error if the stream labels cannot be parsed or [ErrBuilderFull] if the
// builder is full.
//
// Once a Builder is full, call [Builder.Flush] to flush the buffered data,
// then call Append again with the same entry.
func (b *Builder) ObserveLogLine(tenantID string, path string, section int64, streamIDInObject int64, streamIDInIndex int64, ts time.Time, uncompressedSize int64) error {
	// Check whether the buffer is full before a stream can be appended; this is
	// tends to overestimate, but we may still go over our target size.
	//
	// Since this check only happens after the first call to Append,
	// b.currentSizeEstimate will always be updated to reflect the size following
	// the previous append.

	newEntrySize := 4 // ints and times compress well so we just need to make an estimate.

	if b.state != builderStateEmpty && b.currentSizeEstimate+newEntrySize > int(b.cfg.TargetObjectSize) {
		b.builderFull = true
	}

	timer := prometheus.NewTimer(b.metrics.appendTime)
	defer timer.ObserveDuration()

	tenantPointers := b.getPointersBuilderForTenant(tenantID)
	preAppendSizeEstimate := tenantPointers.EstimatedSize()

	tenantPointers.ObserveStream(path, section, streamIDInObject, streamIDInIndex, ts, uncompressedSize)

	postAppendSizeEstimate := tenantPointers.EstimatedSize()
	b.unflushedSizeEstimate += postAppendSizeEstimate - preAppendSizeEstimate

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
func (b *Builder) AppendColumnIndex(tenantID string, path string, section int64, columnName string, columnIndex int64, valuesBloom []byte) error {
	// Check whether the buffer is full before a stream can be appended; this is
	// tends to overestimate, but we may still go over our target size.
	//
	// Since this check only happens after the first call to Append,
	// b.currentSizeEstimate will always be updated to reflect the size following
	// the previous append.

	newEntrySize := len(columnName) + 1 + 1 + len(valuesBloom) + 1

	if b.state != builderStateEmpty && b.currentSizeEstimate+newEntrySize > int(b.cfg.TargetObjectSize) {
		b.builderFull = true
	}

	timer := prometheus.NewTimer(b.metrics.appendTime)
	defer timer.ObserveDuration()

	tenantPointers := b.getPointersBuilderForTenant(tenantID)
	preAppendSizeEstimate := tenantPointers.EstimatedSize()

	tenantPointers.RecordColumnIndex(path, section, columnName, columnIndex, valuesBloom)

	postAppendSizeEstimate := tenantPointers.EstimatedSize()
	b.unflushedSizeEstimate += postAppendSizeEstimate - preAppendSizeEstimate

	// If our logs section has gotten big enough, we want to flush it to the
	// encoder and start a new section.
	if postAppendSizeEstimate > int(b.cfg.TargetSectionSize) {
		if err := b.builder.Append(tenantPointers); err != nil {
			return err
		}
	}

	b.currentSizeEstimate = b.estimatedSize()
	b.state = builderStateDirty
	return nil
}

func (b *Builder) estimatedSize() int {
	var size int
	size += b.unflushedSizeEstimate
	size += b.builder.Bytes()
	b.metrics.sizeEstimate.Set(float64(size))
	return size
}

// TimeRanges returns the time range of the data in the builder, by tenant.
func (b *Builder) TimeRanges() []multitenancy.TimeRange {
	timeRanges := make([]multitenancy.TimeRange, 0, len(b.streams))
	for tenantID, tenantStreams := range b.streams {
		minTime, maxTime := tenantStreams.TimeRange()
		timeRanges = append(timeRanges, multitenancy.TimeRange{
			Tenant:  tenantID,
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

	b.metrics.flushTotal.Inc()
	timer := prometheus.NewTimer(b.metrics.buildTime)
	defer timer.ObserveDuration()

	var flushErrors []error

	for _, tenantStreams := range b.streams {
		if tenantStreams.EstimatedSize() > 0 {
			flushErrors = append(flushErrors, b.builder.Append(tenantStreams))
		}
	}
	for _, tenantPointers := range b.pointers {
		if tenantPointers.EstimatedSize() > 0 {
			flushErrors = append(flushErrors, b.builder.Append(tenantPointers))
		}
	}
	for _, tenantIndexPointers := range b.indexPointers {
		if tenantIndexPointers.EstimatedSize() > 0 {
			flushErrors = append(flushErrors, b.builder.Append(tenantIndexPointers))
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
	clear(b.streams)
	clear(b.pointers)
	clear(b.indexPointers)

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
func (b *Builder) RegisterMetrics(reg prometheus.Registerer) error {
	return b.metrics.Register(reg)
}

// UnregisterMetrics unregisters metrics about builder from reg.
func (b *Builder) UnregisterMetrics(reg prometheus.Registerer) {
	b.metrics.Unregister(reg)
}
