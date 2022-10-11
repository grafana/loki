package ingester

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStreamRateCalculator(t *testing.T) {
	calc := NewStreamRateCalculator()
	defer calc.Stop()

	for i := 0; i < 100; i++ {
		calc.Record(0, 100)
	}

	require.Eventually(t, func() bool {
		return calc.RateFor(0) == 10000
	}, 2*time.Second, 250*time.Millisecond)

	require.Eventually(t, func() bool {
		return calc.RateFor(0) == 0
	}, 2*time.Second, 250*time.Millisecond)
}
