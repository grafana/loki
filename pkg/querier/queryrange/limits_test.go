package queryrange

import (
	"fmt"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/stretchr/testify/require"
)

func TestLimits(t *testing.T) {
	l := fakeLimits{
		splits: map[string]time.Duration{"a": time.Minute},
	}

	require.Equal(t, l.QuerySplitDuration("a"), time.Minute)
	require.Equal(t, l.QuerySplitDuration("b"), time.Duration(0))

	wrapped := WithDefaultLimits(l, queryrange.Config{
		SplitQueriesByDay: true,
	})

	require.Equal(t, wrapped.QuerySplitDuration("a"), time.Minute)
	require.Equal(t, wrapped.QuerySplitDuration("b"), 24*time.Hour)

	wrapped = WithDefaultLimits(l, queryrange.Config{
		SplitQueriesByDay:      true, // should be overridden by SplitQueriesByInterval
		SplitQueriesByInterval: time.Hour,
	})

	require.Equal(t, wrapped.QuerySplitDuration("a"), time.Minute)
	require.Equal(t, wrapped.QuerySplitDuration("b"), time.Hour)

	r := &LokiRequest{
		Query:   "qry",
		StartTs: time.Now(),
		Step:    int64(time.Minute / time.Millisecond),
	}

	require.Equal(
		t,
		fmt.Sprintf("%s:%s:%d:%d:%d", "a", r.GetQuery(), r.GetStep(), r.GetStart()/int64(time.Minute/time.Millisecond), int64(time.Minute)),
		cacheKeyLimits{wrapped}.GenerateCacheKey("a", r),
	)
}
