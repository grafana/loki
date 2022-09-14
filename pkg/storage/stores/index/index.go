package index

import (
	"context"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
)

type Reader interface {
	GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]logproto.ChunkRef, error)
	GetSeries(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]labels.Labels, error)
	LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error)
	LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string) ([]string, error)
	Stats(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (*stats.Stats, error)
	// SetChunkFilterer sets a chunk filter to be used when retrieving chunks.
	// This is only used for GetSeries implementation.
	// Todo we might want to pass it as a parameter to GetSeries instead.
	SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer)
}

type Writer interface {
	IndexChunk(ctx context.Context, chk chunk.Chunk) error
}

type ReaderWriter interface {
	Reader
	Writer
}
