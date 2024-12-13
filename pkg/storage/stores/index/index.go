package index

import (
	"context"
	"time"

	"github.com/grafana/dskit/instrument"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
	loki_instrument "github.com/grafana/loki/v3/pkg/util/instrument"
)

type Filterable interface {
	// SetChunkFilterer sets a chunk filter to be used when retrieving chunks.
	SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer)
}

type BaseReader interface {
	GetSeries(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]labels.Labels, error)
	LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error)
	LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, matchers ...*labels.Matcher) ([]string, error)
}

type StatsReader interface {
	Stats(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (*stats.Stats, error)
	Volume(ctx context.Context, userID string, from, through model.Time, limit int32, targetLabels []string, aggregateBy string, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error)
	GetShards(
		ctx context.Context,
		userID string,
		from, through model.Time,
		targetBytesPerShard uint64,
		predicate chunk.Predicate,
	) (*logproto.ShardsResponse, error)

	// If the underlying index supports it, this will return the ForSeries interface
	// which is used in bloom-filter accelerated sharding calculation optimization.
	HasForSeries(from, through model.Time) (sharding.ForSeries, bool)
}

type Reader interface {
	BaseReader
	StatsReader
	GetChunkRefs(ctx context.Context, userID string, from, through model.Time, predicate chunk.Predicate) ([]logproto.ChunkRef, error)
	Filterable
}

type Writer interface {
	IndexChunk(ctx context.Context, from, through model.Time, chk chunk.Chunk) error
}

type ReaderWriter interface {
	Reader
	Writer
}

type MonitoredReaderWriter struct {
	rw      ReaderWriter
	metrics *metrics
}

func NewMonitoredReaderWriter(rw ReaderWriter, reg prometheus.Registerer) *MonitoredReaderWriter {
	return &MonitoredReaderWriter{
		rw:      rw,
		metrics: newMetrics(reg),
	}
}

func (m MonitoredReaderWriter) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, predicate chunk.Predicate) ([]logproto.ChunkRef, error) {
	var chunks []logproto.ChunkRef

	if err := loki_instrument.TimeRequest(ctx, "chunk_refs", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		chunks, err = m.rw.GetChunkRefs(ctx, userID, from, through, predicate)
		return err
	}); err != nil {
		return nil, err
	}

	return chunks, nil
}

func (m MonitoredReaderWriter) GetSeries(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]labels.Labels, error) {
	var lbls []labels.Labels
	if err := loki_instrument.TimeRequest(ctx, "series", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		lbls, err = m.rw.GetSeries(ctx, userID, from, through, matchers...)
		return err
	}); err != nil {
		return nil, err
	}

	return lbls, nil
}

func (m MonitoredReaderWriter) LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error) {
	var values []string
	if err := loki_instrument.TimeRequest(ctx, "label_values", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		values, err = m.rw.LabelValuesForMetricName(ctx, userID, from, through, metricName, labelName, matchers...)
		return err
	}); err != nil {
		return nil, err
	}

	return values, nil
}

func (m MonitoredReaderWriter) LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, matchers ...*labels.Matcher) ([]string, error) {
	var values []string
	if err := loki_instrument.TimeRequest(ctx, "label_names", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		values, err = m.rw.LabelNamesForMetricName(ctx, userID, from, through, metricName, matchers...)
		return err
	}); err != nil {
		return nil, err
	}

	return values, nil
}

func (m MonitoredReaderWriter) Stats(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (*stats.Stats, error) {
	var sts *stats.Stats
	if err := loki_instrument.TimeRequest(ctx, "stats", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		sts, err = m.rw.Stats(ctx, userID, from, through, matchers...)
		return err
	}); err != nil {
		return nil, err
	}

	return sts, nil
}

func (m MonitoredReaderWriter) Volume(ctx context.Context, userID string, from, through model.Time, limit int32, targetLabels []string, aggregateBy string, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	var vol *logproto.VolumeResponse
	if err := loki_instrument.TimeRequest(ctx, "volume", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		vol, err = m.rw.Volume(ctx, userID, from, through, limit, targetLabels, aggregateBy, matchers...)
		return err
	}); err != nil {
		return nil, err
	}

	return vol, nil
}

func (m MonitoredReaderWriter) GetShards(
	ctx context.Context,
	userID string,
	from, through model.Time,
	targetBytesPerShard uint64,
	predicate chunk.Predicate,
) (*logproto.ShardsResponse, error) {
	var shards *logproto.ShardsResponse
	if err := loki_instrument.TimeRequest(ctx, "shards", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		start := time.Now()
		shards, err = m.rw.GetShards(ctx, userID, from, through, targetBytesPerShard, predicate)

		if err == nil {
			// record duration here from caller to avoid needing to do this in two separate places:
			// 1) when we resolve shards from the index alone
			// 2) when we resolve shards from the index + blooms
			// NB(owen-d): since this is measured by the callee, it does not include time in queue,
			// over the wire, etc.
			shards.Statistics.Index.ShardsDuration = int64(time.Since(start))
		}

		return err
	}); err != nil {
		return nil, err
	}
	return shards, nil
}

func (m MonitoredReaderWriter) SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer) {
	m.rw.SetChunkFilterer(chunkFilter)
}

func (m MonitoredReaderWriter) IndexChunk(ctx context.Context, from, through model.Time, chk chunk.Chunk) error {
	return loki_instrument.TimeRequest(ctx, "index_chunk", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(ctx context.Context) error {
		return m.rw.IndexChunk(ctx, from, through, chk)
	})
}

func (m MonitoredReaderWriter) HasForSeries(from, through model.Time) (sharding.ForSeries, bool) {
	if impl, ok := m.rw.HasForSeries(from, through); ok {
		wrapped := sharding.ForSeriesFunc(
			func(
				ctx context.Context,
				userID string,
				fpFilter index.FingerprintFilter,
				from model.Time,
				through model.Time,
				fn func(
					labels.Labels,
					model.Fingerprint,
					[]index.ChunkMeta,
				) (stop bool),
				matchers ...*labels.Matcher,
			) error {
				return loki_instrument.TimeRequest(ctx, "for_series", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(ctx context.Context) error {
					return impl.ForSeries(ctx, userID, fpFilter, from, through, fn, matchers...)
				})
			},
		)
		return wrapped, true
	}
	return nil, false
}
