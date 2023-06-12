package tsdb

import (
	"context"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
)

type Series struct {
	Labels      labels.Labels
	Fingerprint model.Fingerprint
}

type ChunkRef struct {
	User        string
	Fingerprint model.Fingerprint
	Start, End  model.Time
	Checksum    uint32
}

type IndexCfg struct {
	indexshipper.Config

	CachePostings bool `yaml:"cache_postings"`
}

// Compares by (Start, End)
// Assumes User is equivalent
func (r ChunkRef) Less(x ChunkRef) bool {
	if r.Start != x.Start {
		return r.Start < x.Start
	}
	return r.End <= x.End
}

type shouldIncludeChunk func(index.ChunkMeta) bool

type Index interface {
	Bounded
	SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer)
	Close() error
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
	Stats(ctx context.Context, userID string, from, through model.Time, acc IndexStatsAccumulator, shard *index.ShardAnnotation, shouldIncludeChunk shouldIncludeChunk, matchers ...*labels.Matcher) error
	LabelVolume(ctx context.Context, userID string, from, through model.Time, acc LabelVolumeAccumulator, shard *index.ShardAnnotation, shouldIncludeChunk shouldIncludeChunk, matchers ...*labels.Matcher) error
}

type NoopIndex struct{}

func (NoopIndex) Close() error                    { return nil }
func (NoopIndex) Bounds() (_, through model.Time) { return }
func (NoopIndex) GetChunkRefs(_ context.Context, _ string, _, _ model.Time, _ []ChunkRef, _ *index.ShardAnnotation, _ ...*labels.Matcher) ([]ChunkRef, error) {
	return nil, nil
}

// Series follows the same semantics regarding the passed slice and shard as GetChunkRefs.
func (NoopIndex) Series(_ context.Context, _ string, _, _ model.Time, _ []Series, _ *index.ShardAnnotation, _ ...*labels.Matcher) ([]Series, error) {
	return nil, nil
}
func (NoopIndex) LabelNames(_ context.Context, _ string, _, _ model.Time, _ ...*labels.Matcher) ([]string, error) {
	return nil, nil
}
func (NoopIndex) LabelValues(_ context.Context, _ string, _, _ model.Time, _ string, _ ...*labels.Matcher) ([]string, error) {
	return nil, nil
}

func (NoopIndex) Stats(_ context.Context, _ string, _, _ model.Time, _ IndexStatsAccumulator, _ *index.ShardAnnotation, _ shouldIncludeChunk, _ ...*labels.Matcher) error {
	return nil
}

func (NoopIndex) SetChunkFilterer(_ chunk.RequestChunkFilterer) {}

func (NoopIndex) LabelVolume(_ context.Context, _ string, _, _ model.Time, _ LabelVolumeAccumulator, _ *index.ShardAnnotation, _ shouldIncludeChunk, _ ...*labels.Matcher) error {
	return nil
}
