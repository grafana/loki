package queryrange

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/weaveworks/common/httpgrpc"
)

const (
	limitErrTmpl = "maximum of series (%d) reached for a single query"
)

// Limits extends the cortex limits interface with support for per tenant splitby parameters
type Limits interface {
	queryrange.Limits
	QuerySplitDuration(string) time.Duration
	MaxQuerySeries(string) int
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

type seriesLimiter struct {
	hashes map[uint64]struct{}
	rw     sync.RWMutex
	buf    []byte

	maxSeries int
}

func newSeriesLimiter(maxSeries int) *seriesLimiter {
	return &seriesLimiter{
		hashes:    make(map[uint64]struct{}),
		maxSeries: maxSeries,
		buf:       make([]byte, 0, 1024),
	}
}

func (sl *seriesLimiter) Add(res queryrange.Response, resErr error) (queryrange.Response, error) {
	if resErr != nil {
		return res, resErr
	}
	promResponse, ok := res.(*LokiPromResponse)
	if !ok {
		return res, nil
	}
	if err := sl.IsLimitReached(); err != nil {
		return nil, err
	}
	if promResponse.Response == nil {
		return res, nil
	}
	sl.rw.Lock()
	var hash uint64
	for _, s := range promResponse.Response.Data.Result {
		lbs := client.FromLabelAdaptersToLabels(s.Labels)
		hash, sl.buf = lbs.HashWithoutLabels(sl.buf, []string(nil)...)
		sl.hashes[hash] = struct{}{}
	}
	sl.rw.Unlock()
	if err := sl.IsLimitReached(); err != nil {
		return nil, err
	}
	return res, nil
}

func (sl *seriesLimiter) IsLimitReached() error {
	sl.rw.RLock()
	defer sl.rw.RUnlock()
	if len(sl.hashes) > sl.maxSeries {
		return httpgrpc.Errorf(http.StatusBadRequest, limitErrTmpl, sl.maxSeries)
	}
	return nil
}
