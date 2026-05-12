package workflow

import (
	"math"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

func TestAdmissionControl_getBucket(t *testing.T) {
	ac := newAdmissionControl(32, math.MaxInt64, math.MaxInt64)

	t.Run("Task without a DataObjScan node is considered an 'other' task", func(t *testing.T) {
		fragment := dag.Graph[physical.Node]{}
		task := &Task{
			ULID:     ulid.Make(),
			Fragment: physical.FromGraph(fragment),
		}
		bucket := ac.typeFor(task)
		require.Equal(t, taskTypeOther, bucket)
	})

	t.Run("Task with a DataObjScan node is considered an 'scan' task", func(t *testing.T) {
		fragment := dag.Graph[physical.Node]{}
		fragment.Add(&physical.DataObjScan{})

		task := &Task{
			ULID:     ulid.Make(),
			Fragment: physical.FromGraph(fragment),
		}
		ty := ac.typeFor(task)
		require.Equal(t, taskTypeScan, ty)
	})

	t.Run("Task with a PointersScan node is considered an 'scan' task", func(t *testing.T) {
		fragment := dag.Graph[physical.Node]{}
		fragment.Add(&physical.PointersScan{})

		task := &Task{
			ULID:     ulid.Make(),
			Fragment: physical.FromGraph(fragment),
		}
		ty := ac.typeFor(task)
		require.Equal(t, taskTypeScan, ty)
	})
}

// TestAdmissionControl_CompactionLaneWired verifies the taskTypeCompaction
// lane is allocated with the requested capacity, is reachable via get(),
// and appears in the groupByType map even when no tasks are provided. The
// underlying semaphore is library code and is not re-tested here.
func TestAdmissionControl_CompactionLaneWired(t *testing.T) {
	const compactionCap int64 = 5
	ac := newAdmissionControl(8, 8, compactionCap)

	lane := ac.get(taskTypeCompaction)
	require.NotNil(t, lane)
	require.Equal(t, compactionCap, lane.capacity)

	groups := ac.groupByType(nil)
	require.Contains(t, groups, taskTypeScan)
	require.Contains(t, groups, taskTypeOther)
	require.Contains(t, groups, taskTypeCompaction)
	require.Empty(t, groups[taskTypeCompaction])
}

// TestAdmissionControl_typeFor_CompactionMerge verifies that any task whose
// fragment contains a CompactionMerge node is classified into the compaction
// lane so its parallelism is governed by MaxRunningCompactionTasks.
func TestAdmissionControl_typeFor_CompactionMerge(t *testing.T) {
	g := dag.Graph[physical.Node]{}
	g.Add(&physical.CompactionMerge{NodeID: ulid.Make(), Tenant: "tenant-29"})
	task := &Task{Fragment: physical.FromGraph(g)}

	ac := newAdmissionControl(0, 0, 0)
	require.Equal(t, taskTypeCompaction, ac.typeFor(task))
}

// TestAdmissionControl_typeFor_IndexConsolidate verifies that any task whose
// fragment contains an IndexConsolidate node is classified into the compaction
// lane.
func TestAdmissionControl_typeFor_IndexConsolidate(t *testing.T) {
	ac := newAdmissionControl(math.MaxInt64, math.MaxInt64, math.MaxInt64)

	fragment := dag.Graph[physical.Node]{}
	fragment.Add(&physical.IndexConsolidate{NodeID: ulid.Make()})

	task := &Task{
		ULID:     ulid.Make(),
		Fragment: physical.FromGraph(fragment),
	}
	require.Equal(t, taskTypeCompaction, ac.typeFor(task))
}
