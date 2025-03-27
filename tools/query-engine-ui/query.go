package main

import (
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
)

// query implements the logql.Params interface for the logical planner
type query struct {
	expr      syntax.Expr
	start     time.Time
	end       time.Time
	step      time.Duration
	direction logproto.Direction
	limit     uint32
}

// CachingOptions implements logql.Params.
func (p *query) CachingOptions() resultscache.CachingOptions {
	panic("unimplemented")
}

// GetStoreChunks implements logql.Params.
func (p *query) GetStoreChunks() *logproto.ChunkRefGroup {
	panic("unimplemented")
}

// Interval implements logql.Params.
func (p *query) Interval() time.Duration {
	panic("unimplemented")
}

// Shards implements logql.Params.
func (p *query) Shards() []string {
	panic("unimplemented")
}

func (p *query) GetExpression() syntax.Expr {
	return p.expr
}

func (p *query) Start() time.Time {
	return p.start
}

func (p *query) End() time.Time {
	return p.end
}

func (p *query) Step() time.Duration {
	return p.step
}

func (p *query) Direction() logproto.Direction {
	return p.direction
}

func (p *query) Limit() uint32 {
	return p.limit
}

func (p *query) QueryString() string {
	return p.expr.String()
}

func (p *query) GetShards() []string {
	return nil
}

func (p *query) GetDeletes() []*logproto.Delete {
	return nil
}
