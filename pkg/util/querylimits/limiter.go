package querylimits

import (
	"context"
	"fmt"
	"net/http"
	"strings"
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

// ValidateQueryLimits returns an error with any limits from the query limits struct that
// would exceed the tenants limits for userID.
// This function needs to be updated for any fileds that are added to the QueryLimits struct in
// this package.
func (l *Limiter) ValidateQueryLimits(r *http.Request, userID string) error {
	var isErr bool
	var err []byte
	err = append(err, []byte("the following limits from the query limits struct violate your tenants limits; ")...)
	requestLimits, err2 := ExtractQueryLimitsHTTP(r)
	if err2 != nil {
		return err2
	}
	if requestLimits == nil {
		fmt.Println("request limits were nil")
		return nil
	}

	origQueryLength := l.CombinedLimits.MaxQueryLength(context.TODO(), userID)
	if time.Duration(requestLimits.MaxQueryLength) > origQueryLength {
		isErr = true
		err = append(err, []byte(fmt.Sprintf("MaxQueryLength of %d is greater than %d, ", requestLimits.MaxQueryLength, origQueryLength))...)
	}

	origQueryRange := l.CombinedLimits.MaxQueryRange(context.TODO(), userID)
	if time.Duration(requestLimits.MaxQueryRange) > origQueryRange {
		isErr = true
		err = append(err, []byte(fmt.Sprintf("MaxQueryRange of %d is greater than %d, ", requestLimits.MaxQueryRange, origQueryRange))...)
	}

	origQueryLookback := l.CombinedLimits.MaxQueryLookback(context.TODO(), userID)
	if time.Duration(requestLimits.MaxQueryLookback) > origQueryLookback {
		isErr = true
		err = append(err, []byte(fmt.Sprintf("MaxQueryLookback of %d is greater than %d, ", requestLimits.MaxQueryLookback, origQueryLookback))...)
	}

	origEntriesLimit := l.CombinedLimits.MaxEntriesLimitPerQuery(context.TODO(), userID)
	if requestLimits.MaxEntriesLimitPerQuery > origEntriesLimit {
		isErr = true
		err = append(err, []byte(fmt.Sprintf("MaxEntriesLimitPerQuery of %d is greater than %d, ", requestLimits.MaxEntriesLimitPerQuery, origEntriesLimit))...)
	}

	origQueryTimeout := l.CombinedLimits.QueryTimeout(context.TODO(), userID)
	if time.Duration(requestLimits.QueryTimeout) > origQueryTimeout {
		isErr = true
		err = append(err, []byte(fmt.Sprintf("QueryTimeout of %d is greater than %d, ", requestLimits.QueryTimeout, origQueryTimeout))...)
	}

	// don't need to check required labels, we union the two sets rather than choosing one or the other

	origRequiredNumberLabels := l.CombinedLimits.RequiredNumberLabels(context.TODO(), userID)
	if requestLimits.RequiredNumberLabels > origRequiredNumberLabels {
		isErr = true
		err = append(err, []byte(fmt.Sprintf("RequiredNumberLabels of %d is greater than %d, ", requestLimits.RequiredNumberLabels, origRequiredNumberLabels))...)
	}

	origMaxQueryBytesRead := l.CombinedLimits.MaxQueryBytesRead(context.TODO(), userID)
	if requestLimits.MaxQueryBytesRead.Val() > origMaxQueryBytesRead {
		isErr = true
		err = append(err, []byte(fmt.Sprintf("MaxQueryBytesRead of %d is greater than %d, ", requestLimits.MaxQueryBytesRead, origMaxQueryBytesRead))...)
	}

	if isErr {
		return fmt.Errorf(string(err))
	}
	return nil
}

// MaxQueryLength returns the limit of the length (in time) of a query.
func (l *Limiter) MaxQueryLength(ctx context.Context, userID string) time.Duration {
	original := l.CombinedLimits.MaxQueryLength(ctx, userID)
	requestLimits := ExtractQueryLimitsContext(ctx)
	if requestLimits == nil || requestLimits.MaxQueryLength == 0 || time.Duration(requestLimits.MaxQueryLength) > original {
		return original
	}
	level.Debug(logutil.WithContext(ctx, l.logger)).Log("msg", "using request limit", "limit", "MaxQueryLength", "tenant", userID, "query-limit", requestLimits.MaxQueryLength, "original-limit", original)
	return time.Duration(requestLimits.MaxQueryLength)
}

// MaxQueryLookback returns the max lookback period of queries.
func (l *Limiter) MaxQueryLookback(ctx context.Context, userID string) time.Duration {
	original := l.CombinedLimits.MaxQueryLookback(ctx, userID)
	requestLimits := ExtractQueryLimitsContext(ctx)
	if requestLimits == nil || requestLimits.MaxQueryLookback == 0 || time.Duration(requestLimits.MaxQueryLookback) > original {
		return original
	}
	level.Debug(logutil.WithContext(ctx, l.logger)).Log("msg", "using request limit", "limit", "MaxQueryLookback", "tenant", userID, "query-limit", time.Duration(requestLimits.MaxQueryLookback), "original-limit", original)
	return time.Duration(requestLimits.MaxQueryLookback)
}

// MaxQueryRange retruns the max query range/interval of a query.
func (l *Limiter) MaxQueryRange(ctx context.Context, userID string) time.Duration {
	original := l.CombinedLimits.MaxQueryRange(ctx, userID)
	requestLimits := ExtractQueryLimitsContext(ctx)
	if requestLimits == nil || requestLimits.MaxQueryRange == 0 || time.Duration(requestLimits.MaxQueryRange) > original {
		return original
	}
	level.Debug(logutil.WithContext(ctx, l.logger)).Log("msg", "using request limit", "limit", "MaxQueryRange", "tenant", userID, "query-limit", time.Duration(requestLimits.MaxQueryRange), "original-limit", original)
	return time.Duration(requestLimits.MaxQueryRange)
}

// MaxEntriesLimitPerQuery returns the limit to number of entries the querier should return per query.
func (l *Limiter) MaxEntriesLimitPerQuery(ctx context.Context, userID string) int {
	original := l.CombinedLimits.MaxEntriesLimitPerQuery(ctx, userID)
	requestLimits := ExtractQueryLimitsContext(ctx)
	if requestLimits == nil || requestLimits.MaxEntriesLimitPerQuery == 0 || requestLimits.MaxEntriesLimitPerQuery > original {
		return original
	}
	level.Debug(logutil.WithContext(ctx, l.logger)).Log("msg", "using request limit", "limit", "MaxEntriesLimitPerQuery", "tenant", userID, "query-limit", requestLimits.MaxEntriesLimitPerQuery, "original-limit", original)
	return requestLimits.MaxEntriesLimitPerQuery
}

func (l *Limiter) QueryTimeout(ctx context.Context, userID string) time.Duration {
	original := l.CombinedLimits.QueryTimeout(ctx, userID)
	// in theory this error should never happen
	requestLimits := ExtractQueryLimitsContext(ctx)
	if requestLimits == nil || requestLimits.QueryTimeout == 0 || time.Duration(requestLimits.QueryTimeout) > original {
		return original
	}
	level.Debug(logutil.WithContext(ctx, l.logger)).Log("msg", "using request limit", "limit", "QueryTimeout", "tenant", userID, "query-limit", time.Duration(requestLimits.QueryTimeout), "original-limit", original)
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
	level.Debug(logutil.WithContext(ctx, l.logger)).Log("msg", "using request limit", "limit", "RequiredLabels", "tenant", userID, "query-limit", strings.Join(union, ", "), "original-limit", strings.Join(original, ", "))
	return union
}

func (l *Limiter) RequiredNumberLabels(ctx context.Context, userID string) int {
	original := l.CombinedLimits.RequiredNumberLabels(ctx, userID)
	requestLimits := ExtractQueryLimitsContext(ctx)
	if requestLimits == nil || requestLimits.RequiredNumberLabels == 0 || requestLimits.RequiredNumberLabels < original {
		return original
	}
	level.Debug(logutil.WithContext(ctx, l.logger)).Log("msg", "using request limit", "limit", "RequiredNumberLabels", "tenant", userID, "query-limit", requestLimits.RequiredNumberLabels, "original-limit", original)
	return requestLimits.RequiredNumberLabels
}

func (l *Limiter) MaxQueryBytesRead(ctx context.Context, userID string) int {
	original := l.CombinedLimits.MaxQueryBytesRead(ctx, userID)
	requestLimits := ExtractQueryLimitsContext(ctx)
	if requestLimits == nil || requestLimits.MaxQueryBytesRead.Val() == 0 || requestLimits.MaxQueryBytesRead.Val() > original {
		return original
	}
	level.Debug(logutil.WithContext(ctx, l.logger)).Log("msg", "using request limit", "limit", "MaxQueryBytesRead", "tenant", userID, "query-limit", requestLimits.MaxQueryBytesRead.Val(), "original-limit", original)
	return requestLimits.MaxQueryBytesRead.Val()
}
