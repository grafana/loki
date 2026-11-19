package compactor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
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
	plan := indexMergePlan{group: group, tasks: []*compactionv2pb.TaskSpec{
		task("indexes/b", "indexes/a"),
		task("indexes/c", "indexes/a"),
	}}

	t.Run("reports no work for the zero plan", func(t *testing.T) {
		require.False(t, indexMergePlan{}.hasWork())
	})

	t.Run("reports work for a plan with tasks", func(t *testing.T) {
		require.True(t, plan.hasWork())
	})

	t.Run("returns one physical plan per task that merges the sections of the task, in task order", func(t *testing.T) {
		plans := plan.physicalPlans("acme", window)
		require.Len(t, plans, len(plan.tasks))
		for i, physicalPlan := range plans {
			root, err := physicalPlan.Root()
			require.NoError(t, err)
			node, ok := root.(*physical.IndexMerge)
			require.True(t, ok, "plan %d root is %T", i, root)
			require.Len(t, node.Runs, 1)
			require.Equal(t, plan.tasks[i].Runs[0].Sections, node.Runs[0].Sections)
		}
	})

	t.Run("returns the time range of the source indexes and the layout of the group for each task, in task order", func(t *testing.T) {
		planned, err := plan.plannedRuns()
		require.NoError(t, err)
		shardBuckets := indexpointers.NewShardBuckets(layout.ShardLayout())
		require.Equal(t, []plannedRun{
			{timeRange: newTimeRange(at(1), at(5)), shardBuckets: shardBuckets},
			{timeRange: newTimeRange(at(1), at(4)), shardBuckets: shardBuckets},
		}, planned)
	})

	t.Run("returns an error when a task reads an index outside the group", func(t *testing.T) {
		outside := indexMergePlan{group: group, tasks: []*compactionv2pb.TaskSpec{task("indexes/a", "indexes/other")}}

		_, err := outside.plannedRuns()
		require.ErrorContains(t, err, `task 0: task references unknown index "indexes/other"`)
	})

	t.Run("returns the source paths of every task sorted and without duplicates", func(t *testing.T) {
		require.Equal(t, []string{"indexes/a", "indexes/b", "indexes/c"}, plan.sourcePaths())
	})
}
