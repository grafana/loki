package ingester

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStreamRateCalculator(t *testing.T) {
	for _, tc := range []struct {
		name          string
		newCalculator func() *StreamRateCalculator
		kafka         bool
	}{
		{name: "direct ingestion", newCalculator: NewStreamRateCalculator},
		{name: "Kafka ingestion", newCalculator: func() *StreamRateCalculator {
			return newKafkaStreamRateCalculator(3, "test-ingester")
		}, kafka: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calc := tc.newCalculator()
			defer calc.Stop()
			if tc.kafka {
				snapshot := calc.buildRatesResponse().Kafka
				require.NotNil(t, snapshot, "Kafka metadata must be present before the first sample")
				require.EqualValues(t, 3, snapshot.PartitionID)
				require.Equal(t, "test-ingester", snapshot.InstanceID)
				require.NotEmpty(t, snapshot.SessionID)
				require.False(t, snapshot.Ready)
			} else {
				require.Nil(t, calc.buildRatesResponse().Kafka)
			}

			for i := 0; i < 100; i++ {
				calc.Record(context.Background(), "tenant 1", 1, 1, 100)
			}

			for i := 0; i < 100; i++ {
				calc.Record(context.Background(), "tenant 2", 1, 1, 100)
			}

			require.Eventually(t, func() bool {
				rates := calc.Rates()
				sort.Slice(rates, func(i, j int) bool {
					return rates[i].Tenant < rates[j].Tenant
				})

				if len(rates) > 1 {
					return rates[0].Tenant == "tenant 1" && rates[0].Rate == 10000 && rates[0].Pushes == 100 &&
						rates[1].Tenant == "tenant 2" && rates[1].Rate == 10000 && rates[1].Pushes == 100
				}

				return false
			}, 2*time.Second, 250*time.Millisecond)

			require.Eventually(t, func() bool {
				rates := calc.Rates()
				return len(rates) == 0
			}, 2*time.Second, 250*time.Millisecond)
		})
	}
}

func BenchmarkStreamRateCalculatorUpdateRates(b *testing.B) {
	for _, mode := range []string{"native", "kafka"} {
		for _, count := range []int{1000, 10000, 100000, 1000000} {
			b.Run(fmt.Sprintf("%s/streams_%d", mode, count), func(b *testing.B) {
				var state *kafkaRateState
				if mode == "kafka" {
					state = newKafkaRateState(1, "test")
				}
				c := newStreamRateCalculator(state)

				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					b.StopTimer()
					for i := range count {
						c.Record(context.Background(), "tenant", uint64(i), uint64(i), 100)
					}
					b.StartTimer()
					c.updateRates()
				}
				b.StopTimer()
			})
		}
	}
}
