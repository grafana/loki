// Package indexobj provides tooling for creating index-oriented data objects.
package indexobj

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
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
	tenant  string
	cfg     logsobj.BuilderBaseConfig
	metrics *BuilderMetrics

	currentSizeEstimate int
	builderFull         bool

	builder *dataobj.Builder // Inner builder for accumulating sections.

	// Each single section builder is nil until first use, and Reset sets it
	// back to nil.
	streams       *streams.Builder
	pointers      *pointers.Builder
	indexPointers *indexpointers.Builder

	stats    map[string]*stats.Builder    // The key is the TenantID.
	postings map[string]*postings.Builder // The key is the TenantID.

	// Hot-path cache for the postings builder. Postings are observed per
	// (record × stream label), so getPostingsBuilderForTenant runs in a tight
	// loop. The Calculate pipeline holds builderMtx for the entire ProcessBatch
	// of a single tenant, so consecutive observations always target the same
	// tenant; caching the resolved pointer lets the inner loop skip the map
	// lookup. Invalidated on Reset.
	lastPostingsTenant  string
	lastPostingsBuilder *postings.Builder

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

// NewBuilder returns a [Builder] that builds index objects for tenant.
//
// NewBuilder returns an error if tenant is empty or the provided config is
// invalid.
func NewBuilder(tenant string, cfg logsobj.BuilderBaseConfig, scratchStore scratch.Store, metrics *BuilderMetrics) (*Builder, error) {
	if tenant == "" {
		return nil, errors.New("tenant must not be empty")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	metrics.ObserveConfig(cfg)

	return &Builder{
		tenant:  tenant,
		cfg:     cfg,
		metrics: metrics,

		builder:  dataobj.NewBuilder(scratchStore),
		stats:    make(map[string]*stats.Builder),
		postings: make(map[string]*postings.Builder),
	}, nil
}

// Tenant returns the tenant the builder is bound to.
func (b *Builder) Tenant() string {
	return b.tenant
}

func (b *Builder) GetEstimatedSize() int {
	return b.currentSizeEstimate
}

func (b *Builder) IsFull() bool {
	return b.builderFull
}

func (b *Builder) getIndexPointersBuilder() *indexpointers.Builder {
	if b.indexPointers == nil {
		b.indexPointers = indexpointers.NewBuilder(b.metrics.indexPointers, int(b.cfg.TargetPageSize), b.cfg.MaxPageRows)
		b.indexPointers.SetTenant(b.tenant)
	}
	return b.indexPointers
}

func (b *Builder) getStatsBuilderForTenant(tenantID string) *stats.Builder {
	if _, ok := b.stats[tenantID]; !ok {
		sb := stats.NewBuilder(b.metrics.stats, stats.ColumnarSectionEncoder(int(b.cfg.TargetPageSize), b.cfg.MaxPageRows))
		sb.SetTenant(tenantID)
		b.stats[tenantID] = sb
	}
	return b.stats[tenantID]
}

func (b *Builder) getPostingsBuilderForTenant(tenantID string) *postings.Builder {
	if b.lastPostingsBuilder != nil && b.lastPostingsTenant == tenantID {
		return b.lastPostingsBuilder
	}
	pb, ok := b.postings[tenantID]
	if !ok {
		pb = postings.NewBuilder(b.metrics.postings, int(b.cfg.TargetPageSize), b.cfg.MaxPageRows, int(b.cfg.TargetSectionSize))
		pb.SetTenant(tenantID)
		b.postings[tenantID] = pb
	}
	b.lastPostingsTenant = tenantID
	b.lastPostingsBuilder = pb
	return pb
}

// AppendStat records a per-sort-key aggregate for a data object section.
func (b *Builder) AppendStat(tenantID, objectPath string, sectionIdx int64,
	shardBucket uint32, sortSchema string, labels map[string]string, minTs, maxTs time.Time, rows int, uncompressedSize int64) error {
	b.metrics.appendsTotal.Inc()

	timer := prometheus.NewTimer(b.metrics.appendTime)
	defer timer.ObserveDuration()

	tenantStats := b.getStatsBuilderForTenant(tenantID)
	preAppendSizeEstimate := tenantStats.EstimatedSize()

	tenantStats.Append(stats.Stat{
		ObjectPath:       objectPath,
		SectionIndex:     sectionIdx,
		ShardBucket:      shardBucket,
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

// ObserveLabelPosting records a label-based posting observation for a data
// object column. Multiple observations sharing the same
// (ObjectPath, SectionIndex, ColumnName, LabelValue) key in obs are
// aggregated internally. The aggregated postings are flushed when
// [Builder.Flush] is called.
//
// Unlike other section types (stats, pointers), postings are NOT flushed
// mid-stream when they exceed TargetSectionSize. The aggregation model
// requires all observations for a section to be present before encoding
// (bitmap normalization, bloom filter construction). The builderFull flag
// provides back-pressure via TargetObjectSize.
func (b *Builder) ObserveLabelPosting(tenantID string, obs postings.LabelObservation) {
	// Postings are observed per (record × stream label), so this method runs in
	// a hot loop that fires hundreds of thousands of times per logs section.
	// Per-call prometheus.NewTimer / Histogram.Observe / sizeEstimate.Set were
	// designed for the previous per-posting Append API and are too expensive at
	// this granularity. We keep the cheap atomic counter and account for size
	// growth via the aggregator delta only.
	b.metrics.appendsTotal.Inc()

	tenantPostings := b.getPostingsBuilderForTenant(tenantID)
	preSize := tenantPostings.EstimatedSize()

	tenantPostings.ObserveLabelPosting(obs)

	postSize := tenantPostings.EstimatedSize()
	b.unflushedSizeEstimate += postSize - preSize
	b.currentSizeEstimate += postSize - preSize
	b.state = builderStateDirty
	if b.currentSizeEstimate > int(b.cfg.TargetObjectSize) {
		b.builderFull = true
	}
}

// PrepareBloomColumn initializes the bloom filter for a specific column.
// Must be called before any ObserveBloomPosting calls for the given (objectPath, sectionIdx, columnName).
// shardBuckets is stored on the entry immediately so a prepared-but-unobserved
// column still records the object's shard factor.
func (b *Builder) PrepareBloomColumn(tenantID, objectPath string, sectionIdx int64,
	columnName string, estimatedCardinality uint, shardBuckets int64) {
	tenantPostings := b.getPostingsBuilderForTenant(tenantID)
	tenantPostings.PrepareBloomColumn(objectPath, sectionIdx, columnName, estimatedCardinality, shardBuckets)
}

// ObserveBloomPosting records a bloom-filter posting observation for a data
// object column. Returns an error if the column has not been prepared via
// PrepareBloomColumn. The aggregated postings are flushed when
// [Builder.Flush] is called.
func (b *Builder) ObserveBloomPosting(tenantID string, obs postings.BloomObservation) error {
	// See ObserveLabelPosting for why metrics.appendTime / sizeEstimate.Set are
	// not updated per observation.
	b.metrics.appendsTotal.Inc()

	tenantPostings := b.getPostingsBuilderForTenant(tenantID)
	preSize := tenantPostings.EstimatedSize()

	if err := tenantPostings.ObserveBloomPosting(obs); err != nil {
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

// BloomBytes returns the marshaled bloom filter bytes for a specific column.
// Returns an error if the column has not been prepared via PrepareBloomColumn.
func (b *Builder) BloomBytes(tenantID, objectPath string, sectionIdx int64, columnName string) ([]byte, error) {
	tenantPostings := b.getPostingsBuilderForTenant(tenantID)
	return tenantPostings.BloomBytes(objectPath, sectionIdx, columnName)
}

func (b *Builder) AppendIndexPointer(pointer indexpointers.IndexPointer) error {
	b.metrics.appendsTotal.Inc()
	newEntrySize := len(pointer.Path) + 1 + 1 + 8 + 8 // path, startTs, endTs, fileSize, uncompressedLogsSize

	if b.state != builderStateEmpty && b.currentSizeEstimate+newEntrySize > int(b.cfg.TargetObjectSize) {
		b.builderFull = true
	}

	timer := prometheus.NewTimer(b.metrics.appendTime)
	defer timer.ObserveDuration()

	indexPointersBuilder := b.getIndexPointersBuilder()
	preAppendSizeEstimate := indexPointersBuilder.EstimatedSize()

	indexPointersBuilder.Append(pointer.Path, pointer.StartTs, pointer.EndTs)

	postAppendSizeEstimate := indexPointersBuilder.EstimatedSize()
	b.unflushedSizeEstimate += postAppendSizeEstimate - preAppendSizeEstimate

	if postAppendSizeEstimate > int(b.cfg.TargetSectionSize) {
		if err := b.builder.Append(indexPointersBuilder); err != nil {
			return err
		}
	}

	b.currentSizeEstimate = b.estimatedSize()
	b.state = builderStateDirty

	return nil
}

func (b *Builder) getStreamsBuilder() *streams.Builder {
	if b.streams == nil {
		b.streams = streams.NewBuilder(b.metrics.streams, int(b.cfg.TargetPageSize), b.cfg.MaxPageRows)
		b.streams.SetTenant(b.tenant)
	}
	return b.streams
}

// AppendStream appends a stream to the object's stream section, returning the stream ID within this object.
func (b *Builder) AppendStream(stream streams.Stream) (int64, error) {
	b.metrics.appendsTotal.Inc()

	newEntrySize := labelsEstimate(stream.Labels) + 2

	if b.state != builderStateEmpty && b.currentSizeEstimate+newEntrySize > int(b.cfg.TargetObjectSize) {
		b.builderFull = true
	}

	timer := prometheus.NewTimer(b.metrics.appendTime)
	defer timer.ObserveDuration()

	streamsBuilder := b.getStreamsBuilder()
	preAppendSizeEstimate := streamsBuilder.EstimatedSize()

	// Record the stream in the stream section.
	// Once to capture the min timestamp and uncompressed size, again to record the max timestamp.
	streamID := streamsBuilder.Record(stream.Labels, stream.MinTimestamp, stream.UncompressedSize)
	_ = streamsBuilder.Record(stream.Labels, stream.MaxTimestamp, 0)

	postAppendSizeEstimate := streamsBuilder.EstimatedSize()
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

func (b *Builder) getPointersBuilder() *pointers.Builder {
	if b.pointers == nil {
		b.pointers = pointers.NewBuilder(b.metrics.pointers, int(b.cfg.TargetPageSize), b.cfg.MaxPageRows)
		b.pointers.SetTenant(b.tenant)
	}
	return b.pointers
}

// ObserveLogLine records a log line observation for a stream in the pointers section.
func (b *Builder) ObserveLogLine(path string, section int64, streamIDInObject int64, streamIDInIndex int64, ts time.Time, uncompressedSize int64) error {
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

	pointersBuilder := b.getPointersBuilder()
	preAppendSizeEstimate := pointersBuilder.EstimatedSize()

	pointersBuilder.ObserveStream(path, section, streamIDInObject, streamIDInIndex, ts, uncompressedSize)

	postAppendSizeEstimate := pointersBuilder.EstimatedSize()
	b.unflushedSizeEstimate += postAppendSizeEstimate - preAppendSizeEstimate

	b.currentSizeEstimate = b.estimatedSize()
	b.state = builderStateDirty
	return nil
}

// AppendColumnIndex records a column index entry with bloom filter data in the pointers section.
func (b *Builder) AppendColumnIndex(path string, section int64, columnName string, columnIndex int64, valuesBloom []byte) error {
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

	pointersBuilder := b.getPointersBuilder()
	preAppendSizeEstimate := pointersBuilder.EstimatedSize()

	pointersBuilder.RecordColumnIndex(path, section, columnName, columnIndex, valuesBloom)

	postAppendSizeEstimate := pointersBuilder.EstimatedSize()
	b.unflushedSizeEstimate += postAppendSizeEstimate - preAppendSizeEstimate

	// If our logs section has gotten big enough, we want to flush it to the
	// encoder and start a new section.
	if postAppendSizeEstimate > int(b.cfg.TargetSectionSize) {
		if err := b.builder.Append(pointersBuilder); err != nil {
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
// For each tenant, the range is the union of its streams and postings ranges;
// a source with no observations (zero time range) does not contribute.
func (b *Builder) TimeRanges() []dataobj.TimeRange {
	tenantIDs := make(map[string]struct{}, 1+len(b.postings))
	if b.streams != nil {
		tenantIDs[b.tenant] = struct{}{}
	}
	for tenantID := range b.postings {
		tenantIDs[tenantID] = struct{}{}
	}

	timeRanges := make([]dataobj.TimeRange, 0, len(tenantIDs))
	for tenantID := range tenantIDs {
		var minTime, maxTime time.Time

		if b.streams != nil && tenantID == b.tenant {
			sMin, sMax := b.streams.TimeRange()
			minTime, maxTime = unionTimeRange(minTime, maxTime, sMin, sMax)
		}
		if p, ok := b.postings[tenantID]; ok {
			pMin, pMax := p.TimeRange()
			minTime, maxTime = unionTimeRange(minTime, maxTime, pMin, pMax)
		}

		if minTime.IsZero() && maxTime.IsZero() {
			continue
		}
		timeRanges = append(timeRanges, dataobj.TimeRange{
			Tenant:  tenantID,
			MinTime: minTime,
			MaxTime: maxTime,
		})
	}
	return timeRanges
}

func unionTimeRange(curMin, curMax, candMin, candMax time.Time) (time.Time, time.Time) {
	if candMin.IsZero() {
		return curMin, curMax
	}
	if curMin.IsZero() || candMin.Before(curMin) {
		curMin = candMin
	}
	if curMax.IsZero() || candMax.After(curMax) {
		curMax = candMax
	}
	return curMin, curMax
}

// Flush flushes all buffered data to the buffer provided. Calling Flush can result
// in a no-op if there is no buffered data to flush.
//
// On success the caller owns the returned [io.Closer] and must close it to
// release the object's backing scratch storage; reads of the object fail once
// it is closed. If an error is returned the closer is always nil.
//
// Flush always resets Builder.
func (b *Builder) Flush() (*dataobj.Object, io.Closer, error) {
	if b.state == builderStateEmpty {
		return nil, nil, ErrBuilderEmpty
	}
	defer b.Reset()

	b.metrics.flushTotal.Inc()
	timer := prometheus.NewTimer(b.metrics.buildTime)
	defer timer.ObserveDuration()

	var flushErrors []error

	if b.streams != nil && b.streams.EstimatedSize() > 0 {
		flushErrors = append(flushErrors, b.builder.Append(b.streams))
	}
	if b.pointers != nil && b.pointers.EstimatedSize() > 0 {
		flushErrors = append(flushErrors, b.builder.Append(b.pointers))
	}
	if b.indexPointers != nil && b.indexPointers.EstimatedSize() > 0 {
		flushErrors = append(flushErrors, b.builder.Append(b.indexPointers))
	}

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

	if err := b.observeObject(context.Background(), obj); err != nil {
		return nil, nil, errors.Join(fmt.Errorf("observing object: %w", err), closer.Close())
	}

	return obj, closer, nil
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
	b.streams = nil
	b.pointers = nil
	b.indexPointers = nil
	b.stats = make(map[string]*stats.Builder)
	b.postings = make(map[string]*postings.Builder)
	b.lastPostingsTenant = ""
	b.lastPostingsBuilder = nil

	b.metrics.sizeEstimate.Set(0)
	b.currentSizeEstimate = 0
	b.unflushedSizeEstimate = 0
	b.builderFull = false
	b.state = builderStateEmpty
}
