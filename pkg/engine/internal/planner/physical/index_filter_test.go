package physical

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func TestIndexFilterClone(t *testing.T) {
	t.Run("copies every field and mints a new ID", func(t *testing.T) {
		original := &IndexFilter{
			NodeID:          ulid.Make(),
			Tenant:          "acme",
			SourceIndexPath: "indexes/aa/source",
			ObjectPaths:     []string{"logs/a", "logs/b"},
		}

		cloned := original.Clone().(*IndexFilter)
		require.NotEqual(t, original.ID(), cloned.ID())
		require.Equal(t, original.Tenant, cloned.Tenant)
		require.Equal(t, original.SourceIndexPath, cloned.SourceIndexPath)
		require.Equal(t, original.ObjectPaths, cloned.ObjectPaths)
	})

	t.Run("does not share object paths with the original", func(t *testing.T) {
		original := &IndexFilter{ObjectPaths: []string{"logs/a"}}

		cloned := original.Clone().(*IndexFilter)
		cloned.ObjectPaths[0] = "logs/changed"
		require.Equal(t, "logs/a", original.ObjectPaths[0])
	})
}
