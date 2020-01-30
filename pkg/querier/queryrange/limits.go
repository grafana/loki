package queryrange

import (
	"time"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
)

type Limits interface {
	queryrange.Limits
	QuerySplitDuration(string) time.Duration
}

type limits struct {
	Limits
	splitDuration time.Duration
}

func (l limits) QuerySplitDuration(user string) time.Duration {
	dur := l.Limits.QuerySplitDuration(user)
	if dur == 0 {
		return l.splitDuration
	}
	return dur
}

// WithDefaults will construct a Limits with a default value for QuerySplitDuration when no overrides are present.
func WithDefaultLimits(l Limits, conf queryrange.Config) Limits {
	res := limits{
		Limits: l,
	}

	if conf.SplitQueriesByDay {
		res.splitDuration = 24 * time.Hour
	}

	if conf.SplitQueriesByInterval != 0 {
		res.splitDuration = conf.SplitQueriesByInterval
	}

	return res
}
