package compactor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

func TestIndexMergePlan(t *testing.T) {
	window := imWindow()
	at := func(hours int) time.Time { return window.Add(time.Duration(hours) * time.Hour) }
	layout := streams.NewLayout(streams.DefaultShardLayout(), []string{"label:service_name"})
	group := newLayoutGroup(layout, true)
	for _, entry := range []indexpointers.IndexPointer{
		{Path: "indexes/a", StartTs: at(1), EndTs: at(2)},
		{Path: "indexes/b", StartTs: at(3), EndTs: at(5)},
		{Path: "indexes/c", StartTs: at(2), EndTs: at(4)},
	} {
		group.add(indexRunRead{run: runOfEntry(entry)})
	}
	task := func(paths ...string) *compactionv2pb.TaskSpec {
		run := &compactionv2pb.RunRef{}
		for _, path := range paths {
			run.Sections = append(run.Sections, &compactionv2pb.SectionRef{ObjectPath: path})
		}
		return &compactionv2pb.TaskSpec{Runs: []*compactionv2pb.RunRef{run}}
	}
	whole := func(spec *compactionv2pb.TaskSpec) splitIndexMerge {
		return splitIndexMerge{spec: spec, tasks: []indexMergeTask{{spec: spec}}}
	}
	plan := indexMergePlan{group: group, merges: []splitIndexMerge{
		whole(task("indexes/b", "indexes/a")),
		whole(task("indexes/c", "indexes/a")),
	}}
	envKey := postings.Key{Kind: postings.KindLabel, ColumnName: "env"}
	belowEnv, err := postings.NewKeySpan(nil, &envKey)
	require.NoError(t, err)
	fromEnv, err := postings.NewKeySpan(&envKey, nil)
	require.NoError(t, err)
	splitSpec := task("indexes/a", "indexes/b")
	split := indexMergePlan{group: group, merges: []splitIndexMerge{
		{spec: splitSpec, tasks: []indexMergeTask{
			{spec: task("indexes/a"), keySpan: belowEnv},
			{spec: task("indexes/b"), keySpan: fromEnv},
		}},
		whole(task("indexes/c")),
	}}
	artifact := func(t *testing.T, path, minColumn, maxColumn string) v2.ResultArtifact {
		t.Helper()
		keys, err := postings.NewKeyRange(postings.Key{Kind: postings.KindLabel, ColumnName: minColumn}, postings.Key{Kind: postings.KindLabel, ColumnName: maxColumn})
		require.NoError(t, err)
		return v2.ResultArtifact{Path: path, PostingKeys: keys}
	}

	t.Run("reports no work for the zero plan", func(t *testing.T) {
		require.False(t, indexMergePlan{}.hasWork())
	})

	t.Run("reports work for a plan with tasks", func(t *testing.T) {
		require.True(t, plan.hasWork())
	})

	t.Run("counts the tasks of every merge", func(t *testing.T) {
		require.Equal(t, 3, split.taskCount())
	})

	t.Run("returns one physical plan per task that merges the sections and key span of the task, in merge and task order", func(t *testing.T) {
		plans := split.physicalPlans("acme", window)
		want := []indexMergeTask{split.merges[0].tasks[0], split.merges[0].tasks[1], split.merges[1].tasks[0]}
		require.Len(t, plans, len(want))
		for i, physicalPlan := range plans {
			root, err := physicalPlan.Root()
			require.NoError(t, err)
			node, ok := root.(*physical.IndexMerge)
			require.True(t, ok, "plan %d root is %T", i, root)
			require.Len(t, node.Runs, 1)
			require.Equal(t, want[i].spec.Runs[0].Sections, node.Runs[0].Sections)
			require.Equal(t, want[i].keySpan, node.KeySpan)
		}
	})

	t.Run("returns one planned run per merge with the time range of every source of the merge", func(t *testing.T) {
		planned, err := split.plannedRuns()
		require.NoError(t, err)
		shardBuckets := indexpointers.NewShardBuckets(layout.ShardLayout())
		require.Equal(t, []plannedRun{
			{timeRange: newTimeRange(at(1), at(5)), shardBuckets: shardBuckets},
			{timeRange: newTimeRange(at(2), at(4)), shardBuckets: shardBuckets},
		}, planned)
	})

	t.Run("joins the artifacts of the tasks of each merge in task order", func(t *testing.T) {
		low, high, other := artifact(t, "out/0", "app", "cz"), artifact(t, "out/1", "env", "zone"), artifact(t, "out/2", "app", "zone")
		joined, err := split.joinArtifacts([][]v2.ResultArtifact{{low}, {high}, {other}})
		require.NoError(t, err)
		require.Equal(t, [][]v2.ResultArtifact{{low, high}, {other}}, joined)
	})

	t.Run("returns an error when it gets the artifacts of a different number of tasks", func(t *testing.T) {
		_, err := split.joinArtifacts([][]v2.ResultArtifact{{artifact(t, "out/0", "app", "cz")}})
		require.ErrorContains(t, err, "got the artifacts of 1 index-compaction tasks for 3 tasks")
	})

	t.Run("returns an error when a task of a merge returns keys outside its key span", func(t *testing.T) {
		_, err := split.joinArtifacts([][]v2.ResultArtifact{{artifact(t, "out/0", "app", "zone")}, {artifact(t, "out/1", "env", "zone")}, {artifact(t, "out/2", "app", "zone")}})
		require.ErrorContains(t, err, "index merge 0: task 0: artifact out/0 holds posting keys")
	})

	t.Run("returns the time range of the source indexes and the layout of the group for each merge, in merge order", func(t *testing.T) {
		planned, err := plan.plannedRuns()
		require.NoError(t, err)
		shardBuckets := indexpointers.NewShardBuckets(layout.ShardLayout())
		require.Equal(t, []plannedRun{
			{timeRange: newTimeRange(at(1), at(5)), shardBuckets: shardBuckets},
			{timeRange: newTimeRange(at(1), at(4)), shardBuckets: shardBuckets},
		}, planned)
	})

	t.Run("returns an error when a task reads an index outside the group", func(t *testing.T) {
		outside := indexMergePlan{group: group, merges: []splitIndexMerge{whole(task("indexes/a", "indexes/other"))}}

		_, err := outside.plannedRuns()
		require.ErrorContains(t, err, `merge 0: task references unknown index "indexes/other"`)
	})

	t.Run("returns the source paths of every merge sorted and without duplicates", func(t *testing.T) {
		require.Equal(t, []string{"indexes/a", "indexes/b", "indexes/c"}, plan.sourcePaths())
		require.Equal(t, []string{"indexes/a", "indexes/b", "indexes/c"}, split.sourcePaths())
	})
}
