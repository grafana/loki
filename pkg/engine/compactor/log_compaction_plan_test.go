package compactor

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

func TestLogCompactionPlanLogInputs(t *testing.T) {
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	sortTask := func(oldest time.Time, bytes uint64) sortObjectTask {
		return sortObjectTask{entry: metastore.TableOfContentsEntry{StartTime: oldest}, bytes: bytes}
	}

	t.Run("yields the bytes and oldest timestamp of each task that reads log data", func(t *testing.T) {
		plan := logCompactionPlan{tasks: []logCompactionTask{
			sortTask(now.Add(-30*time.Minute), 16),
			sortTask(now.Add(-12*time.Hour), 100),
		}}

		require.Equal(t, []logInput{
			{bytes: 16, oldestTimestamp: now.Add(-30 * time.Minute)},
			{bytes: 100, oldestTimestamp: now.Add(-12 * time.Hour)},
		}, slices.Collect(plan.logInputs()))
	})

	t.Run("skips an IndexFilter task", func(t *testing.T) {
		plan := logCompactionPlan{tasks: []logCompactionTask{
			indexFilterTask{entry: metastore.TableOfContentsEntry{StartTime: now.Add(-48 * time.Hour)}},
		}}

		require.Empty(t, slices.Collect(plan.logInputs()))
	})

	t.Run("stops yielding when the caller stops iterating", func(t *testing.T) {
		plan := logCompactionPlan{tasks: []logCompactionTask{
			sortTask(now, 1),
			sortTask(now, 2),
		}}

		var seen []logInput
		for input := range plan.logInputs() {
			seen = append(seen, input)
			break
		}
		require.Len(t, seen, 1)
	})
}

func TestLogMergeTaskLogInput(t *testing.T) {
	section := func(path string, minTS int64, size int64) *compactionv2pb.SectionRef {
		return &compactionv2pb.SectionRef{ObjectPath: path, MinTimestamp: minTS, MaxTimestamp: minTS + 10, UncompressedSize: size}
	}

	t.Run("uses the earliest timestamp across all runs and sums every section", func(t *testing.T) {
		task := newLogMergeTask(&compactionv2pb.TaskSpec{Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{section("logs/a", 500, 10), section("logs/a", 600, 20)}},
			{Sections: []*compactionv2pb.SectionRef{section("logs/b", 100, 30)}},
		}})

		require.Equal(t, logInput{bytes: 60, oldestTimestamp: time.Unix(0, 100).UTC()}, task.logInput())
	})
}
