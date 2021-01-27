package stats

import (
	"context"
	"sync/atomic" //lint:ignore faillint we can't use go.uber.org/atomic with a protobuf struct without wrapping it.
	"time"

	"github.com/weaveworks/common/httpgrpc"
)

type contextKey int

var ctxKey = contextKey(0)

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

// IsEnabled returns whether stats tracking is enabled in the context.
func IsEnabled(ctx context.Context) bool {
	// When query statistics are enabled, the stats object is already initialised
	// within the context, so we can just check it.
	return FromContext(ctx) != nil
}

// AddWallTime adds some time to the counter.
func (s *Stats) AddWallTime(t time.Duration) {
	if s == nil {
		return
	}

	atomic.AddInt64((*int64)(&s.WallTime), int64(t))
}

// LoadWallTime returns current wall time.
func (s *Stats) LoadWallTime() time.Duration {
	if s == nil {
		return 0
	}

	return time.Duration(atomic.LoadInt64((*int64)(&s.WallTime)))
}

// Merge the provide Stats into this one.
func (s *Stats) Merge(other *Stats) {
	if s == nil || other == nil {
		return
	}

	s.AddWallTime(other.LoadWallTime())
}

func ShouldTrackHTTPGRPCResponse(r *httpgrpc.HTTPResponse) bool {
	// Do no track statistics for requests failed because of a server error.
	return r.Code < 500
}
