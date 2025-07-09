package chunkenc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIsOutOfOrderErr(t *testing.T) {
	now := time.Now()

	for _, err := range []error{ErrOutOfOrder, ErrTooFarBehind(now, now)} {
		require.Equal(t, true, IsOutOfOrderErr(err))
	}
}
