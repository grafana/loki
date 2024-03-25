package bloomgateway

import (
	"context"
	"sync/atomic" //lint:ignore faillint we can't use go.uber.org/atomic with a protobuf struct without wrapping it.
	"time"
)

type Stats struct {
	QueueTime, FetchTime, ProcessingTime, PostProcessingTime time.Duration
}

type statsKey int

var ctxKey = statsKey(0)

// ContextWithEmptyStats returns a context with empty stats.
func ContextWithEmptyStats(ctx context.Context) (*Stats, context.Context) {
	stats := &Stats{}
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
		"queue_time", s.QueueTime,
		"fetch_time", s.FetchTime,
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

func (s *Stats) AddFetchTime(t time.Duration) {
	if s == nil {
		return
	}
	atomic.AddInt64((*int64)(&s.FetchTime), int64(t))
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
