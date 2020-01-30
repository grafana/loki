package queryrange

import (
	"fmt"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
)

type CacheKeyLimits interface {
	Limits
	GenerateCacheKey(userID string, r queryrange.Request) string
}

type cacheKeyLimits struct {
	Limits
}

// GenerateCacheKey will panic if it encounters a 0 split duration. We ensure against this by requiring
// a nonzero split interval when caching is enabled
func (l cacheKeyLimits) GenerateCacheKey(userID string, r queryrange.Request) string {
	currentInterval := r.GetStart() / int64(l.QuerySplitDuration(userID)/time.Millisecond)
	return fmt.Sprintf("%s:%s:%d:%d", userID, r.GetQuery(), r.GetStep(), currentInterval)
}

func NewCacheKeyLimits(l Limits) CacheKeyLimits {
	return cacheKeyLimits{l}
}
