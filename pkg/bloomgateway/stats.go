package bloomgateway

import (
	"context"
	"sync/atomic"
	"time"
)

type Stats struct {
	Status                              string
	NumTasks, NumMatchers               int
	ChunksRequested, ChunksFiltered     int
	SeriesRequested, SeriesFiltered     int
	QueueTime                           *atomic.Int64
	BlocksFetchTime                     *atomic.Int64
	ProcessingTime, TotalProcessingTime *atomic.Int64
	PostProcessingTime                  *atomic.Int64
	SkippedBlocks                       *atomic.Int32 // blocks skipped because they were not available (yet)
	ProcessedBlocks                     *atomic.Int32 // blocks processed for this specific request
	ProcessedBlocksTotal                *atomic.Int32 // blocks processed for multiplexed request
}

type statsKey int

var ctxKey = statsKey(0)

// ContextWithEmptyStats returns a context with empty stats.
func ContextWithEmptyStats(ctx context.Context) (*Stats, context.Context) {
	stats := &Stats{
		Status:               "unknown",
		SkippedBlocks:        &atomic.Int32{},
		ProcessedBlocks:      &atomic.Int32{},
		ProcessedBlocksTotal: &atomic.Int32{},
		QueueTime:            &atomic.Int64{},
		BlocksFetchTime:      &atomic.Int64{},
		ProcessingTime:       &atomic.Int64{},
		TotalProcessingTime:  &atomic.Int64{},
		PostProcessingTime:   &atomic.Int64{},
	}
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

// aggregates the total duration
func (s *Stats) Duration() (dur time.Duration) {
	dur += time.Duration(s.QueueTime.Load())
	dur += time.Duration(s.BlocksFetchTime.Load())
	dur += time.Duration(s.ProcessingTime.Load())
	dur += time.Duration(s.PostProcessingTime.Load())
	return
}

func (s *Stats) KVArgs() []any {
	if s == nil {
		return []any{}
	}
	chunksRemaining := s.ChunksRequested - s.ChunksFiltered
	filterRatio := float64(s.ChunksFiltered) / float64(max(s.ChunksRequested, 1))

	return []any{
		"msg", "stats-report",
		"status", s.Status,
		"tasks", s.NumTasks,
		"matchers", s.NumMatchers,
		"blocks_skipped", s.SkippedBlocks.Load(),
		"blocks_processed", s.ProcessedBlocks.Load(),
		"blocks_processed_total", s.ProcessedBlocksTotal.Load(),
		"series_requested", s.SeriesRequested,
		"series_filtered", s.SeriesFiltered,
		"chunks_requested", s.ChunksRequested,
		"chunks_filtered", s.ChunksFiltered,
		"chunks_remaining", chunksRemaining,
		"filter_ratio", filterRatio,
		"queue_time", time.Duration(s.QueueTime.Load()),
		"blocks_fetch_time", time.Duration(s.BlocksFetchTime.Load()),
		"processing_time", time.Duration(s.ProcessingTime.Load()),
		"post_processing_time", time.Duration(s.PostProcessingTime.Load()),
		"duration", s.Duration(),
	}
}

func (s *Stats) AddQueueTime(t time.Duration) {
	if s == nil {
		return
	}
	s.QueueTime.Add(int64(t))
}

func (s *Stats) AddBlocksFetchTime(t time.Duration) {
	if s == nil {
		return
	}
	s.BlocksFetchTime.Add(int64(t))
}

func (s *Stats) AddProcessingTime(t time.Duration) {
	if s == nil {
		return
	}
	s.ProcessingTime.Add(int64(t))
}

func (s *Stats) AddTotalProcessingTime(t time.Duration) {
	if s == nil {
		return
	}
	s.TotalProcessingTime.Add(int64(t))
}

func (s *Stats) AddPostProcessingTime(t time.Duration) {
	if s == nil {
		return
	}
	s.PostProcessingTime.Add(int64(t))
}

func (s *Stats) IncSkippedBlocks() {
	if s == nil {
		return
	}
	s.SkippedBlocks.Add(1)
}

func (s *Stats) IncProcessedBlocks() {
	if s == nil {
		return
	}
	s.ProcessedBlocks.Add(1)
}

func (s *Stats) AddProcessedBlocksTotal(delta int) {
	if s == nil {
		return
	}
	s.ProcessedBlocksTotal.Add(int32(delta))
}
