package physical

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func TestSortObjectCloneIsDeepCopy(t *testing.T) {
	original := &SortObject{
		NodeID:           ulid.Make(),
		SourceObjectPath: "objects/aa/bb",
		SortSchema:       []string{"label:app"},
	}

	cloned := original.Clone().(*SortObject)
	require.NotEqual(t, original.ID(), cloned.ID())
	cloned.SortSchema[0] = "label:cluster"
	require.Equal(t, "label:app", original.SortSchema[0])
	require.Equal(t, original.SourceObjectPath, cloned.SourceObjectPath)
}
