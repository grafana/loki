package bloomgateway

import (
	"context"
	"time"

	"go.uber.org/atomic"
)

type Stats struct {
	Status                              string
	NumTasks, NumMatchers               int
	ChunksRequested, ChunksFiltered     int
	SeriesRequested, SeriesFiltered     int
	QueueTime                           *atomic.Duration
	BlocksFetchTime                     *atomic.Duration
	ProcessingTime, TotalProcessingTime *atomic.Duration
	PostProcessingTime                  *atomic.Duration
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
		SkippedBlocks:        atomic.NewInt32(0),
		ProcessedBlocks:      atomic.NewInt32(0),
		ProcessedBlocksTotal: atomic.NewInt32(0),
		QueueTime:            atomic.NewDuration(0),
		BlocksFetchTime:      atomic.NewDuration(0),
		ProcessingTime:       atomic.NewDuration(0),
		TotalProcessingTime:  atomic.NewDuration(0),
		PostProcessingTime:   atomic.NewDuration(0),
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
	dur += s.QueueTime.Load()
	dur += s.BlocksFetchTime.Load()
	dur += s.ProcessingTime.Load()
	dur += s.PostProcessingTime.Load()
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
		"queue_time", s.QueueTime.Load(),
		"blocks_fetch_time", s.BlocksFetchTime.Load(),
		"processing_time", s.ProcessingTime.Load(),
		"post_processing_time", s.PostProcessingTime.Load(),
		"duration", s.Duration(),
	}
}

func (s *Stats) AddQueueTime(t time.Duration) {
	if s == nil {
		return
	}
	s.QueueTime.Add(t)
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

func (s *Stats) AddTotalProcessingTime(t time.Duration) {
	if s == nil {
		return
	}
	s.TotalProcessingTime.Add(t)
}

func (s *Stats) AddPostProcessingTime(t time.Duration) {
	if s == nil {
		return
	}
	s.PostProcessingTime.Add(t)
}

func (s *Stats) IncSkippedBlocks() {
	if s == nil {
		return
	}
	s.SkippedBlocks.Inc()
}

func (s *Stats) IncProcessedBlocks() {
	if s == nil {
		return
	}
	s.ProcessedBlocks.Inc()
}

func (s *Stats) AddProcessedBlocksTotal(delta int) {
	if s == nil {
		return
	}
	s.ProcessedBlocksTotal.Add(int32(delta))
}
