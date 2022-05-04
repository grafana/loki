package tsdb

import (
	"context"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
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

func withTenantLabel(userID string, matchers []*labels.Matcher) []*labels.Matcher {
	cpy := make([]*labels.Matcher, len(matchers))
	copy(cpy, matchers)
	cpy = append(cpy, labels.MustNewMatcher(labels.MatchEqual, TenantLabel, userID))
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

func (m *MultiTenantIndex) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, res []ChunkRef, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]ChunkRef, error) {
	return m.idx.GetChunkRefs(ctx, userID, from, through, res, shard, withTenantLabel(userID, matchers)...)
}

func (m *MultiTenantIndex) Series(ctx context.Context, userID string, from, through model.Time, res []Series, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]Series, error) {
	xs, err := m.idx.Series(ctx, userID, from, through, res, shard, withTenantLabel(userID, matchers)...)
	if err != nil {
		return nil, err
	}
	for i := range xs {
		xs[i].Labels = withoutTenantLabel(xs[i].Labels)
	}
	return xs, nil
}

func (m *MultiTenantIndex) LabelNames(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, error) {
	return m.idx.LabelNames(ctx, userID, from, through, withTenantLabel(userID, matchers)...)
}

func (m *MultiTenantIndex) LabelValues(ctx context.Context, userID string, from, through model.Time, name string, matchers ...*labels.Matcher) ([]string, error) {
	return m.idx.LabelValues(ctx, userID, from, through, name, withTenantLabel(userID, matchers)...)
}
