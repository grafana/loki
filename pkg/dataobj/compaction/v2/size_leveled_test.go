package compactionv2

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

const gib uint64 = 1 << 30

type sizedRun uint64

func (r sizedRun) Sections() []*compactionv2pb.SectionRef { return nil }
func (r sizedRun) Size() uint64                           { return uint64(r) }

func TestNewSizeLeveledStrategy(t *testing.T) {
	t.Run("returns an error when base is zero", func(t *testing.T) {
		_, err := NewSizeLeveledStrategy(0, 8)
		require.Error(t, err)
	})

	t.Run("returns an error when ratio is below 2", func(t *testing.T) {
		_, err := NewSizeLeveledStrategy(16*gib, 1)
		require.Error(t, err)
	})

	t.Run("accepts the default base and ratio", func(t *testing.T) {
		_, err := NewSizeLeveledStrategy(DefaultSizeLevelBase, DefaultSizeLevelRatio)
		require.NoError(t, err)
	})
}

func TestSizeLeveledStrategyLevel(t *testing.T) {
	s, err := NewSizeLeveledStrategy(16*gib, 8)
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
			require.Equal(t, test.want, s.Level(test.size))
		})
	}
}

func TestSizeLeveledStrategyGroupByLevels(t *testing.T) {
	s, err := NewSizeLeveledStrategy(DefaultSizeLevelBase, DefaultSizeLevelRatio)
	require.NoError(t, err)

	t.Run("returns no levels when there are no runs", func(t *testing.T) {
		require.Empty(t, s.GroupByLevels(nil))
	})

	t.Run("returns levels up to the highest level that holds a run", func(t *testing.T) {
		levels := s.GroupByLevels([]Run{sizedRun(200 * gib)})
		require.Len(t, levels, 3)
		require.Nil(t, levels[0])
		require.Nil(t, levels[1])
		require.Equal(t, []Run{sizedRun(200 * gib)}, levels[2])
	})

	t.Run("places the output of merging by the DefaultSizeLevelRatio runs one level up", func(t *testing.T) {
		const fresh = 6 * gib
		levels := s.GroupByLevels([]Run{sizedRun(fresh), sizedRun(8 * fresh), sizedRun(64 * fresh)})
		require.Equal(t, []Run{sizedRun(fresh)}, levels[0])
		require.Equal(t, []Run{sizedRun(8 * fresh)}, levels[1])
		require.Equal(t, []Run{sizedRun(64 * fresh)}, levels[2])
	})

	t.Run("keeps input order within a level", func(t *testing.T) {
		levels := s.GroupByLevels([]Run{sizedRun(3 * gib), sizedRun(200 * gib), sizedRun(1 * gib)})
		require.Equal(t, []Run{sizedRun(3 * gib), sizedRun(1 * gib)}, levels[0])
		require.Equal(t, []Run{sizedRun(200 * gib)}, levels[2])
	})
}

type namedRun struct {
	path string
	size uint64
}

func (r namedRun) Sections() []*compactionv2pb.SectionRef {
	return []*compactionv2pb.SectionRef{{ObjectPath: r.path}}
}
func (r namedRun) Size() uint64 { return r.size }

func taskPaths(tasks []*compactionv2pb.TaskSpec) [][]string {
	out := make([][]string, len(tasks))
	for i, task := range tasks {
		for _, run := range task.Runs {
			out[i] = append(out[i], run.Sections[0].ObjectPath)
		}
	}
	return out
}

func TestSizeLeveledStrategyNeedsCompaction(t *testing.T) {
	s, err := NewSizeLeveledStrategy(DefaultSizeLevelBase, DefaultSizeLevelRatio)
	require.NoError(t, err)

	t.Run("returns false when there are no runs", func(t *testing.T) {
		require.False(t, s.NeedsCompaction(nil, 2))
	})

	t.Run("returns false when each level holds one run", func(t *testing.T) {
		runs := []Run{namedRun{"l0", 6 * gib}, namedRun{"l1", 48 * gib}, namedRun{"l2", 384 * gib}}
		require.False(t, s.NeedsCompaction(runs, 2))
	})

	t.Run("returns false when the total reaches k but no level does", func(t *testing.T) {
		runs := []Run{
			namedRun{"l0-a", 1 * gib}, namedRun{"l0-b", 1 * gib},
			namedRun{"l1-a", 20 * gib}, namedRun{"l1-b", 20 * gib},
		}
		require.False(t, s.NeedsCompaction(runs, 3))
	})

	t.Run("returns true when one level holds exactly k runs", func(t *testing.T) {
		runs := []Run{
			namedRun{"l0", 1 * gib},
			namedRun{"l1-a", 20 * gib}, namedRun{"l1-b", 20 * gib}, namedRun{"l1-c", 20 * gib},
		}
		require.True(t, s.NeedsCompaction(runs, 3))
	})
}

func TestSizeLeveledStrategyPlan(t *testing.T) {
	s, err := NewSizeLeveledStrategy(DefaultSizeLevelBase, DefaultSizeLevelRatio)
	require.NoError(t, err)

	t.Run("returns no tasks when there are no runs", func(t *testing.T) {
		require.Empty(t, s.Plan(nil, "tenant", 2, nil))
	})

	t.Run("splits a level into tasks of at most k runs", func(t *testing.T) {
		runs := []Run{namedRun{"a", 1 * gib}, namedRun{"b", 1 * gib}, namedRun{"c", 1 * gib}}
		require.Equal(t, [][]string{{"a", "b"}, {"c"}}, taskPaths(s.Plan(runs, "tenant", 2, nil)))
	})

	t.Run("does not mix runs from different levels in one task", func(t *testing.T) {
		runs := []Run{namedRun{"l0-a", 1 * gib}, namedRun{"l2", 200 * gib}, namedRun{"l0-b", 2 * gib}}
		require.Equal(t, [][]string{{"l0-a", "l0-b"}, {"l2"}}, taskPaths(s.Plan(runs, "tenant", 8, nil)))
	})

	t.Run("sets the tenant and sort schema on every task", func(t *testing.T) {
		runs := []Run{namedRun{"l0", 1 * gib}, namedRun{"l2", 200 * gib}}
		for _, task := range s.Plan(runs, "tenant", 8, []string{"service"}) {
			require.Equal(t, "tenant", task.Tenant)
			require.Equal(t, []string{"service"}, task.SortSchema)
		}
	})
}
