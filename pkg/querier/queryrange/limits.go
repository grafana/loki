package queryrange

import (
	"fmt"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
)

// Limits extends the cortex limits interface with support for per tenant splitby parameters
type Limits interface {
	queryrange.Limits
	QuerySplitDuration(string) time.Duration
	MaxEntriesLimitPerQuery(string) int
}

type limits struct {
	Limits
	splitDuration time.Duration
	overrides     bool
}

func (l limits) QuerySplitDuration(user string) time.Duration {
	if !l.overrides {
		return l.splitDuration
	}
	dur := l.Limits.QuerySplitDuration(user)
	if dur == 0 {
		return l.splitDuration
	}
	return dur
}

// WithDefaults will construct a Limits with a default value for QuerySplitDuration when no overrides are present.
func WithDefaultLimits(l Limits, conf queryrange.Config) Limits {
	res := limits{
		Limits:    l,
		overrides: true,
	}

	if conf.SplitQueriesByDay {
		res.splitDuration = 24 * time.Hour
	}

	if conf.SplitQueriesByInterval != 0 {
		res.splitDuration = conf.SplitQueriesByInterval
	}

	return res
}

// WithSplitByLimits will construct a Limits with a static split by duration.
func WithSplitByLimits(l Limits, splitBy time.Duration) Limits {
	return limits{
		Limits:        l,
		splitDuration: splitBy,
	}
}

// cacheKeyLimits intersects Limits and CacheSplitter
type cacheKeyLimits struct {
	Limits
}

// GenerateCacheKey will panic if it encounters a 0 split duration. We ensure against this by requiring
// a nonzero split interval when caching is enabled
func (l cacheKeyLimits) GenerateCacheKey(userID string, r queryrange.Request) string {
	split := l.QuerySplitDuration(userID)
	currentInterval := r.GetStart() / int64(split/time.Millisecond)
	// include both the currentInterval and the split duration in key to ensure
	// a cache key can't be reused when an interval changes
	return fmt.Sprintf("%s:%s:%d:%d:%d", userID, r.GetQuery(), r.GetStep(), currentInterval, split)
}
