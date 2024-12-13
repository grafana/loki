package sharding

import (
	"context"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// General purpose iteration over series. Makes it easier to build custom functionality on top of indices
// of different types without them all implementing the same feature.
// The passed callback must _not_ capture its arguments. They're reused for each call for performance.
// The passed callback may be executed concurrently,
// so any shared state must be protected by the caller.
// NB: This is a low-level API and should be used with caution.
// NB: It's possible for the callback to be called multiple times for the same series but possibly different chunks,
// such as when the Index is backed by multiple files with the same series present.
// NB(owen-d): mainly in this package to avoid circular dependencies elsewhere
type ForSeries interface {
	ForSeries(
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
	) error
}

// function Adapter for ForSeries implementation
type ForSeriesFunc func(
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
) error

func (f ForSeriesFunc) ForSeries(
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
	return f(ctx, userID, fpFilter, from, through, fn, matchers...)
}
