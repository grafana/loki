package querylimits

import (
	"context"
	"fmt"
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
func (l *Limiter) MaxQueryLength(ctx context.Context, userID string) (time.Duration, error) {
	original, err := l.CombinedLimits.MaxQueryLength(ctx, userID)
	// in theory this error should never happen
	if err != nil {
		return 0, err
	}
	requestLimits := ExtractQueryLimitsContext(ctx)
	if requestLimits == nil || requestLimits.MaxQueryLength == 0 {
		_ = level.Debug(logutil.WithContext(ctx, logutil.Logger)).Log("msg", "using original limit")
		return original, nil
	}
	if time.Duration(requestLimits.MaxQueryLength) > original {
		_ = level.Error(logutil.WithContext(ctx, logutil.Logger)).Log("msg", "invalid MaxQueryLength", "tenant", userID, "requestLimit", requestLimits.MaxQueryLength, "tenantLimit", original)
		return 0, fmt.Errorf("invalid MaxQueryLength of %v, tenant limit is %v", requestLimits.MaxQueryLength, original)
	}
	_ = level.Debug(logutil.WithContext(ctx, logutil.Logger)).Log("msg", "using request limit", "limit", requestLimits.MaxQueryLength)
	return time.Duration(requestLimits.MaxQueryLength), nil
}

func (l *Limiter) QueryTimeout(ctx context.Context, userID string) (time.Duration, error) {
	original, err := l.CombinedLimits.QueryTimeout(ctx, userID)
	// in theory this error should never happen
	if err != nil {
		return 0, err
	}
	requestLimits := ExtractQueryLimitsContext(ctx)
	if requestLimits == nil || requestLimits.QueryTimeout == 0 {
		_ = level.Debug(logutil.WithContext(ctx, logutil.Logger)).Log("msg", "using original limit")
		return original, nil
	}
	if time.Duration(requestLimits.QueryTimeout) > original {
		_ = level.Error(logutil.WithContext(ctx, logutil.Logger)).Log("msg", "invalid QueryTimeout", "tenant", userID, "requestLimit", requestLimits.QueryTimeout, "tenantLimit", original)
		return 0, fmt.Errorf("invalid QueryTimeout of %v, tenant limit is %v", requestLimits.QueryTimeout, original)
	}
	return time.Duration(requestLimits.QueryTimeout), nil
}

// MaxQueryLookback returns the max lookback period of queries.
func (l *Limiter) MaxQueryLookback(ctx context.Context, userID string) (time.Duration, error) {
	original, err := l.CombinedLimits.MaxQueryLookback(ctx, userID)
	// in theory this error should never happen
	if err != nil {
		return 0, err
	}
	requestLimits := ExtractQueryLimitsContext(ctx)
	if requestLimits == nil || requestLimits.MaxQueryLookback == 0 {
		_ = level.Debug(logutil.WithContext(ctx, logutil.Logger)).Log("msg", "using original limit")
		return original, nil
	}
	if time.Duration(requestLimits.MaxQueryLookback) > original {
		_ = level.Error(logutil.WithContext(ctx, logutil.Logger)).Log("msg", "invalid MaxQueryLookback", "tenant", userID, "requestLimit", requestLimits.MaxQueryLookback, "tenantLimit", original)
		return 0, fmt.Errorf("invalid MaxQueryLookback of %v, tenant limit is %v", requestLimits.MaxQueryLookback, original)
	}
	return time.Duration(requestLimits.MaxQueryLookback), nil
}

// MaxEntriesLimitPerQuery returns the limit to number of entries the querier should return per query.
func (l *Limiter) MaxEntriesLimitPerQuery(ctx context.Context, userID string) (int, error) {
	fmt.Println("query limiter max entries limit per query")
	original, err := l.CombinedLimits.MaxEntriesLimitPerQuery(ctx, userID)
	// in theory this error should never happen
	if err != nil {
		return 0, err
	}
	requestLimits := ExtractQueryLimitsContext(ctx)
	if requestLimits == nil || requestLimits.MaxEntriesLimitPerQuery == 0 {
		_ = level.Debug(logutil.WithContext(ctx, logutil.Logger)).Log("msg", "using original limit")
		return original, nil
	}

	if requestLimits.MaxEntriesLimitPerQuery > original {
		_ = level.Error(logutil.WithContext(ctx, logutil.Logger)).Log("msg", "invalid MaxEntriesLimitPerQuery", "tenant", userID, "requestLimit", requestLimits.MaxEntriesLimitPerQuery, "tenantLimit", original)
		return 0, fmt.Errorf("invalid MaxEntriesLimitPerQuery of %v, tenant limit is %v", requestLimits.MaxEntriesLimitPerQuery, original)
	}
	return requestLimits.MaxEntriesLimitPerQuery, nil
}
