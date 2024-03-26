package bloomgateway

import (
	"context"
	"sync/atomic" //lint:ignore
	"time"
)

type Stats struct {
	Status                                                                         string
	NumTasks, NumFilters                                                           int
	ChunksRequested, ChunksFiltered, SeriesRequested, SeriesFiltered               int
	QueueTime, MetasFetchTime, BlocksFetchTime, ProcessingTime, PostProcessingTime time.Duration
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
		"queue_time", s.QueueTime,
		"metas_fetch_time", s.MetasFetchTime,
		"blocks_fetch_time", s.BlocksFetchTime,
		"processing_time", s.ProcessingTime,
		"post_processing_time", s.PostProcessingTime,
	}
}

func (s *Stats) AddQueueTime(t time.Duration) {
	if s == nil {
		return
	}
	atomic.AddInt64((*int64)(&s.QueueTime), int64(t))
}

func (s *Stats) AddMetasFetchTime(t time.Duration) {
	if s == nil {
		return
	}
	atomic.AddInt64((*int64)(&s.MetasFetchTime), int64(t))
}

func (s *Stats) AddBlocksFetchTime(t time.Duration) {
	if s == nil {
		return
	}
	atomic.AddInt64((*int64)(&s.BlocksFetchTime), int64(t))
}

func (s *Stats) AddProcessingTime(t time.Duration) {
	if s == nil {
		return
	}
	atomic.AddInt64((*int64)(&s.ProcessingTime), int64(t))
}

func (s *Stats) AddPostProcessingTime(t time.Duration) {
	if s == nil {
		return
	}
	atomic.AddInt64((*int64)(&s.PostProcessingTime), int64(t))
}
