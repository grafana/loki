package distributor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShardTracker(t *testing.T) {
	st := NewShardTracker()
	st.SetLastShardNum("tenant 1", 0, 5)
	st.SetLastShardNum("tenant 1", 1, 6)

	st.SetLastShardNum("tenant 2", 0, 5)
	st.SetLastShardNum("tenant 2", 1, 6)

	require.Equal(t, 5, st.LastShardNum("tenant 1", 0))
	require.Equal(t, 6, st.LastShardNum("tenant 1", 1))

	require.Equal(t, 5, st.LastShardNum("tenant 2", 0))
	require.Equal(t, 6, st.LastShardNum("tenant 2", 1))
}
