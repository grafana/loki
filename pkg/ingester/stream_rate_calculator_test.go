package ingester

import (
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStreamRateCalculator(t *testing.T) {
	calc := NewStreamRateCalculator()
	defer calc.Stop()

	for i := 0; i < 100; i++ {
		calc.Record("tenant 1", 1, 1, 100)
	}

	for i := 0; i < 100; i++ {
		calc.Record("tenant 2", 1, 1, 100)
	}

	require.Eventually(t, func() bool {
		rates := calc.Rates()
		sort.Slice(rates, func(i, j int) bool {
			return rates[i].Tenant < rates[j].Tenant
		})

		if len(rates) > 1 {
			return rates[0].Tenant == "tenant 1" && rates[0].Rate == 10000 &&
				rates[1].Tenant == "tenant 2" && rates[1].Rate == 10000
		}

		return false
	}, 2*time.Second, 250*time.Millisecond)

	require.Eventually(t, func() bool {
		rates := calc.Rates()
		return len(rates) == 0
	}, 2*time.Second, 250*time.Millisecond)
}

func TestStreamRateCalculatorDecaying(t *testing.T) {
	calc := NewStreamRateCalculator()
	defer calc.Stop()

	expectedRates := []int64{128 * kilobyte, 64 * kilobyte, 32 * kilobyte, 16 * kilobyte, 8 * kilobyte, 4 * kilobyte, 2 * kilobyte, 1 * kilobyte}

	initialRate := expectedRates[0]
	calc.Record("tenant 1", 1, 1, int(initialRate))

	for _, r := range expectedRates {
		require.Eventually(t, func() bool {
			rates := calc.Rates()
			if len(rates) == 1 {
				return rates[0].Tenant == "tenant 1" && rates[0].Rate == r
			}

			return false
		}, 2*time.Second, 250*time.Millisecond)
	}

	require.Eventually(t, func() bool {
		rates := calc.Rates()
		return len(rates) == 0
	}, 2*time.Second, 250*time.Millisecond)
}
