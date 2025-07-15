package limiter

import (
	"context"
	"fmt"
	"sync"

	"github.com/prometheus/common/model"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/logproto"
)

type queryLimiterCtxKey struct{}

var (
	ctxKey                    = &queryLimiterCtxKey{}
	ErrMaxSeriesHit           = "the query hit the max number of series limit (limit: %d series)"
	ErrMaxChunkBytesHit       = "the query hit the aggregated chunks size limit (limit: %d bytes)"
	ErrMaxChunksPerQueryLimit = "the query hit the max number of chunks limit (limit: %d chunks)"
)

type QueryLimiter struct {
	uniqueSeriesMx sync.Mutex
	uniqueSeries   map[model.Fingerprint]struct{}

	chunkBytesCount atomic.Int64
	chunkCount      atomic.Int64

	maxSeriesPerQuery     int
	maxChunkBytesPerQuery int
	maxChunksPerQuery     int
}

// NewQueryLimiter makes a new per-query limiter. Each query limiter
// is configured using the `maxSeriesPerQuery` limit.
func NewQueryLimiter(maxSeriesPerQuery, maxChunkBytesPerQuery int, maxChunksPerQuery int) *QueryLimiter {
	return &QueryLimiter{
		uniqueSeriesMx: sync.Mutex{},
		uniqueSeries:   map[model.Fingerprint]struct{}{},

		maxSeriesPerQuery:     maxSeriesPerQuery,
		maxChunkBytesPerQuery: maxChunkBytesPerQuery,
		maxChunksPerQuery:     maxChunksPerQuery,
	}
}

func AddQueryLimiterToContext(ctx context.Context, limiter *QueryLimiter) context.Context {
	return context.WithValue(ctx, ctxKey, limiter)
}

// QueryLimiterFromContextWithFallback returns a QueryLimiter from the current context.
// If there is not a QueryLimiter on the context it will return a new no-op limiter.
func QueryLimiterFromContextWithFallback(ctx context.Context) *QueryLimiter {
	ql, ok := ctx.Value(ctxKey).(*QueryLimiter)
	if !ok {
		// If there's no limiter return a new unlimited limiter as a fallback
		ql = NewQueryLimiter(0, 0, 0)
	}
	return ql
}

// AddSeries adds the input series and returns an error if the limit is reached.
func (ql *QueryLimiter) AddSeries(seriesLabels []logproto.LabelAdapter) error {
	// If the max series is unlimited just return without managing map
	if ql.maxSeriesPerQuery == 0 {
		return nil
	}
	fingerprint := client.FastFingerprint(seriesLabels)

	ql.uniqueSeriesMx.Lock()
	defer ql.uniqueSeriesMx.Unlock()

	ql.uniqueSeries[fingerprint] = struct{}{}
	if len(ql.uniqueSeries) > ql.maxSeriesPerQuery {
		// Format error with max limit
		return fmt.Errorf(ErrMaxSeriesHit, ql.maxSeriesPerQuery)
	}
	return nil
}

// AddChunkBytes adds the input chunk size in bytes and returns an error if the limit is reached.
func (ql *QueryLimiter) AddChunkBytes(chunkSizeInBytes int) error {
	if ql.maxChunkBytesPerQuery == 0 {
		return nil
	}
	if ql.chunkBytesCount.Add(int64(chunkSizeInBytes)) > int64(ql.maxChunkBytesPerQuery) {
		return fmt.Errorf(ErrMaxChunkBytesHit, ql.maxChunkBytesPerQuery)
	}
	return nil
}

func (ql *QueryLimiter) AddChunks(count int) error {
	if ql.maxChunksPerQuery == 0 {
		return nil
	}

	if ql.chunkCount.Add(int64(count)) > int64(ql.maxChunksPerQuery) {
		return fmt.Errorf("%s %d", ErrMaxChunksPerQueryLimit, ql.maxChunksPerQuery)
	}
	return nil
}
