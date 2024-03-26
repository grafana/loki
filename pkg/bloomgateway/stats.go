package bloomgateway

import (
	"context"
	"time"

	"go.uber.org/atomic"
)

type Stats struct {
	Status                                                                         string
	NumTasks, NumFilters                                                           int
	ChunksRequested, ChunksFiltered, SeriesRequested, SeriesFiltered               int
	QueueTime, MetasFetchTime, BlocksFetchTime, ProcessingTime, PostProcessingTime atomic.Duration
}

type statsKey int

var ctxKey = statsKey(0)

// ContextWithEmptyStats returns a context with empty stats.
func ContextWithEmptyStats(ctx context.Context) (*Stats, context.Context) {
	stats := &Stats{Status: "unknown"}
	ctx = context.WithValue(ctx, ctxKey, stats)
	return stats, ctx
}

// FromContext gets the Stats out of the Context. Returns nil if stats have not
// been initialised in the context.
func FromContext(ctx context.Context) *Stats {
	o := ctx.Value(ctxKey)
	if o == nil {
		return nil
	}
	return o.(*Stats)
}

func (s *Stats) KVArgs() []any {
	if s == nil {
		return []any{}
	}
	return []any{
		"status", s.Status,
		"tasks", s.NumTasks,
		"series_requested", s.SeriesRequested,
		"series_filtered", s.SeriesFiltered,
		"chunks_requested", s.ChunksRequested,
		"chunks_filtered", s.ChunksFiltered,
		"queue_time", s.QueueTime.Load(),
		"metas_fetch_time", s.MetasFetchTime.Load(),
		"blocks_fetch_time", s.BlocksFetchTime.Load(),
		"processing_time", s.ProcessingTime.Load(),
		"post_processing_time", s.PostProcessingTime.Load(),
	}
}

func (s *Stats) AddQueueTime(t time.Duration) {
	if s == nil {
		return
	}
	s.QueueTime.Add(t)
}

func (s *Stats) AddMetasFetchTime(t time.Duration) {
	if s == nil {
		return
	}
	s.MetasFetchTime.Add(t)
}

func (s *Stats) AddBlocksFetchTime(t time.Duration) {
	if s == nil {
		return
	}
	s.BlocksFetchTime.Add(t)
}

func (s *Stats) AddProcessingTime(t time.Duration) {
	if s == nil {
		return
	}
	s.ProcessingTime.Add(t)
}

func (s *Stats) AddPostProcessingTime(t time.Duration) {
	if s == nil {
		return
	}
	s.PostProcessingTime.Add(t)
}
