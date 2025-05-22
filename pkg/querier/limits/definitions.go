package limits

import (
	"context"
	"time"

	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/pattern/aggregation"
)

type TimeRangeLimits interface {
	MaxQueryLookback(context.Context, string) time.Duration
	MaxQueryLength(context.Context, string) time.Duration
}

type Limits interface {
	logql.Limits
	aggregation.Limits
	TimeRangeLimits
	QueryTimeout(context.Context, string) time.Duration
	MaxStreamsMatchersPerQuery(context.Context, string) int
	MaxConcurrentTailRequests(context.Context, string) int
	MaxEntriesLimitPerQuery(context.Context, string) int
}
