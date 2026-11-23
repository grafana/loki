package compactor

import (
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
)

func TestPlanPairs(t *testing.T) {
	cfg := PairPlanConfig{LogMaxRunsPerTask: 2, LogMinCompactionSize: 1}

	t.Run("reports the pairs, merges and splits of the window", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairIndex(t.Context(), t, bucket, "indexes/a", pairTestSchema, testLogObject{"logs/a", "a", "z"})
		seedPairIndex(t.Context(), t, bucket, "indexes/b", pairTestSchema, testLogObject{"logs/b", "a", "z"})
		seedPairIndex(t.Context(), t, bucket, "indexes/c", pairTestSchema, testLogObject{"logs/c", "a", "z"})
		seedPairIndex(t.Context(), t, bucket, "indexes/split", pairTestSchema, testLogObject{"logs/split-0", "a", "c"}, testLogObject{"logs/split-1", "b", "d"})

		plan, err := PlanPairs(t.Context(), log.NewNopLogger(), bucket, "acme", imWindow(), cfg)
		require.NoError(t, err)

		require.Equal(t, 4, plan.IndexRuns)
		require.Zero(t, plan.ReadFailures)
		require.Len(t, plan.Pairs, 3)
		for _, source := range plan.Pairs {
			require.Equal(t, 1, source.LogObjects)
			require.Equal(t, uint64(200), source.UncompressedSize)
			require.Equal(t, 1, source.Level)
		}
		require.Len(t, plan.Merges, 1)
		merge := plan.Merges[0]
		require.Len(t, merge.IndexRuns, 2)
		require.Equal(t, 1, merge.Level)
		require.Equal(t, 1, merge.OutputLevel)
		require.Equal(t, 1, merge.LogTasks)
		require.Equal(t, int64(400), merge.InputBytes)
		require.Equal(t, imWindow().Add(time.Hour), merge.MinTime)
		require.Equal(t, imWindow().Add(2*time.Hour), merge.MaxTime)
		require.Equal(t, []PlannedSplit{{IndexPaths: []string{"indexes/split"}, LogRuns: 2, UncompressedSize: 400}}, plan.Splits)
	})

	t.Run("splits a merge into log tasks above the maximum log task size", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairIndex(t.Context(), t, bucket, "indexes/a", pairTestSchema, testLogObject{"logs/a", "a", "z"})
		seedPairIndex(t.Context(), t, bucket, "indexes/b", pairTestSchema, testLogObject{"logs/b", "a", "z"})
		splitCfg := cfg
		splitCfg.LogMaxTaskSize = 200

		plan, err := PlanPairs(t.Context(), log.NewNopLogger(), bucket, "acme", imWindow(), splitCfg)
		require.NoError(t, err)
		require.Len(t, plan.Merges, 1)
		require.Equal(t, 2, plan.Merges[0].LogTasks)
	})

	t.Run("returns an error when K is below 2", func(t *testing.T) {
		_, err := PlanPairs(t.Context(), log.NewNopLogger(), objstore.NewInMemBucket(), "acme", imWindow(), PairPlanConfig{LogMaxRunsPerTask: 1})
		require.Error(t, err)
	})

	t.Run("has no work in a window without a ToC", func(t *testing.T) {
		plan, err := PlanPairs(t.Context(), log.NewNopLogger(), objstore.NewInMemBucket(), "acme", imWindow(), cfg)
		require.NoError(t, err)
		require.Equal(t, PairPlan{}, plan)
	})
}
