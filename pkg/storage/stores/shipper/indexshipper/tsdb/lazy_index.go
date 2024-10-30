package tsdb

import (
	"context"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// Index adapter for a function which returns an index when queried.
type LazyIndex func() (Index, error)

func (f LazyIndex) Bounds() (model.Time, model.Time) {
	i, err := f()
	if err != nil {
		return 0, 0
	}
	return i.Bounds()
}

func (f LazyIndex) SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer) {
	i, err := f()
	if err == nil {
		i.SetChunkFilterer(chunkFilter)
	}
}

func (f LazyIndex) Close() error {
	i, err := f()
	if err != nil {
		return err
	}
	return i.Close()
}

func (f LazyIndex) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, res []ChunkRef, fpFilter index.FingerprintFilter, matchers ...*labels.Matcher) ([]ChunkRef, error) {
	i, err := f()
	if err != nil {
		return nil, err
	}
	return i.GetChunkRefs(ctx, userID, from, through, res, fpFilter, matchers...)
}
func (f LazyIndex) Series(ctx context.Context, userID string, from, through model.Time, res []Series, fpFilter index.FingerprintFilter, matchers ...*labels.Matcher) ([]Series, error) {
	i, err := f()
	if err != nil {
		return nil, err
	}
	return i.Series(ctx, userID, from, through, res, fpFilter, matchers...)
}
func (f LazyIndex) LabelNames(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, error) {
	i, err := f()
	if err != nil {
		return nil, err
	}
	return i.LabelNames(ctx, userID, from, through, matchers...)
}
func (f LazyIndex) LabelValues(ctx context.Context, userID string, from, through model.Time, name string, matchers ...*labels.Matcher) ([]string, error) {
	i, err := f()
	if err != nil {
		return nil, err
	}
	return i.LabelValues(ctx, userID, from, through, name, matchers...)
}

func (f LazyIndex) Stats(ctx context.Context, userID string, from, through model.Time, acc IndexStatsAccumulator, fpFilter index.FingerprintFilter, shouldIncludeChunk shouldIncludeChunk, matchers ...*labels.Matcher) error {
	i, err := f()
	if err != nil {
		return err
	}
	return i.Stats(ctx, userID, from, through, acc, fpFilter, shouldIncludeChunk, matchers...)
}

func (f LazyIndex) Volume(ctx context.Context, userID string, from, through model.Time, acc VolumeAccumulator, fpFilter index.FingerprintFilter, shouldIncludeChunk shouldIncludeChunk, targetLabels []string, aggregateBy string, matchers ...*labels.Matcher) error {
	i, err := f()
	if err != nil {
		return err
	}
	return i.Volume(ctx, userID, from, through, acc, fpFilter, shouldIncludeChunk, targetLabels, aggregateBy, matchers...)
}

func (f LazyIndex) ForSeries(ctx context.Context, userID string, fpFilter index.FingerprintFilter, from model.Time, through model.Time, fn func(labels.Labels, model.Fingerprint, []index.ChunkMeta) (stop bool), matchers ...*labels.Matcher) error {
	i, err := f()
	if err != nil {
		return err
	}
	return i.ForSeries(ctx, userID, fpFilter, from, through, fn, matchers...)
}
