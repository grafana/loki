package workflow

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

func TestAdmissionControl_getBucket(t *testing.T) {
	ac := newAdmissionControl(defaultAdmissionControlOpts)

	t.Run("Task without a DataObjScan node is considered an 'other' task", func(t *testing.T) {
		fragment := dag.Graph[physical.Node]{}
		task := &Task{
			ULID:     ulid.Make(),
			Fragment: physical.FromGraph(fragment),
		}
		bucket := ac.tokenBucketFor(task)
		require.Equal(t, ac.other, bucket)
	})

	t.Run("Task with a DataObjScan node is considered an 'scan' task", func(t *testing.T) {
		fragment := dag.Graph[physical.Node]{}
		fragment.Add(&physical.DataObjScan{})

		task := &Task{
			ULID:     ulid.Make(),
			Fragment: physical.FromGraph(fragment),
		}
		bucket := ac.tokenBucketFor(task)
		require.Equal(t, ac.scan, bucket)
	})
}
