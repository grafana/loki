package tsdb

import (
	"context"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
)

type Index interface {
	Bounded
	// GetChunkRefs accepts an optional []ChunkRef argument.
	// If not nil, it will use that slice to build the result,
	// allowing us to avoid unnecessary allocations at the caller's discretion.
	// If nil, the underlying index implementation is required
	// to build the resulting slice nonetheless (it should not panic),
	// ideally by requesting a slice from the pool.
	// Shard is also optional. If not nil, TSDB will limit the result to
	// the requested shard. If it is nil, TSDB will return all results,
	// regardless of shard.
	// Note: any shard used must be a valid factor of two, meaning `0_of_2` and `3_of_4` are fine, but `0_of_3` is not.
	GetChunkRefs(ctx context.Context, userID string, from, through model.Time, res []ChunkRef, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]ChunkRef, error)
	// Series follows the same semantics regarding the passed slice and shard as GetChunkRefs.
	Series(ctx context.Context, userID string, from, through model.Time, res []Series, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]Series, error)
	LabelNames(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, error)
	LabelValues(ctx context.Context, userID string, from, through model.Time, name string, matchers ...*labels.Matcher) ([]string, error)
}
