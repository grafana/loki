package compactionv2

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompactedIndexPath(t *testing.T) {
	p, err := CompactedIndexPath("acme", strings.NewReader("hello"))
	require.NoError(t, err)
	require.Equal(t, "indexes/tenants/acme/ea/09ae9cc6768c50fcee903ed054556e5bfc8347907f12598aa24193", p)
}

func TestCompactedLogObjectPath(t *testing.T) {
	p, err := CompactedLogObjectPath("acme", strings.NewReader("hello"))
	require.NoError(t, err)
	require.Equal(t, "objects/tenants/acme/ea/09ae9cc6768c50fcee903ed054556e5bfc8347907f12598aa24193", p)
}
