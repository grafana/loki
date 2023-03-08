package logql

import (
	"context"
	"math"
	"time"

	"github.com/grafana/loki/pkg/util/validation"
)

var (
	NoLimits = &fakeLimits{maxSeries: math.MaxInt32}
)

// Limits allow the engine to fetch limits for a given users.
type Limits interface {
	MaxQuerySeries(context.Context, string) int
	QueryTimeout(context.Context, string) time.Duration
	BlockedQueries(context.Context, string) []*validation.BlockedQuery
}

type fakeLimits struct {
	maxSeries      int
	timeout        time.Duration
	blockedQueries []*validation.BlockedQuery
}

func (f fakeLimits) MaxQuerySeries(ctx context.Context, userID string) int {
	return f.maxSeries
}

func (f fakeLimits) QueryTimeout(ctx context.Context, userID string) time.Duration {
	return f.timeout
}

func (f fakeLimits) BlockedQueries(ctx context.Context, userID string) []*validation.BlockedQuery {
	return f.blockedQueries
}
