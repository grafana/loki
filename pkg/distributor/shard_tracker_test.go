package distributor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShardTracker(t *testing.T) {
	st := NewShardTracker()
	st.SetLastShardNum(0, 5)
	st.SetLastShardNum(1, 6)

	require.Equal(t, 5, st.LastShardNum(0))
	require.Equal(t, 6, st.LastShardNum(1))
}
