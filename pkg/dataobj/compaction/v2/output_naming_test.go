package compactionv2

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

func TestCompactedIndexPath(t *testing.T) {
	t.Run("returns the key of the tenant below the indexes tenants directory", func(t *testing.T) {
		p, err := CompactedIndexPath("acme", strings.NewReader("hello"))
		require.NoError(t, err)
		require.Equal(t, "indexes/tenants/acme/ea/09ae9cc6768c50fcee903ed054556e5bfc8347907f12598aa24193", p)
	})

	t.Run("returns a key that IsCompactedObjectPath accepts", func(t *testing.T) {
		p, err := CompactedIndexPath("acme", strings.NewReader("hello"))
		require.NoError(t, err)
		require.True(t, dataobj.IsCompactedObjectPath(p))
	})
}

func TestCompactedLogObjectPath(t *testing.T) {
	t.Run("returns the key of the tenant below the objects tenants directory", func(t *testing.T) {
		p, err := CompactedLogObjectPath("acme", strings.NewReader("hello"))
		require.NoError(t, err)
		require.Equal(t, "objects/tenants/acme/ea/09ae9cc6768c50fcee903ed054556e5bfc8347907f12598aa24193", p)
	})

	t.Run("returns a key that IsCompactedObjectPath accepts", func(t *testing.T) {
		p, err := CompactedLogObjectPath("acme", strings.NewReader("hello"))
		require.NoError(t, err)
		require.True(t, dataobj.IsCompactedObjectPath(p))
	})
}
