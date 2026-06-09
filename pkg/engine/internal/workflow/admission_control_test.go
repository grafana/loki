package workflow

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

func TestAdmissionControl_getBucket(t *testing.T) {
	t.Run("Task without a DataObjScan node is considered an 'other' task", func(t *testing.T) {
		fragment := dag.Graph[physical.Node]{}
		task := &Task{
			ULID:     ulid.Make(),
			Fragment: physical.FromGraph(fragment),
		}
		bucket := task.Type()
		require.Equal(t, TaskTypeOther, bucket)
	})

	t.Run("Task with a DataObjScan node is considered an 'scan' task", func(t *testing.T) {
		fragment := dag.Graph[physical.Node]{}
		fragment.Add(&physical.DataObjScan{})

		task := &Task{
			ULID:     ulid.Make(),
			Fragment: physical.FromGraph(fragment),
		}
		ty := task.Type()
		require.Equal(t, TaskTypeScan, ty)
	})

	t.Run("Task with a PointersScan node is considered an 'scan' task", func(t *testing.T) {
		fragment := dag.Graph[physical.Node]{}
		fragment.Add(&physical.PointersScan{})

		task := &Task{
			ULID:     ulid.Make(),
			Fragment: physical.FromGraph(fragment),
		}
		ty := task.Type()
		require.Equal(t, TaskTypeScan, ty)
	})
}

// TestAdmissionControl_CompactionLaneWired verifies the taskTypeCompaction
// lane is allocated with the requested capacity, is reachable via get(),
// and appears in the groupByType map even when no tasks are provided. The
// underlying semaphore is library code and is not re-tested here.
func TestAdmissionControl_CompactionLaneWired(t *testing.T) {
	const compactionCap int64 = 5
	ac := newAdmissionControl(8, 8, compactionCap)

	lane := ac.get(TaskTypeCompaction)
	require.NotNil(t, lane)
	require.Equal(t, compactionCap, lane.capacity)

	groups := ac.groupByType(nil)
	require.Contains(t, groups, TaskTypeScan)
	require.Contains(t, groups, TaskTypeOther)
	require.Contains(t, groups, TaskTypeCompaction)
	require.Empty(t, groups[TaskTypeCompaction])
}

func TestAdmissionControl_typeFor_IndexMerge(t *testing.T) {
	// A task whose Fragment contains an IndexMerge node must be classified
	// as taskTypeCompaction so the compactor's parallelism is governed by
	// MaxRunningCompactionTasks.
	g := dag.Graph[physical.Node]{}
	g.Add(&physical.IndexMerge{NodeID: ulid.Make(), Tenant: "tenant-29"})
	task := &Task{Fragment: physical.FromGraph(g)}

	require.Equal(t, TaskTypeCompaction, task.Type())
}

func TestAdmissionControl_typeFor_LogMerge(t *testing.T) {
	// A task whose Fragment contains a LogMerge node must be classified
	// as taskTypeCompaction.
	g := dag.Graph[physical.Node]{}
	g.Add(&physical.LogMerge{NodeID: ulid.Make(), Tenant: "tenant-29"})
	task := &Task{Fragment: physical.FromGraph(g)}

	require.Equal(t, TaskTypeCompaction, task.Type())
}
