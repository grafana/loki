package tsdb

import (
	"context"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
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

// Compares by (Fp, Start, End, checksum)
// Assumes User is equivalent
func (r ChunkRef) Less(x ChunkRef) bool {
	if r.Fingerprint != x.Fingerprint {
		return r.Fingerprint < x.Fingerprint
	}

	if r.Start != x.Start {
		return r.Start < x.Start
	}

	if r.End != x.End {
		return r.End < x.End
	}

	return r.Checksum < x.Checksum
}

type shouldIncludeChunk func(index.ChunkMeta) bool

type Index interface {
	Bounded
	SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer)
	Close() error
	sharding.ForSeries
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
	GetChunkRefs(ctx context.Context, userID string, from, through model.Time, res []ChunkRef, fpFilter index.FingerprintFilter, matchers ...*labels.Matcher) ([]ChunkRef, error)
	// Series follows the same semantics regarding the passed slice and shard as GetChunkRefs.
	Series(ctx context.Context, userID string, from, through model.Time, res []Series, fpFilter index.FingerprintFilter, matchers ...*labels.Matcher) ([]Series, error)
	LabelNames(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, error)
	LabelValues(ctx context.Context, userID string, from, through model.Time, name string, matchers ...*labels.Matcher) ([]string, error)
	Stats(ctx context.Context, userID string, from, through model.Time, acc IndexStatsAccumulator, fpFilter index.FingerprintFilter, shouldIncludeChunk shouldIncludeChunk, matchers ...*labels.Matcher) error
	Volume(ctx context.Context, userID string, from, through model.Time, acc VolumeAccumulator, fpFilter index.FingerprintFilter, shouldIncludeChunk shouldIncludeChunk, targetLabels []string, aggregateBy string, matchers ...*labels.Matcher) error
}

type NoopIndex struct{}

func (NoopIndex) Close() error                    { return nil }
func (NoopIndex) Bounds() (_, through model.Time) { return }
func (NoopIndex) GetChunkRefs(_ context.Context, _ string, _, _ model.Time, _ []ChunkRef, _ index.FingerprintFilter, _ ...*labels.Matcher) ([]ChunkRef, error) {
	return nil, nil
}

// Series follows the same semantics regarding the passed slice and shard as GetChunkRefs.
func (NoopIndex) Series(_ context.Context, _ string, _, _ model.Time, _ []Series, _ index.FingerprintFilter, _ ...*labels.Matcher) ([]Series, error) {
	return nil, nil
}
func (NoopIndex) LabelNames(_ context.Context, _ string, _, _ model.Time, _ ...*labels.Matcher) ([]string, error) {
	return nil, nil
}
func (NoopIndex) LabelValues(_ context.Context, _ string, _, _ model.Time, _ string, _ ...*labels.Matcher) ([]string, error) {
	return nil, nil
}

func (NoopIndex) Stats(_ context.Context, _ string, _, _ model.Time, _ IndexStatsAccumulator, _ index.FingerprintFilter, _ shouldIncludeChunk, _ ...*labels.Matcher) error {
	return nil
}

func (NoopIndex) SetChunkFilterer(_ chunk.RequestChunkFilterer) {}

func (NoopIndex) Volume(_ context.Context, _ string, _, _ model.Time, _ VolumeAccumulator, _ index.FingerprintFilter, _ shouldIncludeChunk, _ []string, _ string, _ ...*labels.Matcher) error {
	return nil
}

func (NoopIndex) ForSeries(_ context.Context, _ string, _ index.FingerprintFilter, _ model.Time, _ model.Time, _ func(labels.Labels, model.Fingerprint, []index.ChunkMeta) (stop bool), _ ...*labels.Matcher) error {
	return nil
}
