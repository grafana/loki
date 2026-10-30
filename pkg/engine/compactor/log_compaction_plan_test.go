package compactor

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	compactionv2pb "github.com/grafana/loki/v4/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v4/pkg/dataobj/metastore"
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
			indexFilterTask{entry: metastore.TableOfContentsEntry{StartTime: now.Add(-48 * time.Hour)}},
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
}
