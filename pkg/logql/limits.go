package logql

import (
	"context"
	"math"
	"time"

	"github.com/grafana/loki/v3/pkg/util/validation"
)

var (
	NoLimits = &fakeLimits{maxSeries: math.MaxInt32}
)

// Limits allow the engine to fetch limits for a given users.
type Limits interface {
	MaxQuerySeries(context.Context, string) int
	MaxQueryRange(ctx context.Context, userID string) time.Duration
	QueryTimeout(context.Context, string) time.Duration
	BlockedQueries(context.Context, string) []*validation.BlockedQuery
}

type fakeLimits struct {
	maxSeries      int
	timeout        time.Duration
	blockedQueries []*validation.BlockedQuery
	rangeLimit     time.Duration
	requiredLabels []string
}

func (f fakeLimits) MaxQuerySeries(_ context.Context, _ string) int {
	return f.maxSeries
}

func (f fakeLimits) MaxQueryRange(_ context.Context, _ string) time.Duration {
	return f.rangeLimit
}

func (f fakeLimits) QueryTimeout(_ context.Context, _ string) time.Duration {
	return f.timeout
}

func (f fakeLimits) BlockedQueries(_ context.Context, _ string) []*validation.BlockedQuery {
	return f.blockedQueries
}

func (f fakeLimits) RequiredLabels(_ context.Context, _ string) []string {
	return f.requiredLabels
}
