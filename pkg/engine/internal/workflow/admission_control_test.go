package workflow

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func TestAdmissionControl_getBucket(t *testing.T) {
	ac := newAdmissionControl(t.Context(), defaultAdmissionControlOpts)

	t.Run("Task that has category TaskIsSharded is considered 'scan'", func(t *testing.T) {
		task := &Task{
			ULID: ulid.Make(),
		}
		task.SetCategory(TaskIsSharded)
		bucket := ac.tokenBucketFor(task)
		require.Equal(t, ac.scan, bucket)
	})

	t.Run("Task that does not have category TaskIsSharded is considered 'other'", func(t *testing.T) {
		task := &Task{
			ULID: ulid.Make(),
		}
		task.UnsetCategory(TaskIsSharded)
		bucket := ac.tokenBucketFor(task)
		require.Equal(t, ac.other, bucket)
	})
}
