package logql

import (
	"math"
	"time"

	"github.com/grafana/loki/pkg/util/validation"
)

var (
	NoLimits = &fakeLimits{maxSeries: math.MaxInt32}
)

// Limits allow the engine to fetch limits for a given users.
type Limits interface {
	MaxQuerySeries(userID string) int
	QueryTimeout(userID string) time.Duration
	BlockedQueries(userID string) []*validation.BlockedQuery
}

type fakeLimits struct {
	maxSeries      int
	timeout        time.Duration
	blockedQueries []*validation.BlockedQuery
}

func (f fakeLimits) MaxQuerySeries(userID string) int {
	return f.maxSeries
}

func (f fakeLimits) QueryTimeout(userID string) time.Duration {
	return f.timeout
}

func (f fakeLimits) BlockedQueries(userID string) []*validation.BlockedQuery {
	return f.blockedQueries
}
