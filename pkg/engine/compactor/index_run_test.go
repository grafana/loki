package compactor

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
)

func TestGroupIndexRuns(t *testing.T) {
	paths := func(runs []indexRun) [][]string {
		var out [][]string
		for _, run := range runs {
			var group []string
			for _, entry := range run.entries {
				group = append(group, entry.Path)
			}
			out = append(out, group)
		}
		return out
	}

	t.Run("makes each entry of no run a run of its own", func(t *testing.T) {
		got := groupIndexRuns([]indexEntry{{Path: "b"}, {Path: "a"}})
		require.Equal(t, [][]string{{"b"}, {"a"}}, paths(got))
	})

	t.Run("groups the entries of one run in the order of the entries", func(t *testing.T) {
		got := groupIndexRuns([]indexEntry{
			{Path: "run-1/b", RunID: 1},
			{Path: "single"},
			{Path: "run-1/a", RunID: 1},
			{Path: "run-2/a", RunID: 2},
		})
		require.Equal(t, [][]string{{"run-1/b", "run-1/a"}, {"single"}, {"run-2/a"}}, paths(got))
	})
}

func TestIndexRun_String(t *testing.T) {
	runOf := func(n int) indexRun {
		var run indexRun
		for i := range n {
			run.entries = append(run.entries, indexEntry{Path: fmt.Sprintf("indexes/%02d", i), RunID: 3})
		}
		return run
	}

	t.Run("returns the path of an index object of no run", func(t *testing.T) {
		require.Equal(t, "indexes/a", indexRun{entries: []indexEntry{{Path: "indexes/a"}}}.String())
	})

	t.Run("lists every path of a run of at most eight index objects", func(t *testing.T) {
		require.Equal(t, "run 3 [indexes/00 indexes/01]", runOf(2).String())
	})

	t.Run("lists eight paths and counts the rest of a larger run", func(t *testing.T) {
		require.Equal(t,
			"run 3 [indexes/00 indexes/01 indexes/02 indexes/03 indexes/04 indexes/05 indexes/06 indexes/07 and 2 more]",
			runOf(10).String(),
		)
	})
}

func TestLayoutGroup_CompleteTasks(t *testing.T) {
	newGroup := func(runs ...indexRun) *layoutGroup {
		group := newLayoutGroup(indexpointers.ShardBuckets{})
		for _, run := range runs {
			group.add(run)
		}
		return group
	}
	run := indexRun{entries: []indexEntry{{Path: "run/a", RunID: 1}, {Path: "run/b", RunID: 1}, {Path: "run/stats", RunID: 1}}}
	plain := indexRun{entries: []indexEntry{{Path: "plain"}}}
	paths := func(task *compactionv2pb.TaskSpec) [][]string {
		var out [][]string
		for _, runRef := range task.Runs {
			var refs []string
			for _, section := range runRef.Sections {
				refs = append(refs, section.ObjectPath)
			}
			out = append(out, refs)
		}
		return out
	}

	t.Run("adds every missing index object of a run once, to the planner run that holds the run", func(t *testing.T) {
		task := &compactionv2pb.TaskSpec{Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "run/a"}, {ObjectPath: "run/a", SectionIndex: 1}}},
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "plain"}}},
		}}

		require.NoError(t, newGroup(run, plain).completeTasks([]*compactionv2pb.TaskSpec{task}))
		require.Equal(t, [][]string{{"run/a", "run/a", "run/b", "run/stats"}, {"plain"}}, paths(task))
	})

	t.Run("adds nothing to a task that holds every index object of its runs", func(t *testing.T) {
		task := &compactionv2pb.TaskSpec{Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "run/a"}, {ObjectPath: "run/b"}, {ObjectPath: "run/stats"}}},
		}}

		require.NoError(t, newGroup(run).completeTasks([]*compactionv2pb.TaskSpec{task}))
		require.Equal(t, [][]string{{"run/a", "run/b", "run/stats"}}, paths(task))
	})

	t.Run("adds no index object of a run that the task does not merge", func(t *testing.T) {
		task := &compactionv2pb.TaskSpec{Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "plain"}}},
		}}

		require.NoError(t, newGroup(run, plain).completeTasks([]*compactionv2pb.TaskSpec{task}))
		require.Equal(t, [][]string{{"plain"}}, paths(task))
	})

	t.Run("returns an error when two tasks reference index objects of one run", func(t *testing.T) {
		tasks := []*compactionv2pb.TaskSpec{
			{Runs: []*compactionv2pb.RunRef{{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "run/a"}}}}},
			{Runs: []*compactionv2pb.RunRef{{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "run/b"}}}}},
		}

		require.Error(t, newGroup(run).completeTasks(tasks))
	})

	t.Run("returns an error when a task references an index object outside the group", func(t *testing.T) {
		task := &compactionv2pb.TaskSpec{Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "unknown"}}},
		}}

		require.Error(t, newGroup(plain).completeTasks([]*compactionv2pb.TaskSpec{task}))
	})
}

func TestLayoutGroup_CountRuns(t *testing.T) {
	group := newLayoutGroup(indexpointers.ShardBuckets{})
	group.add(indexRun{entries: []indexEntry{{Path: "run/a", RunID: 1}, {Path: "run/b", RunID: 1}}})
	group.add(indexRun{entries: []indexEntry{{Path: "plain"}}})

	t.Run("counts the index objects of one run as one run", func(t *testing.T) {
		got, err := group.countRuns([]string{"run/a", "run/b", "plain"})
		require.NoError(t, err)
		require.Equal(t, 2, got)
	})

	t.Run("returns an error for an index object outside the group", func(t *testing.T) {
		_, err := group.countRuns([]string{"unknown"})
		require.Error(t, err)
	})
}
