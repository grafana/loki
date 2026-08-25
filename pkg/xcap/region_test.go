package xcap

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegion_Record(t *testing.T) {
	t.Run("first observation of a stat should be stored", func(t *testing.T) {
		ctx, _ := NewCapture(context.Background(), nil)
		_, region := StartRegion(ctx, "test")

		bytesRead := NewStatisticInt64("bytes.read", AggregationTypeSum)
		obs := bytesRead.Observe(1024)

		region.Record(obs)

		require.Len(t, region.observations, 1, "should have one observation")

		agg, ok := region.observations[bytesRead.Key()]
		require.True(t, ok, "observation should exist for statistic")
		require.Equal(t, int64(1024), agg.Value.(int64))
		require.Equal(t, 1, agg.Count)
		require.Equal(t, bytesRead, agg.Statistic)
	})

	t.Run("multiple observations of same stat should be aggregated", func(t *testing.T) {
		ctx, _ := NewCapture(context.Background(), nil)
		_, region := StartRegion(ctx, "test")

		bytesRead := NewStatisticInt64("bytes.read", AggregationTypeSum)

		region.Record(bytesRead.Observe(1024))
		region.Record(bytesRead.Observe(2048))
		region.Record(bytesRead.Observe(512))

		require.Len(t, region.observations, 1, "should have one aggregated observation")

		agg, ok := region.observations[bytesRead.Key()]
		require.True(t, ok, "observation should exist for statistic")
		require.Equal(t, int64(3584), agg.Value.(int64), "values should be summed")
		require.Equal(t, 3, agg.Count, "count should be 3")
	})

	t.Run("multiple stats and multiple observations", func(t *testing.T) {
		ctx, _ := NewCapture(context.Background(), nil)
		_, region := StartRegion(ctx, "test")

		bytesRead := NewStatisticInt64("bytes.read", AggregationTypeSum)
		latency := NewStatisticFloat64("latency.ms", AggregationTypeMin)
		success := NewStatisticFlag("success")

		// Record multiple observations for bytes.read (sum)
		region.Record(bytesRead.Observe(1024))
		region.Record(bytesRead.Observe(2048))
		region.Record(bytesRead.Observe(512))

		// Record multiple observations for latency (min)
		region.Record(latency.Observe(10.5))
		region.Record(latency.Observe(5.2))
		region.Record(latency.Observe(15.8))

		// Record multiple observations for success flag (max)
		region.Record(success.Observe(false))
		region.Record(success.Observe(false))
		region.Record(success.Observe(true))

		require.Len(t, region.observations, 3, "should have three different statistics")

		// Verify bytes.read aggregation (sum)
		bytesAgg, ok := region.observations[bytesRead.Key()]
		require.True(t, ok)
		require.Equal(t, int64(3584), bytesAgg.Value.(int64))
		require.Equal(t, 3, bytesAgg.Count)

		// Verify latency aggregation (min)
		latencyAgg, ok := region.observations[latency.Key()]
		require.True(t, ok)
		require.Equal(t, float64(5.2), latencyAgg.Value.(float64))
		require.Equal(t, 3, latencyAgg.Count)

		// Verify success flag aggregation (max)
		successAgg, ok := region.observations[success.Key()]
		require.True(t, ok)
		require.Equal(t, true, successAgg.Value.(bool))
		require.Equal(t, 3, successAgg.Count)
	})

	t.Run("record after End is ignored", func(t *testing.T) {
		ctx, _ := NewCapture(context.Background(), nil)
		_, region := StartRegion(ctx, "test")

		bytesRead := NewStatisticInt64("bytes.read", AggregationTypeSum)
		region.Record(bytesRead.Observe(1024))
		region.End()

		// Record after End should be ignored
		region.Record(bytesRead.Observe(2048))

		key := bytesRead.Key()
		agg := region.observations[key]
		require.Equal(t, int64(1024), agg.Value.(int64), "value should not change after End")
		require.Equal(t, 1, agg.Count, "count should not change after End")
	})
}
