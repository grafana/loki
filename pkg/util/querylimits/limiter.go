package querylimits

import (
	"context"
	"time"

	"github.com/go-kit/log/level"

	"github.com/grafana/loki/pkg/util/limiter"
	logutil "github.com/grafana/loki/pkg/util/log"
)

type Limiter struct {
	limiter.CombinedLimits
}

func NewLimiter(original limiter.CombinedLimits) *Limiter {
	return &Limiter{
		CombinedLimits: original,
	}
}

// MaxQueryLength returns the limit of the length (in time) of a query.
func (l *Limiter) MaxQueryLength(ctx context.Context, userID string) time.Duration {
	original := l.CombinedLimits.MaxQueryLength(ctx, userID)
	requestLimits := ExtractQueryLimitsContext(ctx)
	if requestLimits == nil || requestLimits.MaxQueryLength == 0 || time.Duration(requestLimits.MaxQueryLength) > original {
		_ = level.Debug(logutil.WithContext(ctx, logutil.Logger)).Log("msg", "using original limit")
		return original
	}
	_ = level.Debug(logutil.WithContext(ctx, logutil.Logger)).Log("msg", "using request limit", "limit", requestLimits.MaxQueryLength)
	return time.Duration(requestLimits.MaxQueryLength)
}

// MaxQueryLookback returns the max lookback period of queries.
func (l *Limiter) MaxQueryLookback(ctx context.Context, userID string) time.Duration {
	original := l.CombinedLimits.MaxQueryLookback(ctx, userID)
	// in theory this error should never happen
	requestLimits := ExtractQueryLimitsContext(ctx)
	if requestLimits == nil || requestLimits.MaxQueryLookback == 0 || time.Duration(requestLimits.MaxQueryLookback) > original {
		_ = level.Debug(logutil.WithContext(ctx, logutil.Logger)).Log("msg", "using original limit")
		return original
	}
	return time.Duration(requestLimits.MaxQueryLookback)
}

// MaxEntriesLimitPerQuery returns the limit to number of entries the querier should return per query.
func (l *Limiter) MaxEntriesLimitPerQuery(ctx context.Context, userID string) int {
	original := l.CombinedLimits.MaxEntriesLimitPerQuery(ctx, userID)
	// in theory this error should never happen
	requestLimits := ExtractQueryLimitsContext(ctx)
	if requestLimits == nil || requestLimits.MaxEntriesLimitPerQuery == 0 || requestLimits.MaxEntriesLimitPerQuery > original {
		_ = level.Debug(logutil.WithContext(ctx, logutil.Logger)).Log("msg", "using original limit")
		return original
	}
	return requestLimits.MaxEntriesLimitPerQuery
}
