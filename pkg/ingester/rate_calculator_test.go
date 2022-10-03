package ingester

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRateCalculator(t *testing.T) {
	c := NewRateCalculator()
	defer c.Stop()

	for i := 0; i < 100; i++ {
		c.Record(50)
	}

	require.Eventually(t, func() bool {
		return c.Rate() == 5000
	}, 2*time.Second, 500*time.Millisecond)

	require.Eventually(t, func() bool {
		return c.Rate() == 0
	}, time.Second*2, 100*time.Millisecond)
}
