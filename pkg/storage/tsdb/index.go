package tsdb

import (
	"context"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/querier/astmapper"
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

// Compares by (Start, End)
// Assumes User is equivalent
func (r ChunkRef) Less(x ChunkRef) bool {
	if r.Start != x.Start {
		return r.Start < x.Start
	}
	return r.End <= x.End
}

type Index interface {
	Bounded
	GetChunkRefs(ctx context.Context, userID string, from, through model.Time, shard *astmapper.ShardAnnotation, matchers ...*labels.Matcher) ([]ChunkRef, error)
	Series(ctx context.Context, userID string, from, through model.Time, shard *astmapper.ShardAnnotation, matchers ...*labels.Matcher) ([]Series, error)
	LabelNames(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, error)
	LabelValues(ctx context.Context, userID string, from, through model.Time, name string, matchers ...*labels.Matcher) ([]string, error)
}
