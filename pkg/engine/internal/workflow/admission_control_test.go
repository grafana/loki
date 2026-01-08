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
	ac := newAdmissionControl(32, math.MaxInt64)

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
