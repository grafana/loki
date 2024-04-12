package tsdb

import (
	"context"
	"sort"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// TenantLabel is part of the reserved label namespace (__ prefix)
// It's used to create multi-tenant TSDBs (which do not have a tenancy concept)
// These labels are stripped out during compaction to single-tenant TSDBs
const TenantLabel = "__loki_tenant__"

// MultiTenantIndex will inject a tenant label to it's queries
// This works with pre-compacted TSDBs which aren't yet per tenant.
type MultiTenantIndex struct {
	idx Index
}

func NewMultiTenantIndex(idx Index) *MultiTenantIndex {
	return &MultiTenantIndex{idx: idx}
}

func withTenantLabelMatcher(userID string, matchers []*labels.Matcher) []*labels.Matcher {
	cpy := make([]*labels.Matcher, len(matchers)+1)
	cpy[0] = labels.MustNewMatcher(labels.MatchEqual, TenantLabel, userID)
	copy(cpy[1:], matchers)
	return cpy
}

func withoutTenantLabel(ls labels.Labels) labels.Labels {
	for i, l := range ls {
		if l.Name == TenantLabel {
			ls = append(ls[:i], ls[i+1:]...)
			break
		}
	}
	return ls
}

func (m *MultiTenantIndex) Bounds() (model.Time, model.Time) { return m.idx.Bounds() }

func (m *MultiTenantIndex) SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer) {
	m.idx.SetChunkFilterer(chunkFilter)
}

func (m *MultiTenantIndex) Close() error { return m.idx.Close() }

func (m *MultiTenantIndex) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, res []ChunkRef, fpFilter index.FingerprintFilter, matchers ...*labels.Matcher) ([]ChunkRef, error) {
	return m.idx.GetChunkRefs(ctx, userID, from, through, res, fpFilter, withTenantLabelMatcher(userID, matchers)...)
}

func (m *MultiTenantIndex) Series(ctx context.Context, userID string, from, through model.Time, res []Series, fpFilter index.FingerprintFilter, matchers ...*labels.Matcher) ([]Series, error) {
	xs, err := m.idx.Series(ctx, userID, from, through, res, fpFilter, withTenantLabelMatcher(userID, matchers)...)
	if err != nil {
		return nil, err
	}
	for i := range xs {
		xs[i].Labels = withoutTenantLabel(xs[i].Labels)
	}
	return xs, nil
}

func (m *MultiTenantIndex) LabelNames(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, error) {
	res, err := m.idx.LabelNames(ctx, userID, from, through, withTenantLabelMatcher(userID, matchers)...)
	if err != nil {
		return nil, err
	}

	// Strip out the tenant label in response.
	i := sort.SearchStrings(res, TenantLabel)
	if i == len(res) || res[i] != TenantLabel {
		return res, nil
	}

	return append(res[:i], res[i+1:]...), nil
}

func (m *MultiTenantIndex) LabelValues(ctx context.Context, userID string, from, through model.Time, name string, matchers ...*labels.Matcher) ([]string, error) {
	// Prevent queries for the internal tenant label
	if name == TenantLabel {
		return nil, nil
	}
	return m.idx.LabelValues(ctx, userID, from, through, name, withTenantLabelMatcher(userID, matchers)...)
}

func (m *MultiTenantIndex) Stats(ctx context.Context, userID string, from, through model.Time, acc IndexStatsAccumulator, fpFilter index.FingerprintFilter, shouldIncludeChunk shouldIncludeChunk, matchers ...*labels.Matcher) error {
	return m.idx.Stats(ctx, userID, from, through, acc, fpFilter, shouldIncludeChunk, withTenantLabelMatcher(userID, matchers)...)
}

func (m *MultiTenantIndex) Volume(ctx context.Context, userID string, from, through model.Time, acc VolumeAccumulator, fpFilter index.FingerprintFilter, shouldIncludeChunk shouldIncludeChunk, targetLabels []string, aggregateBy string, matchers ...*labels.Matcher) error {
	return m.idx.Volume(ctx, userID, from, through, acc, fpFilter, shouldIncludeChunk, targetLabels, aggregateBy, withTenantLabelMatcher(userID, matchers)...)
}

func (m *MultiTenantIndex) ForSeries(ctx context.Context, userID string, fpFilter index.FingerprintFilter, from, through model.Time, fn func(labels.Labels, model.Fingerprint, []index.ChunkMeta) (stop bool), matchers ...*labels.Matcher) error {
	return m.idx.ForSeries(ctx, userID, fpFilter, from, through, fn, withTenantLabelMatcher(userID, matchers)...)
}
