package tsdb

import (
	"context"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

type Series struct {
	Labels labels.Labels
}

type ChunkRef struct {
	User        string
	Fingerprint model.Fingerprint
	Start, End  model.Time
	Checksum    uint32
}

type Interface interface {
	GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]ChunkRef, error)
	Series(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]Series, error)
	LabelNames(ctx context.Context, userID string, from, through model.Time) ([]string, error)
	LabelValues(ctx context.Context, userID string, from, through model.Time, name string) ([]string, error)
}
