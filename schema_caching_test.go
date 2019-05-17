package chunk

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/mtime"
)

func TestCachingSchema(t *testing.T) {
	const (
		userID         = "userid"
		periodicPrefix = "periodicPrefix"
	)

	dailyBuckets := makeSchema("v3")
	schema := &schemaCaching{
		Schema:         dailyBuckets,
		cacheOlderThan: 24 * time.Hour,
	}

	baseTime := time.Unix(0, 0)
	baseTime = baseTime.Add(30*24*time.Hour - 1)

	mtime.NowForce(baseTime)

	for _, tc := range []struct {
		from, through time.Time

		cacheableIdx int
	}{
		{
			// Completely cacheable.
			baseTime.Add(-36 * time.Hour),
			baseTime.Add(-25 * time.Hour),
			0,
		},
		{
			// Completely active.
			baseTime.Add(-23 * time.Hour),
			baseTime.Add(-2 * time.Hour),
			-1,
		},
		{
			// Mix of both but the cacheable entry is also active.
			baseTime.Add(-36 * time.Hour),
			baseTime.Add(-2 * time.Hour),
			-1,
		},
		{
			// Mix of both.
			baseTime.Add(-50 * time.Hour),
			baseTime.Add(-2 * time.Hour),
			0,
		},
	} {
		have, err := schema.GetReadQueriesForMetric(
			model.TimeFromUnix(tc.from.Unix()), model.TimeFromUnix(tc.through.Unix()),
			userID, "foo",
		)
		if err != nil {
			t.Fatal(err)
		}

		for i := range have {
			if i <= tc.cacheableIdx {
				require.True(t, have[i].Immutable)
			} else {
				require.False(t, have[i].Immutable)
			}
		}
	}
}
