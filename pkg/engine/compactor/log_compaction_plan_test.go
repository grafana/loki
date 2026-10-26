package compactor

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
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

func TestLogCompactionPlanCheckRecordCounts(t *testing.T) {
	a0 := v2.SectionRecords{ObjectPath: "logs/a", SectionIndex: 0, Records: 10}
	b0 := v2.SectionRecords{ObjectPath: "logs/b", SectionIndex: 0, Records: 6}
	splitMerge := logMergeGroup{tasks: []int{0, 1}, sections: []sectionKey{{"logs/a", 0}, {"logs/b", 0}}}
	counted := func(kept int64, sections ...v2.SectionRecords) v2.ResultArtifact {
		return v2.ResultArtifact{Path: "index", LogCounts: &v2.LogMergeCounts{KeptRecords: kept, Sections: sections}}
	}

	tests := []struct {
		name      string
		merges    []logMergeGroup
		artifacts []v2.ResultArtifact
		wantErr   string
	}{
		{
			name:      "passes when the tasks of a split merge write every record once",
			merges:    []logMergeGroup{splitMerge},
			artifacts: []v2.ResultArtifact{counted(7, a0, b0), counted(9, a0)},
		},
		{
			name:      "fails when the tasks write fewer records than the sections hold",
			merges:    []logMergeGroup{splitMerge},
			artifacts: []v2.ResultArtifact{counted(7, a0, b0), counted(8, a0)},
			wantErr:   "wrote 15 records, but its 2 input sections hold 16",
		},
		{
			name:      "fails when the tasks write more records than the sections hold",
			merges:    []logMergeGroup{splitMerge},
			artifacts: []v2.ResultArtifact{counted(10, a0, b0), counted(10, a0)},
			wantErr:   "wrote 20 records, but its 2 input sections hold 16",
		},
		{
			name:      "fails when no task read a section of the merge",
			merges:    []logMergeGroup{splitMerge},
			artifacts: []v2.ResultArtifact{counted(5, a0), counted(5, a0)},
			wantErr:   "no task of the merge read logs section logs/b#0",
		},
		{
			name:      "fails when tasks report different record counts for one section",
			merges:    []logMergeGroup{splitMerge},
			artifacts: []v2.ResultArtifact{counted(7, a0, b0), counted(9, v2.SectionRecords{ObjectPath: "logs/a", Records: 11})},
			wantErr:   "tasks report 10 and 11 records for logs section logs/a#0",
		},
		{
			name:      "fails when a task of a split merge reports no counts",
			merges:    []logMergeGroup{splitMerge},
			artifacts: []v2.ResultArtifact{counted(7, a0, b0), {Path: "index"}},
			wantErr:   "reported no record counts",
		},
		{
			name:      "passes when the only task of a merge reports no counts",
			merges:    []logMergeGroup{{tasks: []int{0}, sections: []sectionKey{{"logs/a", 0}}}},
			artifacts: []v2.ResultArtifact{{Path: "index"}},
		},
		{
			name:      "ignores tasks outside every merge",
			merges:    []logMergeGroup{{tasks: []int{0}, sections: []sectionKey{{"logs/a", 0}}}},
			artifacts: []v2.ResultArtifact{counted(10, a0), {Path: "filtered-index"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan := logCompactionPlan{tasks: make([]logCompactionTask, len(tc.artifacts)), merges: tc.merges}
			err := plan.checkRecordCounts(tc.artifacts)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}
