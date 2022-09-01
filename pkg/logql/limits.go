package logql

import (
	"math"
	"time"
)

var (
	NoLimits = &fakeLimits{maxSeries: math.MaxInt32}
)

// Limits allow the engine to fetch limits for a given users.
type Limits interface {
	MaxQuerySeries(userID string) int
	QueryTimeout(userID string) time.Duration
}

type fakeLimits struct {
	maxSeries int
	timeout   time.Duration
}

func (f fakeLimits) MaxQuerySeries(userID string) int {
	return f.maxSeries
}

func (f fakeLimits) QueryTimeout(userID string) time.Duration {
	return f.timeout
}
