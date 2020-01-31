package queryrange

import (
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/stretchr/testify/require"
)

func TestWithDefaultLimits(t *testing.T) {
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

}
