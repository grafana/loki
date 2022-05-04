package tsdb

import (
	"context"

	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

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

func (m *MultiTenantIndex) Bounds() (model.Time, model.Time) { return m.idx.Bounds() }

func (m *MultiTenantIndex) Close() error { return m.idx.Close() }

func (m *MultiTenantIndex) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, res []ChunkRef, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]ChunkRef, error) {
	return m.idx.GetChunkRefs(ctx, userID, from, through, res, shard, withTenantLabel(userID, matchers)...)
}

func (m *MultiTenantIndex) Series(ctx context.Context, userID string, from, through model.Time, res []Series, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]Series, error) {
	return m.idx.Series(ctx, userID, from, through, res, shard, withTenantLabel(userID, matchers)...)
}

func (m *MultiTenantIndex) LabelNames(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, error) {
	return m.idx.LabelNames(ctx, userID, from, through, withTenantLabel(userID, matchers)...)
}

func (m *MultiTenantIndex) LabelValues(ctx context.Context, userID string, from, through model.Time, name string, matchers ...*labels.Matcher) ([]string, error) {
	return m.idx.LabelValues(ctx, userID, from, through, name, withTenantLabel(userID, matchers)...)
}
