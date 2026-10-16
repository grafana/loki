package compactionv2

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

const gib uint64 = 1 << 30

type namedRun struct {
	path string
	size uint64
}

func (r namedRun) Sections() []*compactionv2pb.SectionRef {
	return []*compactionv2pb.SectionRef{{ObjectPath: r.path}}
}
func (r namedRun) Size() uint64 { return r.size }

func TestNewSizeLeveledStrategy(t *testing.T) {
	t.Run("returns an error when base is zero", func(t *testing.T) {
		_, err := NewSizeLeveledStrategy(0, 8, 8)
		require.Error(t, err)
	})

	t.Run("returns an error when ratio is below 2", func(t *testing.T) {
		_, err := NewSizeLeveledStrategy(16*gib, 1, 8)
		require.Error(t, err)
	})

	t.Run("returns an error when k is 1 because a single run would always need compaction", func(t *testing.T) {
		_, err := NewSizeLeveledStrategy(DefaultSizeLevelBase, DefaultSizeLevelRatio, 1)
		require.Error(t, err)
	})

	t.Run("accepts the default base and ratio with k of 2", func(t *testing.T) {
		_, err := NewSizeLeveledStrategy(DefaultSizeLevelBase, DefaultSizeLevelRatio, 2)
		require.NoError(t, err)
	})
}

func TestSizeLeveledStrategyLevel(t *testing.T) {
	s, err := NewSizeLeveledStrategy(16*gib, 8, 8)
	require.NoError(t, err)

	tests := []struct {
		name string
		size uint64
		want int
	}{
		{name: "an empty run is in level 0", size: 0, want: 0},
		{name: "a run just below base is in level 0", size: 16*gib - 1, want: 0},
		{name: "a run equal to base is in level 1", size: 16 * gib, want: 1},
		{name: "a run just below base times ratio is in level 1", size: 128*gib - 1, want: 1},
		{name: "a run equal to base times ratio is in level 2", size: 128 * gib, want: 2},
		{name: "a run of 1PiB is in level 6", size: 1 << 50, want: 6},
		{name: "the largest possible run does not overflow and is in the top level", size: math.MaxUint64, want: 10},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, s.level(test.size))
		})
	}
}

func TestSizeLeveledStrategyGroupByLevels(t *testing.T) {
	s, err := NewSizeLeveledStrategy(DefaultSizeLevelBase, DefaultSizeLevelRatio, 8)
	require.NoError(t, err)

	t.Run("returns no levels when there are no runs", func(t *testing.T) {
		require.Empty(t, s.groupByLevels(nil))
	})

	t.Run("returns levels up to the highest level that holds a run", func(t *testing.T) {
		levels := s.groupByLevels([]Run{namedRun{"big", 200 * gib}})
		require.Len(t, levels, 3)
		require.Nil(t, levels[0])
		require.Nil(t, levels[1])
		require.Equal(t, []Run{namedRun{"big", 200 * gib}}, levels[2])
	})

	t.Run("places a merge of ratio fresh runs one level above a fresh run", func(t *testing.T) {
		const fresh = 6 * gib
		levels := s.groupByLevels([]Run{namedRun{"fresh", fresh}, namedRun{"fresh-x8", 8 * fresh}, namedRun{"fresh-x64", 64 * fresh}})
		require.Equal(t, []Run{namedRun{"fresh", fresh}}, levels[0])
		require.Equal(t, []Run{namedRun{"fresh-x8", 8 * fresh}}, levels[1])
		require.Equal(t, []Run{namedRun{"fresh-x64", 64 * fresh}}, levels[2])
	})

	t.Run("keeps input order within a level", func(t *testing.T) {
		levels := s.groupByLevels([]Run{namedRun{"a", 3 * gib}, namedRun{"big", 200 * gib}, namedRun{"b", 1 * gib}})
		require.Equal(t, []Run{namedRun{"a", 3 * gib}, namedRun{"b", 1 * gib}}, levels[0])
		require.Equal(t, []Run{namedRun{"big", 200 * gib}}, levels[2])
	})
}

func newStrategy(t *testing.T, k int) *SizeLeveledStrategy {
	t.Helper()
	s, err := NewSizeLeveledStrategy(DefaultSizeLevelBase, DefaultSizeLevelRatio, k)
	require.NoError(t, err)
	return s
}

func taskPaths(tasks []*compactionv2pb.TaskSpec) [][]string {
	out := make([][]string, len(tasks))
	for i, task := range tasks {
		for _, run := range task.Runs {
			out[i] = append(out[i], run.Sections[0].ObjectPath)
		}
	}
	return out
}

func TestSizeLeveledStrategyRunsPerLevel(t *testing.T) {
	t.Run("returns no counts when there are no runs", func(t *testing.T) {
		require.Empty(t, newStrategy(t, 2).RunsPerLevel(nil))
	})

	t.Run("counts the runs in each level and zero for empty levels", func(t *testing.T) {
		runs := []Run{namedRun{"l0-a", 1 * gib}, namedRun{"l2", 200 * gib}, namedRun{"l0-b", 2 * gib}}
		require.Equal(t, []int{2, 0, 1}, newStrategy(t, 2).RunsPerLevel(runs))
	})
}

func TestSizeLeveledStrategyNeedsCompaction(t *testing.T) {
	t.Run("returns false when there are no runs", func(t *testing.T) {
		require.False(t, newStrategy(t, 2).NeedsCompaction(nil))
	})

	t.Run("returns false when each level holds one run", func(t *testing.T) {
		runs := []Run{namedRun{"l0", 6 * gib}, namedRun{"l1", 48 * gib}, namedRun{"l2", 384 * gib}}
		require.False(t, newStrategy(t, 2).NeedsCompaction(runs))
	})

	t.Run("returns false when k is larger than the number of runs", func(t *testing.T) {
		runs := []Run{namedRun{"a", 1 * gib}, namedRun{"b", 1 * gib}}
		require.False(t, newStrategy(t, 8).NeedsCompaction(runs))
	})

	t.Run("returns false when the total reaches k but no level does", func(t *testing.T) {
		runs := []Run{
			namedRun{"l0-a", 1 * gib}, namedRun{"l0-b", 1 * gib},
			namedRun{"l1-a", 20 * gib}, namedRun{"l1-b", 20 * gib},
		}
		require.False(t, newStrategy(t, 3).NeedsCompaction(runs))
	})

	t.Run("returns true when one level holds exactly k runs", func(t *testing.T) {
		runs := []Run{
			namedRun{"l0", 1 * gib},
			namedRun{"l1-a", 20 * gib}, namedRun{"l1-b", 20 * gib}, namedRun{"l1-c", 20 * gib},
		}
		require.True(t, newStrategy(t, 3).NeedsCompaction(runs))
	})
}

func runPaths(runs []Run) []string {
	paths := make([]string, len(runs))
	for i, run := range runs {
		paths[i] = run.Sections()[0].ObjectPath
	}
	return paths
}

func TestSizeLeveledStrategyPlan(t *testing.T) {
	t.Run("returns no tasks and no unmerged runs when there are no runs", func(t *testing.T) {
		merges, unmerged := newStrategy(t, 2).Plan(nil, "tenant", nil)
		require.Empty(t, merges)
		require.Empty(t, unmerged)
	})

	t.Run("splits a level into merge tasks of at most k runs", func(t *testing.T) {
		runs := []Run{namedRun{"a", 1 * gib}, namedRun{"b", 1 * gib}, namedRun{"c", 1 * gib}, namedRun{"d", 1 * gib}}
		merges, unmerged := newStrategy(t, 2).Plan(runs, "tenant", nil)
		require.Equal(t, [][]string{{"a", "b"}, {"c", "d"}}, taskPaths(merges))
		require.Empty(t, unmerged)
	})

	t.Run("returns a run left over after the split as unmerged", func(t *testing.T) {
		runs := []Run{namedRun{"a", 1 * gib}, namedRun{"b", 1 * gib}, namedRun{"c", 1 * gib}}
		merges, unmerged := newStrategy(t, 2).Plan(runs, "tenant", nil)
		require.Equal(t, [][]string{{"a", "b"}}, taskPaths(merges))
		require.Equal(t, []string{"c"}, runPaths(unmerged))
	})

	t.Run("returns a run alone in its level as unmerged", func(t *testing.T) {
		runs := []Run{namedRun{"l0-a", 1 * gib}, namedRun{"l2", 200 * gib}, namedRun{"l0-b", 2 * gib}}
		merges, unmerged := newStrategy(t, 8).Plan(runs, "tenant", nil)
		require.Equal(t, [][]string{{"l0-a", "l0-b"}}, taskPaths(merges))
		require.Equal(t, []string{"l2"}, runPaths(unmerged))
	})

	t.Run("puts a run of exactly base size in level 1 and not with level 0 runs", func(t *testing.T) {
		runs := []Run{namedRun{"below", DefaultSizeLevelBase - 1}, namedRun{"at", DefaultSizeLevelBase}, namedRun{"small", 1 * gib}}
		merges, unmerged := newStrategy(t, 8).Plan(runs, "tenant", nil)
		require.Equal(t, [][]string{{"below", "small"}}, taskPaths(merges))
		require.Equal(t, []string{"at"}, runPaths(unmerged))
	})

	t.Run("sets the tenant and sort schema on every merge task", func(t *testing.T) {
		runs := []Run{namedRun{"a", 1 * gib}, namedRun{"b", 1 * gib}, namedRun{"c", 200 * gib}, namedRun{"d", 200 * gib}}
		merges, _ := newStrategy(t, 8).Plan(runs, "tenant", []string{"service"})
		require.Len(t, merges, 2)
		for _, task := range merges {
			require.Equal(t, "tenant", task.Tenant)
			require.Equal(t, []string{"service"}, task.SortSchema)
		}
	})
}
