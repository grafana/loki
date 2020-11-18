package queryrange

import (
	"context"
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
	buf    []byte // buf used for hashing to avoid allocations.

	maxSeries int
	next      queryrange.Handler
}

type seriesLimiterMiddleware int

// newSeriesLimiter creates a new series limiter middleware for use for a single request.
func newSeriesLimiter(maxSeries int) queryrange.Middleware {
	return seriesLimiterMiddleware(maxSeries)
}

// Wrap wraps a global handler and returns a per request limited handler.
// The handler returned is thread safe.
func (slm seriesLimiterMiddleware) Wrap(next queryrange.Handler) queryrange.Handler {
	return &seriesLimiter{
		hashes:    make(map[uint64]struct{}),
		maxSeries: int(slm),
		buf:       make([]byte, 0, 1024),
		next:      next,
	}
}

func (sl *seriesLimiter) Do(ctx context.Context, req queryrange.Request) (queryrange.Response, error) {
	// no need to fire a request if the limit is already reached.
	if sl.isLimitReached() {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, limitErrTmpl, sl.maxSeries)
	}
	res, err := sl.next.Do(ctx, req)
	if err != nil {
		return res, err
	}
	promResponse, ok := res.(*LokiPromResponse)
	if !ok {
		return res, nil
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
	if sl.isLimitReached() {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, limitErrTmpl, sl.maxSeries)
	}
	return res, nil
}

func (sl *seriesLimiter) isLimitReached() bool {
	sl.rw.RLock()
	defer sl.rw.RUnlock()
	return len(sl.hashes) > sl.maxSeries
}
