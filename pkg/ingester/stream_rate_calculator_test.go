package ingester

import (
	"sort"
	"testing"

	"github.com/grafana/loki/pkg/logproto"

	"github.com/stretchr/testify/require"
)

func TestStreamRateCalculator(t *testing.T) {
	t.Run("it records rates for tenants and streams", func(t *testing.T) {
		calc := setupCalculator()

		for i := 0; i < 100; i++ {
			calc.Record("tenant 1", 1, 1, 100)
		}

		for i := 0; i < 100; i++ {
			calc.Record("tenant 2", 1, 1, 100)
		}

		calc.updateRates()
		rates := calc.Rates()
		require.Len(t, rates, 2)
		sort.Slice(rates, func(i, j int) bool {
			return rates[i].Tenant < rates[j].Tenant
		})

		require.Equal(t, []logproto.StreamRate{
			{StreamHash: 1, StreamHashNoShard: 1, Rate: 10000, Tenant: "tenant 1"},
			{StreamHash: 1, StreamHashNoShard: 1, Rate: 10000, Tenant: "tenant 2"},
		}, rates)

		calc.Remove("tenant 1", 1)
		calc.Remove("tenant 2", 1)
		calc.updateRates()

		require.Len(t, calc.Rates(), 0)
	})

	t.Run("it records rates using an exponential weighted average", func(t *testing.T) {
		calc := setupCalculator()

		calc.Record("tenant 1", 1, 1, 10000)
		calc.updateRates()
		rates := calc.Rates()
		require.Len(t, rates, 1)
		require.Equal(t, int64(10000), rates[0].Rate)

		calc.updateRates()
		rates = calc.Rates()
		require.Len(t, rates, 1)
		require.Equal(t, int64(8000), rates[0].Rate)

		calc.Record("tenant 1", 1, 1, 10000)
		calc.updateRates()
		rates = calc.Rates()
		require.Len(t, rates, 1)
		require.Equal(t, int64(8400), rates[0].Rate)
	})

	t.Run("it uses the larger sample without taking the average when there's a spike in load", func(t *testing.T) {
		calc := setupCalculator()

		calc.Record("tenant 1", 1, 1, 10000)
		calc.updateRates()
		rates := calc.Rates()
		require.Len(t, rates, 1)
		require.Equal(t, int64(10000), rates[0].Rate)

		calc.updateRates()
		rates = calc.Rates()
		require.Len(t, rates, 1)
		require.Equal(t, int64(8000), rates[0].Rate)

		calc.Record("tenant 1", 1, 1, 13000)
		calc.updateRates()
		rates = calc.Rates()
		require.Len(t, rates, 1)
		require.Equal(t, int64(13000), rates[0].Rate)
	})
}

// This is just the constructor without the async start so we can control it for tests
func setupCalculator() *StreamRateCalculator {
	calc := &StreamRateCalculator{
		size: defaultStripeSize,
		// Lookup pattern: tenant -> fingerprint -> rate
		samples:  make([]map[string]map[uint64]logproto.StreamRate, defaultStripeSize),
		locks:    make([]stripeLock, defaultStripeSize),
		stopchan: make(chan struct{}),
	}

	for i := 0; i < defaultStripeSize; i++ {
		calc.samples[i] = make(map[string]map[uint64]logproto.StreamRate)
	}

	return calc
}
