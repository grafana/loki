package xcap

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAggregatedObservation_Record(t *testing.T) {
	t.Run("sum aggregation int64", func(t *testing.T) {
		stat := NewStatisticInt64("value", AggregationTypeSum)
		agg := &AggregatedObservation{
			Statistic: stat,
			Value:     int64(0),
			Count:     0,
		}

		agg.Record(stat.Observe(10))
		agg.Record(stat.Observe(20))
		agg.Record(stat.Observe(30))

		require.Equal(t, 3, agg.Count)
		require.Equal(t, int64(60), agg.Value.(int64))
	})

	t.Run("sum aggregation float64", func(t *testing.T) {
		stat := NewStatisticFloat64("value", AggregationTypeSum)
		agg := &AggregatedObservation{
			Statistic: stat,
			Value:     float64(0),
			Count:     0,
		}

		agg.Record(stat.Observe(10.5))
		agg.Record(stat.Observe(20.3))
		agg.Record(stat.Observe(30.2))

		require.Equal(t, 3, agg.Count)
		require.InDelta(t, 61.0, agg.Value.(float64), 0.001)
	})

	t.Run("min aggregation int64", func(t *testing.T) {
		stat := NewStatisticInt64("value", AggregationTypeMin)
		agg := &AggregatedObservation{
			Statistic: stat,
			Value:     int64(30),
			Count:     0,
		}

		agg.Record(stat.Observe(30))
		agg.Record(stat.Observe(10))
		agg.Record(stat.Observe(20))

		require.Equal(t, 3, agg.Count)
		require.Equal(t, int64(10), agg.Value.(int64))
	})

	t.Run("min aggregation float64", func(t *testing.T) {
		stat := NewStatisticFloat64("value", AggregationTypeMin)
		agg := &AggregatedObservation{
			Statistic: stat,
			Value:     float64(30.5),
			Count:     0,
		}

		agg.Record(stat.Observe(30.5))
		agg.Record(stat.Observe(10.2))
		agg.Record(stat.Observe(20.8))

		require.Equal(t, 3, agg.Count)
		require.Equal(t, float64(10.2), agg.Value.(float64))
	})

	t.Run("max aggregation int64", func(t *testing.T) {
		stat := NewStatisticInt64("value", AggregationTypeMax)
		agg := &AggregatedObservation{
			Statistic: stat,
			Value:     int64(10),
			Count:     0,
		}

		agg.Record(stat.Observe(10))
		agg.Record(stat.Observe(30))
		agg.Record(stat.Observe(20))

		require.Equal(t, 3, agg.Count)
		require.Equal(t, int64(30), agg.Value.(int64))
	})

	t.Run("max aggregation float64", func(t *testing.T) {
		stat := NewStatisticFloat64("value", AggregationTypeMax)
		agg := &AggregatedObservation{
			Statistic: stat,
			Value:     float64(10.0),
			Count:     0,
		}

		agg.Record(stat.Observe(10.0))
		agg.Record(stat.Observe(30.5))
		agg.Record(stat.Observe(20.8))

		require.Equal(t, 3, agg.Count)
		require.Equal(t, float64(30.5), agg.Value.(float64))
	})

	t.Run("max aggregation bool flag", func(t *testing.T) {
		stat := NewStatisticFlag("success")
		agg := &AggregatedObservation{
			Statistic: stat,
			Value:     false,
			Count:     0,
		}

		agg.Record(stat.Observe(false))
		agg.Record(stat.Observe(false))
		agg.Record(stat.Observe(true))

		require.Equal(t, 3, agg.Count)
		require.Equal(t, true, agg.Value.(bool))
	})

	t.Run("last aggregation int64", func(t *testing.T) {
		stat := NewStatisticInt64("value", AggregationTypeLast)
		agg := &AggregatedObservation{
			Statistic: stat,
			Value:     int64(0),
			Count:     0,
		}

		agg.Record(stat.Observe(10))
		agg.Record(stat.Observe(20))
		agg.Record(stat.Observe(30))

		require.Equal(t, 3, agg.Count)
		require.Equal(t, int64(30), agg.Value.(int64))
	})

	t.Run("first aggregation int64", func(t *testing.T) {
		stat := NewStatisticInt64("value", AggregationTypeFirst)
		agg := &AggregatedObservation{
			Statistic: stat,
			Value:     nil,
			Count:     0,
		}

		agg.Record(stat.Observe(10))
		agg.Record(stat.Observe(20))
		agg.Record(stat.Observe(30))

		require.Equal(t, 3, agg.Count)
		require.Equal(t, int64(10), agg.Value.(int64))
	})
}
