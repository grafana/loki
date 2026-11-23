package compactor

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

func TestLogCompactionPlanLogTaskStats(t *testing.T) {
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	mergeTask := func(oldest time.Time, bytes uint64) logMergeTask {
		return newLogMergeTask(&compactionv2pb.TaskSpec{Runs: []*compactionv2pb.RunRef{{Sections: []*compactionv2pb.SectionRef{
			{ObjectPath: "logs/a", MinTimestamp: oldest.UnixNano(), MaxTimestamp: oldest.UnixNano(), UncompressedSize: int64(bytes)},
		}}}})
	}

	t.Run("yields the bytes and oldest timestamp of each task that reads log data", func(t *testing.T) {
		plan := logCompactionPlan{tasks: []logCompactionTask{
			mergeTask(now.Add(-30*time.Minute), 16),
			mergeTask(now.Add(-12*time.Hour), 100),
		}}

		require.Equal(t, []logTaskStats{
			{inputBytes: 16, oldestTimestamp: now.Add(-30 * time.Minute)},
			{inputBytes: 100, oldestTimestamp: now.Add(-12 * time.Hour)},
		}, slices.Collect(plan.logTaskStats()))
	})

	t.Run("skips an IndexFilter task", func(t *testing.T) {
		plan := logCompactionPlan{tasks: []logCompactionTask{
			indexFilterTask{inputRange: newTimeRange(now.Add(-48*time.Hour), now)},
		}}

		require.Empty(t, slices.Collect(plan.logTaskStats()))
	})

	t.Run("stops yielding when the caller stops iterating", func(t *testing.T) {
		plan := logCompactionPlan{tasks: []logCompactionTask{
			mergeTask(now, 1),
			mergeTask(now, 2),
		}}

		var seen []logTaskStats
		for stats := range plan.logTaskStats() {
			seen = append(seen, stats)
			break
		}
		require.Len(t, seen, 1)
	})
}

func TestLogMergeTaskLogStats(t *testing.T) {
	section := func(path string, minTS int64, size int64) *compactionv2pb.SectionRef {
		return &compactionv2pb.SectionRef{ObjectPath: path, MinTimestamp: minTS, MaxTimestamp: minTS + 10, UncompressedSize: size}
	}

	t.Run("uses the earliest timestamp across all runs and sums every section", func(t *testing.T) {
		task := newLogMergeTask(&compactionv2pb.TaskSpec{Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{section("logs/a", 500, 10), section("logs/a", 600, 20)}},
			{Sections: []*compactionv2pb.SectionRef{section("logs/b", 100, 30)}},
		}})

		require.Equal(t, logTaskStats{inputBytes: 60, oldestTimestamp: time.Unix(0, 100).UTC()}, task.logStats())
	})

	t.Run("reports the input bytes of the spec instead of the section sizes when it has them", func(t *testing.T) {
		task := newLogMergeTask(&compactionv2pb.TaskSpec{
			Runs:       []*compactionv2pb.RunRef{{Sections: []*compactionv2pb.SectionRef{section("logs/a", 500, 100), section("logs/b", 100, 50)}}},
			InputBytes: 70,
		})

		require.Equal(t, logTaskStats{inputBytes: 70, oldestTimestamp: time.Unix(0, 100).UTC()}, task.logStats())
	})
}

func TestFilterSourcePaths(t *testing.T) {
	entry := func(t *testing.T, path, minColumn, maxColumn string) indexpointers.IndexPointer {
		t.Helper()
		keys, err := postings.NewKeyRange(postings.Key{Kind: postings.KindLabel, ColumnName: minColumn}, postings.Key{Kind: postings.KindLabel, ColumnName: maxColumn})
		require.NoError(t, err)
		return indexpointers.IndexPointer{Path: path, RunID: 1, PostingKeys: keys}
	}

	t.Run("returns the paths in the order of the posting keys of the index objects", func(t *testing.T) {
		run := indexRun{entries: []indexpointers.IndexPointer{entry(t, "indexes/z", "zone", "zz"), entry(t, "indexes/a", "app", "az"), entry(t, "indexes/e", "env", "ez")}}
		paths, err := filterSourcePaths(run)
		require.NoError(t, err)
		require.Equal(t, []string{"indexes/a", "indexes/e", "indexes/z"}, paths)
	})

	t.Run("returns the path of a run of one index object without posting key bounds", func(t *testing.T) {
		paths, err := filterSourcePaths(indexRun{entries: []indexpointers.IndexPointer{{Path: "indexes/only"}}})
		require.NoError(t, err)
		require.Equal(t, []string{"indexes/only"}, paths)
	})

	t.Run("returns a corrupt index error when an index object of a run of several has no posting key bounds", func(t *testing.T) {
		run := indexRun{entries: []indexpointers.IndexPointer{entry(t, "indexes/a", "app", "az"), {Path: "indexes/b", RunID: 1}}}
		_, err := filterSourcePaths(run)
		require.ErrorIs(t, err, errCorruptIndex)
		require.ErrorContains(t, err, "indexes/b")
	})

	t.Run("returns a corrupt index error when the key ranges of two index objects overlap", func(t *testing.T) {
		run := indexRun{entries: []indexpointers.IndexPointer{entry(t, "indexes/a", "app", "env"), entry(t, "indexes/e", "env", "zone")}}
		_, err := filterSourcePaths(run)
		require.ErrorIs(t, err, errCorruptIndex)
		require.ErrorContains(t, err, "overlapping posting key ranges")
	})
}
