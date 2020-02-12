package distributor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLimits(t *testing.T) {
	require.Equal(t, 0, constLimits(0).MaxLineSize("a"))
	require.Equal(t, 2, constLimits(2).MaxLineSize("a"))
	require.Equal(t, 1,
		PriorityLimits([]Limits{
			constLimits(0),
			constLimits(1),
		}).MaxLineSize("a"),
	)
}
