package compactor

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
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
		got := groupIndexRuns([]indexpointers.IndexPointer{{Path: "b"}, {Path: "a"}})
		require.Equal(t, [][]string{{"b"}, {"a"}}, paths(got))
	})

	t.Run("groups the entries of one run in the order of the entries", func(t *testing.T) {
		got := groupIndexRuns([]indexpointers.IndexPointer{
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
			run.entries = append(run.entries, indexpointers.IndexPointer{Path: fmt.Sprintf("indexes/%02d", i), RunID: 3})
		}
		return run
	}

	t.Run("returns the path of an index object of no run", func(t *testing.T) {
		require.Equal(t, "indexes/a", indexRun{entries: []indexpointers.IndexPointer{{Path: "indexes/a"}}}.String())
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

func TestLayoutGroup_CountRuns(t *testing.T) {
	group := newLayoutGroup(streams.Layout{}, false)
	group.add(indexRun{entries: []indexpointers.IndexPointer{{Path: "run/a", RunID: 1}, {Path: "run/b", RunID: 1}}})
	group.add(indexRun{entries: []indexpointers.IndexPointer{{Path: "plain"}}})

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

// runOfEntry returns the index run of entry alone.
func runOfEntry(entry indexpointers.IndexPointer) indexRun {
	return indexRun{entries: []indexpointers.IndexPointer{entry}}
}

// runOfPath returns the index run of the index object at path alone.
func runOfPath(path string) indexRun {
	return runOfEntry(indexpointers.IndexPointer{Path: path})
}

func TestExistsInToC(t *testing.T) {
	ctx := context.Background()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	pointer := func(path string) indexpointers.IndexPointer {
		return indexpointers.IndexPointer{Path: path, StartTs: window.Add(time.Hour), EndTs: window.Add(2 * time.Hour), RunID: 1}
	}
	run := indexRun{entries: []indexpointers.IndexPointer{pointer("indexes/run/a"), pointer("indexes/run/b")}}

	t.Run("reports true when the ToC lists every index object of the run", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writeToCWithPointers(ctx, t, bucket, "acme", window, pointer("indexes/run/a"), pointer("indexes/run/b"))
		c := newTestCoordinator(t, bucket, &fakeRunner{}, &fakeReplacer{}, fixedClock(window), newFakeLimits("acme"))

		exists, err := c.existsInToC(ctx, "acme", window, run)
		require.NoError(t, err)
		require.True(t, exists)
	})

	t.Run("reports false when the ToC lists only some index objects of the run", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writeToCWithPointers(ctx, t, bucket, "acme", window, pointer("indexes/run/a"))
		c := newTestCoordinator(t, bucket, &fakeRunner{}, &fakeReplacer{}, fixedClock(window), newFakeLimits("acme"))

		exists, err := c.existsInToC(ctx, "acme", window, run)
		require.NoError(t, err)
		require.False(t, exists)
	})
}
