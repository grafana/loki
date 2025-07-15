// Package logsobj provides tooling for creating logs-oriented data objects.
package logsobj

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
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
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

	builder *dataobj.Builder            // Inner builder for accumulating sections.
	streams map[string]*streams.Builder // The key is the TenantID.
	logs    map[string]*logs.Builder    // The key is the TenantID.

	// Counts the number of tenants in each data object.
	tenants prometheus.Histogram

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

		builder: dataobj.NewBuilder(),
		streams: make(map[string]*streams.Builder),
		logs:    make(map[string]*logs.Builder),

		tenants: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki",
			Subsystem: "dataobj",
			Name:      "tenants",
			Help:      "The distribution of tenants per object.",

			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
	}, nil
}

func (b *MultiTenantBuilder) GetEstimatedSize() int {
	return b.currentSizeEstimate
}

// Append buffers a stream to be written to a data object. Append returns an
// error if the stream labels cannot be parsed or [ErrBuilderFull] if the
// builder is full.
//
// Once a Builder is full, call [Builder.Flush] to flush the buffered data,
// then call Append again with the same entry.
func (b *MultiTenantBuilder) Append(tenantID string, stream logproto.Stream) error {
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

	tenantStreams, ok := b.streams[tenantID]
	if !ok {
		tenantStreams = streams.NewBuilder(tenantID, b.metrics.streams, int(b.cfg.TargetPageSize))
		b.streams[tenantID] = tenantStreams
	}

	tenantLogs, ok := b.logs[tenantID]
	if !ok {
		tenantLogs = logs.NewBuilder(tenantID, b.metrics.logs, logs.BuilderOptions{
			PageSizeHint:     int(b.cfg.TargetPageSize),
			BufferSize:       int(b.cfg.BufferSize),
			StripeMergeLimit: b.cfg.SectionStripeMergeLimit,
		})
		b.logs[tenantID] = tenantLogs
	}

	timer := prometheus.NewTimer(b.metrics.appendTime)
	defer timer.ObserveDuration()

	for _, entry := range stream.Entries {
		sz := int64(len(entry.Line))
		for _, md := range entry.StructuredMetadata {
			sz += int64(len(md.Value))
		}

		streamID := tenantStreams.Record(ls, entry.Timestamp, sz)

		tenantLogs.Append(logs.Record{
			StreamID:  streamID,
			Timestamp: entry.Timestamp,
			Metadata:  convertMetadata(entry.StructuredMetadata),
			Line:      []byte(entry.Line),
		})

		// If our logs section has gotten big enough, we want to flush it to the
		// encoder and start a new section.
		if tenantLogs.UncompressedSize() > int(b.cfg.TargetSectionSize) {
			if err := b.builder.Append(tenantLogs); err != nil {
				return err
			}
		}
	}

	b.currentSizeEstimate = b.estimatedSize()
	b.state = builderStateDirty
	return nil
}

func (b *MultiTenantBuilder) estimatedSize() int {
	var size int
	for _, tenantStreams := range b.streams {
		size += tenantStreams.EstimatedSize()
	}
	for _, tenantLogs := range b.logs {
		size += tenantLogs.EstimatedSize()
	}
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

	timer := prometheus.NewTimer(b.metrics.buildTime)
	defer timer.ObserveDuration()

	// Appending sections resets them, so we need to load the time range before
	// appending.
	var minTime, maxTime time.Time
	for _, ts := range b.streams {
		tMinTime, tMaxTime := ts.TimeRange()
		if minTime.Equal(time.Time{}) || tMinTime.Before(minTime) {
			minTime = tMinTime
		}
		if maxTime.Equal(time.Time{}) || tMaxTime.After(maxTime) {
			maxTime = tMaxTime
		}
	}

	// Flush sections one more time in case they have data.
	var flushErrors []error
	var tenantIDs []string

	for tenant := range b.streams {
		if err := b.builder.Append(b.streams[tenant]); err != nil {
			flushErrors = append(flushErrors, err)
			continue
		}

		if tenantLogs, ok := b.logs[tenant]; ok {
			if err := b.builder.Append(tenantLogs); err != nil {
				flushErrors = append(flushErrors, err)
				continue
			}
		}
		tenantIDs = append(tenantIDs, tenant)
	}

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
	b.tenants.Observe(float64(len(b.streams)))

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
	return FlushStats{TenantIDs: tenantIDs, MinTimestamp: minTime, MaxTimestamp: maxTime}, err
}

func (b *MultiTenantBuilder) observeObject(ctx context.Context, obj *dataobj.Object) error {
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
func (b *MultiTenantBuilder) Reset() {
	b.builder.Reset()
	// TODO(grobinson): Return these to a pool. For now, just allow them to
	// be GC'd and we will create new ones.
	b.logs = make(map[string]*logs.Builder)
	b.streams = make(map[string]*streams.Builder)

	b.metrics.sizeEstimate.Set(0)
	b.currentSizeEstimate = 0
	b.state = builderStateEmpty
}

// RegisterMetrics registers metrics about builder to report to reg. All
// metrics will have a tenant label set to the tenant ID of the Builder.
//
// If multiple Builders for the same tenant are running in the same process,
// reg must contain additional labels to differentiate between them.
func (b *MultiTenantBuilder) RegisterMetrics(reg prometheus.Registerer) error {
	var errs []error
	errs = append(errs, b.metrics.Register(reg))
	errs = append(errs, reg.Register(b.tenants))
	return errors.Join(errs...)
}

// UnregisterMetrics unregisters metrics about builder from reg.
func (b *MultiTenantBuilder) UnregisterMetrics(reg prometheus.Registerer) {
	b.metrics.Unregister(reg)
	reg.Unregister(b.tenants)
}

func (b *MultiTenantBuilder) parseLabels(labelString string) (labels.Labels, error) {
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
