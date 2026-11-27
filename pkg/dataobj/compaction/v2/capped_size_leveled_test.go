package compactionv2

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func newCappedStrategy(t *testing.T, k int, maxRunUncompressedBytes uint64) *CappedSizeLeveledStrategy {
	t.Helper()
	return NewCappedSizeLeveledStrategy(newStrategy(t, k), maxRunUncompressedBytes)
}

func TestCappedSizeLeveledStrategyNeedsCompaction(t *testing.T) {
	t.Run("returns true when there is no maximum run size and a level holds k runs", func(t *testing.T) {
		runs := []Run{namedRun{"a", 10 * gib}, namedRun{"b", 10 * gib}}
		require.True(t, newCappedStrategy(t, 2, 0).NeedsCompaction(runs))
	})

	t.Run("returns false when every full level sums above the maximum run size", func(t *testing.T) {
		runs := []Run{namedRun{"a", 10 * gib}, namedRun{"b", 10 * gib}}
		require.False(t, newCappedStrategy(t, 2, 19*gib).NeedsCompaction(runs))
	})

	t.Run("returns false when a full level sums to exactly the maximum run size", func(t *testing.T) {
		runs := []Run{namedRun{"a", 10 * gib}, namedRun{"b", 10 * gib}}
		require.False(t, newCappedStrategy(t, 2, 20*gib).NeedsCompaction(runs))
	})

	t.Run("returns true when a full level sums to below the maximum run size", func(t *testing.T) {
		runs := []Run{namedRun{"a", 10 * gib}, namedRun{"b", 10 * gib}}
		require.True(t, newCappedStrategy(t, 2, 20*gib+1).NeedsCompaction(runs))
	})
}

func TestCappedSizeLeveledStrategyCappedRuns(t *testing.T) {
	t.Run("returns zero when there is no maximum run size", func(t *testing.T) {
		runs := []Run{namedRun{"a", 10 * gib}, namedRun{"b", 10 * gib}}
		require.Zero(t, newCappedStrategy(t, 2, 0).CappedRuns(runs))
	})

	t.Run("counts the runs of groups at the maximum run size and not the leftover runs", func(t *testing.T) {
		runs := []Run{
			namedRun{"l1-a", 1 * gib}, namedRun{"l1-b", 1 * gib},
			namedRun{"l2-a", 64 * gib}, namedRun{"l2-b", 64 * gib}, namedRun{"l2-c", 64 * gib},
		}
		require.Equal(t, 2, newCappedStrategy(t, 2, 128*gib).CappedRuns(runs))
	})
}

func TestCappedSizeLeveledStrategyPlan(t *testing.T) {
	t.Run("returns the plan of the inner strategy when there is no maximum run size", func(t *testing.T) {
		runs := []Run{namedRun{"a", 10 * gib}, namedRun{"b", 10 * gib}, namedRun{"c", 10 * gib}}
		merges, unmerged := newCappedStrategy(t, 2, 0).Plan(runs, "tenant")
		require.Equal(t, [][]string{{"a", "b"}}, taskPaths(merges))
		require.Equal(t, []string{"c"}, runPaths(unmerged))
	})

	t.Run("returns the runs of a group above the maximum run size as unmerged after the leftover runs", func(t *testing.T) {
		runs := []Run{
			namedRun{"l1-a", 1 * gib}, namedRun{"l1-b", 1 * gib},
			namedRun{"l2-a", 64 * gib}, namedRun{"l2-b", 64 * gib}, namedRun{"l2-c", 64 * gib},
		}
		merges, unmerged := newCappedStrategy(t, 2, 100*gib).Plan(runs, "tenant")
		require.Equal(t, [][]string{{"l1-a", "l1-b"}}, taskPaths(merges))
		require.Equal(t, []string{"l2-c", "l2-a", "l2-b"}, runPaths(unmerged))
	})

	t.Run("returns unmerged runs with the size of their sections", func(t *testing.T) {
		runs := []Run{namedRun{"a", 10 * gib}, namedRun{"b", 12 * gib}}
		_, unmerged := newCappedStrategy(t, 2, 1*gib).Plan(runs, "tenant")
		require.Len(t, unmerged, 2)
		require.Equal(t, 10*gib, unmerged[0].Size())
		require.Equal(t, 12*gib, unmerged[1].Size())
	})

	t.Run("merges other groups of a level when one group is above the maximum run size", func(t *testing.T) {
		runs := []Run{
			namedRun{"big-a", 15 * gib}, namedRun{"big-b", 15 * gib},
			namedRun{"small-a", 1 * gib}, namedRun{"small-b", 1 * gib},
		}
		merges, unmerged := newCappedStrategy(t, 2, 16*gib).Plan(runs, "tenant")
		require.Equal(t, [][]string{{"small-a", "small-b"}}, taskPaths(merges))
		require.Equal(t, []string{"big-a", "big-b"}, runPaths(unmerged))
	})

	t.Run("merges into level 4 and not level 5 when the maximum run size is the level 5 lower bound", func(t *testing.T) {
		inner, err := NewSizeLeveledStrategy(DefaultSizeLevelBase, DefaultSizeLevelRatio, 8)
		require.NoError(t, err)
		level5 := DefaultSizeLevelBase * DefaultSizeLevelRatio * DefaultSizeLevelRatio * DefaultSizeLevelRatio
		require.Equal(t, 5, inner.Level(level5))
		require.Equal(t, 4, inner.Level(level5-1))

		var runs []Run
		for _, name := range []string{"l3-a", "l3-b", "l3-c", "l3-d", "l3-e", "l3-f", "l3-g", "l3-h"} {
			runs = append(runs, namedRun{name, 512 * gib})
		}
		for _, name := range []string{"l4-a", "l4-b", "l4-c", "l4-d", "l4-e", "l4-f", "l4-g", "l4-h"} {
			runs = append(runs, namedRun{name, 1024 * gib})
		}
		merges, unmerged := NewCappedSizeLeveledStrategy(inner, level5).Plan(runs, "tenant")
		require.Len(t, merges, 1)
		require.Equal(t, uint32(4), merges[0].OutputLevel)
		require.Equal(t, []string{"l4-a", "l4-b", "l4-c", "l4-d", "l4-e", "l4-f", "l4-g", "l4-h"}, runPaths(unmerged))
	})
}
