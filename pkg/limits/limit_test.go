package limits

import (
	"context"
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func TestLimitsChecker_UpdateRate(t *testing.T) {
	clock := quartz.NewMock(t)
	store, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 1, prometheus.NewRegistry())
	require.NoError(t, err)
	store.clock = clock

	checker := &limitsChecker{
		store: store,
		clock: clock,
	}

	t.Run("single rate update", func(t *testing.T) {
		req := &proto.UpdateRateRequest{
			Tenant: "tenant1",
			Rates: []*proto.StreamMetadata{
				{
					StreamHash: 0x1,
					TotalSize:  100,
				},
			},
		}

		resp, err := checker.UpdateRate(context.Background(), req)
		require.NoError(t, err)
		require.Len(t, resp.Results, 1)
		require.Equal(t, "1", resp.Results[0].Key)              // Stream hash as string
		require.Equal(t, int64(0), resp.Results[0].CurrentRate) // No previous data
	})

	t.Run("multiple rate updates", func(t *testing.T) {
		req := &proto.UpdateRateRequest{
			Tenant: "tenant1",
			Rates: []*proto.StreamMetadata{
				{
					StreamHash: 0x2,
					TotalSize:  200,
				},
				{
					StreamHash: 0x3,
					TotalSize:  300,
				},
			},
		}

		resp, err := checker.UpdateRate(context.Background(), req)
		require.NoError(t, err)
		require.Len(t, resp.Results, 2)

		// Check that we get results for both streams
		keys := make(map[string]bool)
		for _, result := range resp.Results {
			keys[result.Key] = true
		}
		require.True(t, keys["2"])
		require.True(t, keys["3"])
	})

	t.Run("rate calculation over time", func(t *testing.T) {
		// First update
		req1 := &proto.UpdateRateRequest{
			Tenant: "tenant2",
			Rates: []*proto.StreamMetadata{
				{
					StreamHash: 0x4,
					TotalSize:  100,
				},
			},
		}
		resp1, err := checker.UpdateRate(context.Background(), req1)
		require.NoError(t, err)
		require.Len(t, resp1.Results, 1)

		// Advance time and add more data
		clock.Advance(30 * time.Second)
		req2 := &proto.UpdateRateRequest{
			Tenant: "tenant2",
			Rates: []*proto.StreamMetadata{
				{
					StreamHash: 0x4,
					TotalSize:  200, // Additional 200 bytes
				},
			},
		}
		resp2, err := checker.UpdateRate(context.Background(), req2)
		require.NoError(t, err)
		require.Len(t, resp2.Results, 1)

		// Should now have a non-zero rate
		require.Greater(t, resp2.Results[0].CurrentRate, int64(0))
	})

	t.Run("rate calculation with time window", func(t *testing.T) {
		// Add data at different times to test rate calculation
		req1 := &proto.UpdateRateRequest{
			Tenant: "tenant3",
			Rates: []*proto.StreamMetadata{
				{
					StreamHash: 0x5,
					TotalSize:  100,
				},
			},
		}
		_, err := checker.UpdateRate(context.Background(), req1)
		require.NoError(t, err)

		// Advance time by 1 minute and add more data
		clock.Advance(time.Minute)
		req2 := &proto.UpdateRateRequest{
			Tenant: "tenant3",
			Rates: []*proto.StreamMetadata{
				{
					StreamHash: 0x5,
					TotalSize:  200,
				},
			},
		}
		resp2, err := checker.UpdateRate(context.Background(), req2)
		require.NoError(t, err)
		require.Len(t, resp2.Results, 1)

		// Should have a rate based on the data within the rate window
		require.Greater(t, resp2.Results[0].CurrentRate, int64(0))
	})

	t.Run("empty request", func(t *testing.T) {
		req := &proto.UpdateRateRequest{
			Tenant: "tenant4",
			Rates:  []*proto.StreamMetadata{},
		}

		resp, err := checker.UpdateRate(context.Background(), req)
		require.NoError(t, err)
		require.Empty(t, resp.Results)
	})
}

func TestLimitsChecker_UpdateRate_WithoutProducer(t *testing.T) {
	clock := quartz.NewMock(t)
	store, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 1, prometheus.NewRegistry())
	require.NoError(t, err)
	store.clock = clock

	// Test without producer (should not fail)
	checker := &limitsChecker{
		store:    store,
		clock:    clock,
		producer: nil, // No producer
	}

	req := &proto.UpdateRateRequest{
		Tenant: "tenant1",
		Rates: []*proto.StreamMetadata{
			{
				StreamHash: 0x1,
				TotalSize:  100,
			},
		},
	}

	resp, err := checker.UpdateRate(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	require.Equal(t, "1", resp.Results[0].Key)
	require.Equal(t, int64(0), resp.Results[0].CurrentRate) // No previous data
}
