// Package indexobj provides tooling for creating index-oriented data objects.
package indexobj

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/scratch"
)

var ErrBuilderEmpty = errors.New("builder empty")

// A Builder constructs a single-tenant index object. Callers add index data with the
// Append and Observe methods, and build the object with [Builder.Flush].
//
// Methods on Builder are not goroutine-safe; callers are responsible for
// synchronization.
type Builder struct {
	tenant  string
	cfg     logsobj.BuilderBaseConfig
	metrics *BuilderMetrics

	builder *dataobj.Builder // Inner builder for accumulating sections.

	// Each section builder is nil until first use, and Reset sets it back to
	// nil. NewBuilder does not create them, so a Builder is cheap to create.
	streams       *streams.Builder
	pointers      *pointers.Builder
	indexPointers *indexpointers.Builder
	stats         *stats.Builder
	postings      *postings.Builder

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

		builder: dataobj.NewBuilder(scratchStore),
	}, nil
}

func (b *Builder) Tenant() string {
	return b.tenant
}

func (b *Builder) getIndexPointersBuilder() *indexpointers.Builder {
	if b.indexPointers == nil {
		b.indexPointers = indexpointers.NewBuilder(b.metrics.indexPointers, int(b.cfg.TargetPageSize), b.cfg.MaxPageRows)
		b.indexPointers.SetTenant(b.tenant)
	}
	return b.indexPointers
}

func (b *Builder) getStatsBuilder() *stats.Builder {
	if b.stats == nil {
		b.stats = stats.NewBuilder(b.metrics.stats, stats.ColumnarSectionEncoder(int(b.cfg.TargetPageSize), b.cfg.MaxPageRows))
		b.stats.SetTenant(b.tenant)
	}
	return b.stats
}

func (b *Builder) getPostingsBuilder() *postings.Builder {
	if b.postings == nil {
		b.postings = postings.NewBuilder(b.metrics.postings, int(b.cfg.TargetPageSize), b.cfg.MaxPageRows, int(b.cfg.TargetSectionSize))
		b.postings.SetTenant(b.tenant)
	}
	return b.postings
}

// AppendStat records a per-sort-key aggregate for a data object section.
func (b *Builder) AppendStat(objectPath string, sectionIdx int64,
	shardBucket uint32, sortSchema string, labels map[string]string, minTs, maxTs time.Time, rows int, uncompressedSize int64) error {
	b.metrics.appendsTotal.Inc()

	statsBuilder := b.getStatsBuilder()

	statsBuilder.Append(stats.Stat{
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

	if statsBuilder.EstimatedSize() > int(b.cfg.TargetSectionSize) {
		if err := b.builder.Append(statsBuilder); err != nil {
			return err
		}
	}

	b.state = builderStateDirty

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
// (bitmap normalization, bloom filter construction).
func (b *Builder) ObserveLabelPosting(obs postings.LabelObservation) {
	b.metrics.appendsTotal.Inc()

	b.getPostingsBuilder().ObserveLabelPosting(obs)

	b.state = builderStateDirty
}

// PrepareBloomColumn initializes the bloom filter for a specific column.
// Must be called before any ObserveBloomPosting calls for the given (objectPath, sectionIdx, columnName).
// shardBuckets is stored on the entry immediately so a prepared-but-unobserved
// column still records the object's shard factor.
func (b *Builder) PrepareBloomColumn(
	objectPath string,
	sectionIdx int64,
	columnName string,
	estimatedCardinality uint,
	shardBuckets int64,
) {
	b.getPostingsBuilder().PrepareBloomColumn(objectPath, sectionIdx, columnName, estimatedCardinality, shardBuckets)
}

// ObserveBloomPosting records a bloom-filter posting observation for a data
// object column. Returns an error if the column has not been prepared via
// PrepareBloomColumn. The aggregated postings are flushed when
// [Builder.Flush] is called.
func (b *Builder) ObserveBloomPosting(obs postings.BloomObservation) error {
	b.metrics.appendsTotal.Inc()

	if err := b.getPostingsBuilder().ObserveBloomPosting(obs); err != nil {
		return err
	}

	b.state = builderStateDirty
	return nil
}

// BloomBytes returns the marshaled bloom filter bytes for a specific column.
// Returns an error if the column has not been prepared via PrepareBloomColumn.
func (b *Builder) BloomBytes(objectPath string, sectionIdx int64, columnName string) ([]byte, error) {
	return b.getPostingsBuilder().BloomBytes(objectPath, sectionIdx, columnName)
}

func (b *Builder) AppendIndexPointer(pointer indexpointers.IndexPointer) error {
	b.metrics.appendsTotal.Inc()

	indexPointersBuilder := b.getIndexPointersBuilder()
	indexPointersBuilder.Append(pointer.Path, pointer.StartTs, pointer.EndTs)
	b.state = builderStateDirty

	if indexPointersBuilder.EstimatedSize() > int(b.cfg.TargetSectionSize) {
		if err := b.builder.Append(indexPointersBuilder); err != nil {
			return err
		}
	}

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

	streamsBuilder := b.getStreamsBuilder()

	// Record the stream in the stream section.
	// Once to capture the min timestamp and uncompressed size, again to record the max timestamp.
	streamID := streamsBuilder.Record(stream.Labels, stream.MinTimestamp, stream.UncompressedSize)
	_ = streamsBuilder.Record(stream.Labels, stream.MaxTimestamp, 0)

	b.state = builderStateDirty

	return streamID, nil
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
	b.metrics.appendsTotal.Inc()

	b.getPointersBuilder().ObserveStream(path, section, streamIDInObject, streamIDInIndex, ts, uncompressedSize)

	b.state = builderStateDirty
	return nil
}

// AppendColumnIndex records a column index entry with bloom filter data in the pointers section.
func (b *Builder) AppendColumnIndex(path string, section int64, columnName string, columnIndex int64, valuesBloom []byte) error {
	b.metrics.appendsTotal.Inc()

	pointersBuilder := b.getPointersBuilder()

	pointersBuilder.RecordColumnIndex(path, section, columnName, columnIndex, valuesBloom)
	b.state = builderStateDirty

	// If our logs section has gotten big enough, we want to flush it to the
	// encoder and start a new section.
	if pointersBuilder.EstimatedSize() > int(b.cfg.TargetSectionSize) {
		if err := b.builder.Append(pointersBuilder); err != nil {
			return err
		}
	}

	return nil
}

// TimeRange returns the builder's tenant and the time range of the data in the
// builder. The range is the union of the ranges of the streams and postings
// section builders; a section builder with no observations does not
// contribute. MinTime and MaxTime are zero when neither has observations.
func (b *Builder) TimeRange() dataobj.TimeRange {
	var minTime, maxTime time.Time
	if b.streams != nil {
		sMin, sMax := b.streams.TimeRange()
		minTime, maxTime = unionTimeRange(minTime, maxTime, sMin, sMax)
	}
	if b.postings != nil {
		pMin, pMax := b.postings.TimeRange()
		minTime, maxTime = unionTimeRange(minTime, maxTime, pMin, pMax)
	}
	return dataobj.TimeRange{
		Tenant:  b.tenant,
		MinTime: minTime,
		MaxTime: maxTime,
	}
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
	if b.stats != nil && b.stats.EstimatedSize() > 0 {
		flushErrors = append(flushErrors, b.builder.Append(b.stats))
	}
	if b.postings != nil && b.postings.EstimatedSize() > 0 {
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
	b.stats = nil
	b.postings = nil

	b.state = builderStateEmpty
}
