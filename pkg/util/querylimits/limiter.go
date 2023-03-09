package querylimits

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/pkg/util/limiter"
	logutil "github.com/grafana/loki/pkg/util/log"
)

type Limiter struct {
	logger log.Logger
	limiter.CombinedLimits
}

func NewLimiter(log log.Logger, original limiter.CombinedLimits) *Limiter {
	return &Limiter{
		logger:         log,
		CombinedLimits: original,
	}
}

// MaxQueryLength returns the limit of the length (in time) of a query.
func (l *Limiter) MaxQueryLength(ctx context.Context, tenantID string) time.Duration {
	original := l.CombinedLimits.MaxQueryLength(ctx, tenantID)
	requestLimits := ExtractQueryLimitsContext(ctx)
	if requestLimits == nil || requestLimits.MaxQueryLength == nil || time.Duration(*requestLimits.MaxQueryLength) > original {
		_ = level.Debug(logutil.WithContext(ctx, l.logger)).Log("msg", "using original limit")
		return original
	}
	_ = level.Debug(logutil.WithContext(ctx, l.logger)).Log("msg", "using request limit", "limit", requestLimits.MaxQueryLength)
	return time.Duration(*requestLimits.MaxQueryLength)
}

// MaxQueryLookback returns the max lookback period of queries.
func (l *Limiter) MaxQueryLookback(ctx context.Context, tenantID string) time.Duration {
	original := l.CombinedLimits.MaxQueryLookback(ctx, tenantID)
	requestLimits := ExtractQueryLimitsContext(ctx)
	fmt.Println("request limits")
	if requestLimits == nil || requestLimits.MaxQueryLookback == nil || time.Duration(*requestLimits.MaxQueryLookback) > original {
		_ = level.Debug(logutil.WithContext(ctx, l.logger)).Log("msg", "using original limit")
		return original
	}
	return time.Duration(*requestLimits.MaxQueryLookback)
}

// MaxEntriesLimitPerQuery returns the limit to number of entries the querier should return per query.
func (l *Limiter) MaxEntriesLimitPerQuery(ctx context.Context, tenantID string) int {
	original := l.CombinedLimits.MaxEntriesLimitPerQuery(ctx, tenantID)
	requestLimits := ExtractQueryLimitsContext(ctx)
	if requestLimits == nil || requestLimits.MaxEntriesLimitPerQuery == nil || *requestLimits.MaxEntriesLimitPerQuery > original {
		_ = level.Debug(logutil.WithContext(ctx, l.logger)).Log("msg", "using original limit")
		return original
	}
	return *requestLimits.MaxEntriesLimitPerQuery
}

func (l *Limiter) QueryTimeout(ctx context.Context, tenantID string) time.Duration {
	original := l.CombinedLimits.QueryTimeout(ctx, tenantID)
	// in theory this error should never happen
	requestLimits := ExtractQueryLimitsContext(ctx)
	fmt.Println("requestlimts: ", requestLimits)

	if requestLimits == nil || requestLimits.QueryTimeout == nil || time.Duration(*requestLimits.QueryTimeout) > original {
		_ = level.Debug(logutil.WithContext(ctx, l.logger)).Log("msg", "using original limit")
		return original
	}
	return time.Duration(*requestLimits.QueryTimeout)
}
