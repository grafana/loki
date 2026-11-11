package compactor

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
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
