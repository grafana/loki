package workflow

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

func TestAdmissionControl_getBucket(t *testing.T) {
	ac := newAdmissionControl(t.Context(), defaultAdmissionControlOpts)

	t.Run("Task without Sources is considered a 'scan' task", func(t *testing.T) {
		task := &Task{
			ULID: ulid.Make(),
		}
		bucket := ac.tokenBucketFor(task)
		require.Equal(t, ac.scan, bucket)
	})

	t.Run("Task with Sources is considered an 'other' task", func(t *testing.T) {
		task := &Task{
			ULID: ulid.Make(),
			Sources: map[physical.Node][]*Stream{
				&physical.ScanSet{}: {
					{ULID: ulid.Make()},
				},
			},
		}
		bucket := ac.tokenBucketFor(task)
		require.Equal(t, ac.other, bucket)
	})
}
