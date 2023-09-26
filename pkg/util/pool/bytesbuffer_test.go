package pool

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_ZeroBuffer(t *testing.T) {
	p := NewBuffer(2, 10, 2)
	require.Equal(t, 0, p.Get(1).Len())
	require.Equal(t, 0, p.Get(1).Len())
	require.Equal(t, 0, p.Get(2).Len())
	require.Equal(t, 0, p.Get(2).Len())
	require.Equal(t, 0, p.Get(20).Len())
	require.Equal(t, 0, p.Get(20).Len())
}
