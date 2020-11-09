package logql

import (
	"math"
)

var (
	NoLimits = &fakeLimits{maxSeries: math.MaxInt32}
)

// Limits allow the engine to fetch limits for a given users.
type Limits interface {
	MaxQuerySeries(userID string) int
}

type fakeLimits struct {
	maxSeries int
}

func (f fakeLimits) MaxQuerySeries(userID string) int {
	return f.maxSeries
}
