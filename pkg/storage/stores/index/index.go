package index

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/weaveworks/common/instrument"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
)

type Filterable interface {
	// SetChunkFilterer sets a chunk filter to be used when retrieving chunks.
	SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer)
}

type BaseReader interface {
	GetSeries(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]labels.Labels, error)
	LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error)
	LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string) ([]string, error)
	Stats(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (*stats.Stats, error)
}

type Reader interface {
	BaseReader
	GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]logproto.ChunkRef, error)
	Filterable
}

type Writer interface {
	IndexChunk(ctx context.Context, from, through model.Time, chk chunk.Chunk) error
}

type ReaderWriter interface {
	Reader
	Writer
}

type monitoredReaderWriter struct {
	rw      ReaderWriter
	metrics *metrics
}

func NewMonitoredReaderWriter(rw ReaderWriter, reg prometheus.Registerer) ReaderWriter {
	return &monitoredReaderWriter{
		rw:      rw,
		metrics: newMetrics(reg),
	}
}

func (m monitoredReaderWriter) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]logproto.ChunkRef, error) {
	var chunks []logproto.ChunkRef

	if err := instrument.CollectedRequest(ctx, "chunk_refs", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		chunks, err = m.rw.GetChunkRefs(ctx, userID, from, through, matchers...)
		return err
	}); err != nil {
		return nil, err
	}

	return chunks, nil
}

func (m monitoredReaderWriter) GetSeries(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]labels.Labels, error) {
	var lbls []labels.Labels
	if err := instrument.CollectedRequest(ctx, "series", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		lbls, err = m.rw.GetSeries(ctx, userID, from, through, matchers...)
		return err
	}); err != nil {
		return nil, err
	}

	return lbls, nil
}

func (m monitoredReaderWriter) LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error) {
	var values []string
	if err := instrument.CollectedRequest(ctx, "label_values", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		values, err = m.rw.LabelValuesForMetricName(ctx, userID, from, through, metricName, labelName, matchers...)
		return err
	}); err != nil {
		return nil, err
	}

	return values, nil
}

func (m monitoredReaderWriter) LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string) ([]string, error) {
	var values []string
	if err := instrument.CollectedRequest(ctx, "label_names", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		values, err = m.rw.LabelNamesForMetricName(ctx, userID, from, through, metricName)
		return err
	}); err != nil {
		return nil, err
	}

	return values, nil
}

func (m monitoredReaderWriter) Stats(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (*stats.Stats, error) {
	var sts *stats.Stats
	if err := instrument.CollectedRequest(ctx, "stats", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(innerCtx context.Context) error {
		var err error
		sts, err = m.rw.Stats(innerCtx, userID, from, through, matchers...)
		return err
	}); err != nil {
		return nil, err
	}

	return sts, nil

}

func (m monitoredReaderWriter) SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer) {
	m.rw.SetChunkFilterer(chunkFilter)
}

func (m monitoredReaderWriter) IndexChunk(ctx context.Context, from, through model.Time, chk chunk.Chunk) error {
	return instrument.CollectedRequest(ctx, "index_chunk", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(ctx context.Context) error {
		return m.rw.IndexChunk(ctx, from, through, chk)
	})
}
