package index

import (
	"context"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
)

// RequestChunkFilterer creates ChunkFilterer for a given request context.
type RequestChunkFilterer interface {
	ForRequest(ctx context.Context) Filterer
}

// Filterer filters chunks based on the metric.
type Filterer interface {
	ShouldFilter(metric labels.Labels) bool
}

type Filterable interface {
	// SetChunkFilterer sets a chunk filter to be used when retrieving chunks.
	SetChunkFilterer(filterer RequestChunkFilterer)
}

type ReadStoreBase interface {
	GetSeries(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]labels.Labels, error)
	LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error)
	LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string) ([]string, error)
	Stats(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (*stats.Stats, error)
	Volume(ctx context.Context, userID string, from, through model.Time, limit int32, targetLabels []string, aggregateBy string, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error)
}

type ReadStore interface {
	ReadStoreBase
	Filterable
	GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]logproto.ChunkRef, error)
}

type WriteStore interface {
	IndexChunk(ctx context.Context, from, through model.Time, chk chunk.Chunk) error
}

type ReadWriteStore interface {
	ReadStore
	WriteStore
}
