package limits

import (
	"context"
	"time"

	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/pattern"
)

type TimeRangeLimits interface {
	MaxQueryLookback(context.Context, string) time.Duration
	MaxQueryLength(context.Context, string) time.Duration
}

type Limits interface {
	logql.Limits
	pattern.Limits
	TimeRangeLimits
	QueryTimeout(context.Context, string) time.Duration
	MaxStreamsMatchersPerQuery(context.Context, string) int
	MaxConcurrentTailRequests(context.Context, string) int
	MaxEntriesLimitPerQuery(context.Context, string) int
	RequiredLabels(context.Context, string) []string
	RequiredNumberLabels(context.Context, string) int

	// DataObjQueryStartTime returns the time from which the querier reads the tenant's
	// stream-first metric queries from data objects. The zero time disables it.
	DataObjQueryStartTime(userID string) time.Time
}
