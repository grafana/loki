package xcap

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestObservations(t *testing.T) {
	ctx, capture := NewCapture(context.Background(), nil)

	statA := NewStatisticInt64("stat.one", AggregationTypeSum)
	statB := NewStatisticInt64("stat.two", AggregationTypeSum)
	statC := NewStatisticInt64("stat.three", AggregationTypeSum)

	_, region := StartRegion(ctx, "Test")
	region.Record(statA.Observe(10))
	region.Record(statB.Observe(20))
	region.Record(statC.Observe(30))
	region.End()

	collector := newObservationCollector(capture)
	obs := collector.fromRegions("Test", false)

	t.Run("filter", func(t *testing.T) {
		filtered := obs.filter(statA.Key(), statB.Key())
		require.Len(t, filtered.data, 2)
		require.Equal(t, int64(10), filtered.data[statA.Key()].Value)
		require.Equal(t, int64(20), filtered.data[statB.Key()].Value)
	})

	t.Run("prefix", func(t *testing.T) {
		prefixed := obs.filter(statA.Key()).prefix("metastore_")
		expectedKey := StatisticKey{Name: "metastore_stat.one", DataType: DataTypeInt64, Aggregation: AggregationTypeSum}
		require.Len(t, prefixed.data, 1)
		require.Equal(t, int64(10), prefixed.data[expectedKey].Value)
	})

	t.Run("normalizeKeys", func(t *testing.T) {
		normalized := obs.filter(statC.Key()).normalizeKeys()
		expectedKey := StatisticKey{Name: "stat_three", DataType: DataTypeInt64, Aggregation: AggregationTypeSum}
		require.Len(t, normalized.data, 1)
		require.Equal(t, int64(30), normalized.data[expectedKey].Value)
	})

	t.Run("merge", func(t *testing.T) {
		target := newObservations()
		target.merge(obs.filter(statA.Key(), statB.Key()))
		target.merge(obs.filter(statB.Key(), statC.Key()))

		require.Len(t, target.data, 3)
		require.Equal(t, int64(10), target.data[statA.Key()].Value)
		require.Equal(t, int64(40), target.data[statB.Key()].Value)
		require.Equal(t, int64(30), target.data[statC.Key()].Value)
	})

	t.Run("toLogValues", func(t *testing.T) {
		logValues := obs.toLogValues()
		// Sorted: stat.one, stat.two, stat.three
		require.Equal(t, []any{"stat.one", int64(10), "stat.three", int64(30), "stat.two", int64(20)}, logValues)
	})

	t.Run("chaining", func(t *testing.T) {
		result := obs.filter(statC.Key()).prefix("logs_").normalizeKeys().toLogValues()
		require.Equal(t, []any{"logs_stat_three", int64(30)}, result)
	})
}

func TestRollups(t *testing.T) {
	t.Run("includes child observations when rollup=true", func(t *testing.T) {
		ctx, capture := NewCapture(context.Background(), nil)
		stat := NewStatisticInt64("count", AggregationTypeSum)

		ctx, parent := StartRegion(ctx, "Parent")
		parent.Record(stat.Observe(10))

		ctx, child := StartRegion(ctx, "Child")
		child.Record(stat.Observe(20))

		_, grandchild := StartRegion(ctx, "Grandchild")
		grandchild.Record(stat.Observe(30))

		grandchild.End()
		child.End()
		parent.End()

		collector := newObservationCollector(capture)

		withRollup := collector.fromRegions("Parent", true)
		require.Equal(t, int64(60), withRollup.data[stat.Key()].Value) // 10 + 20 + 30

		withoutRollup := collector.fromRegions("Parent", false)
		require.Equal(t, int64(10), withoutRollup.data[stat.Key()].Value) // parent only
	})

	t.Run("excludes matching descendants", func(t *testing.T) {
		ctx, capture := NewCapture(context.Background(), nil)
		stat := NewStatisticInt64("count", AggregationTypeSum)

		ctx, parent := StartRegion(ctx, "Parent")
		parent.Record(stat.Observe(1))

		_, included := StartRegion(ctx, "included")
		included.Record(stat.Observe(10))
		included.End()

		ctx2, excluded := StartRegion(ctx, "excluded")
		excluded.Record(stat.Observe(100))

		_, excludedChild := StartRegion(ctx2, "excludedChild")
		excludedChild.Record(stat.Observe(1000))
		excludedChild.End()

		excluded.End()
		parent.End()

		collector := newObservationCollector(capture)
		stats := collector.fromRegions("Parent", true, "excluded")
		require.Equal(t, int64(11), stats.data[stat.Key()].Value) // 1 + 10, excludes 100 + 1000
	})
}
