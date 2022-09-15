package ingester

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRateCalculator(t *testing.T) {
	c := NewRateCalculator()

	for i := 0; i < 100; i++ {
		c.Record(50)
	}

	require.Eventually(t, func() bool {
		return c.Rate() == 5000
	}, 500*time.Millisecond, time.Millisecond)
}
