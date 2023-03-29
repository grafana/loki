package querylimits

import (
	"context"
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
func (l *Limiter) MaxQueryLength(ctx context.Context, userID string) time.Duration {
	original := l.CombinedLimits.MaxQueryLength(ctx, userID)
	requestLimits := ExtractQueryLimitsContext(ctx)
	if requestLimits == nil || requestLimits.MaxQueryLength == 0 || time.Duration(requestLimits.MaxQueryLength) > original {
		level.Debug(logutil.WithContext(ctx, l.logger)).Log("msg", "using original limit")
		return original
	}
	level.Debug(logutil.WithContext(ctx, l.logger)).Log("msg", "using request limit", "limit", requestLimits.MaxQueryLength)
	return time.Duration(requestLimits.MaxQueryLength)
}

// MaxQueryLookback returns the max lookback period of queries.
func (l *Limiter) MaxQueryLookback(ctx context.Context, userID string) time.Duration {
	original := l.CombinedLimits.MaxQueryLookback(ctx, userID)
	requestLimits := ExtractQueryLimitsContext(ctx)
	if requestLimits == nil || requestLimits.MaxQueryLookback == 0 || time.Duration(requestLimits.MaxQueryLookback) > original {
		level.Debug(logutil.WithContext(ctx, l.logger)).Log("msg", "using original limit")
		return original
	}
	return time.Duration(requestLimits.MaxQueryLookback)
}

// MaxEntriesLimitPerQuery returns the limit to number of entries the querier should return per query.
func (l *Limiter) MaxEntriesLimitPerQuery(ctx context.Context, userID string) int {
	original := l.CombinedLimits.MaxEntriesLimitPerQuery(ctx, userID)
	requestLimits := ExtractQueryLimitsContext(ctx)
	if requestLimits == nil || requestLimits.MaxEntriesLimitPerQuery == 0 || requestLimits.MaxEntriesLimitPerQuery > original {
		level.Debug(logutil.WithContext(ctx, l.logger)).Log("msg", "using original limit")
		return original
	}
	return requestLimits.MaxEntriesLimitPerQuery
}

func (l *Limiter) QueryTimeout(ctx context.Context, userID string) time.Duration {
	original := l.CombinedLimits.QueryTimeout(ctx, userID)
	// in theory this error should never happen
	requestLimits := ExtractQueryLimitsContext(ctx)
	if requestLimits == nil || requestLimits.QueryTimeout == 0 || time.Duration(requestLimits.QueryTimeout) > original {
		level.Debug(logutil.WithContext(ctx, l.logger)).Log("msg", "using original limit")
		return original
	}
	return time.Duration(requestLimits.QueryTimeout)
}

func (l *Limiter) RequiredLabels(ctx context.Context, userID string) []string {
	original := l.CombinedLimits.RequiredLabels(ctx, userID)
	requestLimits := ExtractQueryLimitsContext(ctx)

	if requestLimits == nil {
		return original
	}

	// The most restricting is a union of both slices
	unionMap := make(map[string]struct{})
	for _, label := range original {
		unionMap[label] = struct{}{}
	}

	for _, label := range requestLimits.RequiredLabels {
		unionMap[label] = struct{}{}
	}

	union := make([]string, 0, len(unionMap))
	for label := range unionMap {
		union = append(union, label)
	}
	return union
}

func (l *Limiter) RequiredNumberLabels(ctx context.Context, userID string) int {
	original := l.CombinedLimits.RequiredNumberLabels(ctx, userID)
	requestLimits := ExtractQueryLimitsContext(ctx)
	if requestLimits == nil || requestLimits.RequiredNumberLabels == 0 || requestLimits.RequiredNumberLabels < original {
		level.Debug(logutil.WithContext(ctx, l.logger)).Log("msg", "using original limit")
		return original
	}
	return requestLimits.RequiredNumberLabels
}

func (l *Limiter) MaxQueryBytesRead(ctx context.Context, userID string) int {
	original := l.CombinedLimits.MaxQueryBytesRead(ctx, userID)
	requestLimits := ExtractQueryLimitsContext(ctx)
	if requestLimits == nil || requestLimits.MaxQueryBytesRead.Val() == 0 || requestLimits.MaxQueryBytesRead.Val() > original {
		level.Debug(logutil.WithContext(ctx, l.logger)).Log("msg", "using original limit")
		return original
	}
	return requestLimits.MaxQueryBytesRead.Val()
}
