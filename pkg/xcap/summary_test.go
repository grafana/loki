package xcap

import (
	"context"
	"testing"
	"time"

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

func TestToStatsSummary(t *testing.T) {
	t.Run("nil capture returns empty summary with provided values", func(t *testing.T) {
		execTime := 2 * time.Second
		queueTime := 100 * time.Millisecond
		entriesReturned := 42

		var capture *Capture
		result := capture.ToStatsSummary(execTime, queueTime, entriesReturned)

		require.Equal(t, execTime.Seconds(), result.Summary.ExecTime)
		require.Equal(t, queueTime.Seconds(), result.Summary.QueueTime)
		require.Equal(t, int64(entriesReturned), result.Summary.TotalEntriesReturned)
		require.Equal(t, int64(0), result.Querier.Store.Dataobj.PrePredicateDecompressedRows)
		require.Equal(t, int64(0), result.Querier.Store.Dataobj.PrePredicateDecompressedBytes)
		require.Equal(t, int64(0), result.Querier.Store.Dataobj.PostPredicateDecompressedBytes)
		require.Equal(t, int64(0), result.Querier.Store.Dataobj.PostFilterRows)
	})

	t.Run("computes bytes and lines from DataObjScan regions", func(t *testing.T) {
		ctx, capture := NewCapture(context.Background(), nil)

		// Create DataObjScan regions with observations using registry stats
		_, region1 := StartRegion(ctx, "DataObjScan")
		region1.Record(StatDatasetPrimaryRowBytes.Observe(1000))
		region1.Record(StatDatasetSecondaryRowBytes.Observe(500))
		region1.Record(StatDatasetPrimaryRowsRead.Observe(100))
		region1.Record(StatPipelineRowsOut.Observe(80))
		region1.End()

		_, region2 := StartRegion(ctx, "DataObjScan")
		region2.Record(StatDatasetPrimaryRowBytes.Observe(2000))
		region2.Record(StatDatasetSecondaryRowBytes.Observe(1000))
		region2.Record(StatDatasetPrimaryRowsRead.Observe(200))
		region2.Record(StatPipelineRowsOut.Observe(150))
		region2.End()

		// Other region - should be ignored
		_, otherRegion := StartRegion(ctx, "OtherRegion")
		otherRegion.Record(StatDatasetPrimaryRowBytes.Observe(5000))
		otherRegion.End()

		capture.End()

		execTime := 2 * time.Second
		queueTime := 100 * time.Millisecond
		entriesReturned := 42

		result := capture.ToStatsSummary(execTime, queueTime, entriesReturned)

		require.Equal(t, execTime.Seconds(), result.Summary.ExecTime)
		require.Equal(t, queueTime.Seconds(), result.Summary.QueueTime)
		require.Equal(t, int64(entriesReturned), result.Summary.TotalEntriesReturned)

		require.Equal(t, int64(300), result.Querier.Store.Dataobj.PrePredicateDecompressedRows)
		require.Equal(t, int64(3000), result.Querier.Store.Dataobj.PrePredicateDecompressedBytes)
		require.Equal(t, int64(1500), result.Querier.Store.Dataobj.PostPredicateDecompressedBytes)
		require.Equal(t, int64(230), result.Querier.Store.Dataobj.PostFilterRows)
	})

	t.Run("missing statistics result in zero values", func(t *testing.T) {
		ctx, capture := NewCapture(context.Background(), nil)

		// Only record some statistics
		_, region := StartRegion(ctx, "DataObjScan")
		region.Record(StatDatasetPrimaryRowBytes.Observe(1000))
		region.End()
		capture.End()

		execTime := 1 * time.Second
		result := capture.ToStatsSummary(execTime, 0, 0)

		require.Equal(t, int64(0), result.Summary.TotalPostFilterLines)
		require.Equal(t, int64(0), result.Querier.Store.Dataobj.PrePredicateDecompressedRows)
		require.Equal(t, int64(1000), result.Querier.Store.Dataobj.PrePredicateDecompressedBytes)
		require.Equal(t, int64(0), result.Querier.Store.Dataobj.PostPredicateDecompressedBytes)
		require.Equal(t, int64(0), result.Querier.Store.Dataobj.PostFilterRows)
	})

	t.Run("rolls up child region observations into DataObjScan", func(t *testing.T) {
		ctx, capture := NewCapture(context.Background(), nil)

		// Parent DataObjScan region
		ctx, parent := StartRegion(ctx, "DataObjScan")
		parent.Record(StatDatasetPrimaryRowBytes.Observe(500))

		// Child region (should be rolled up into parent)
		_, child := StartRegion(ctx, "child_operation")
		child.Record(StatDatasetPrimaryRowBytes.Observe(300))
		child.End()

		parent.End()
		capture.End()

		result := capture.ToStatsSummary(time.Second, 0, 0)

		// Child observations rolled up into DataObjScan: 500 + 300 = 800
		require.Equal(t, int64(800), result.Querier.Store.Dataobj.PrePredicateDecompressedBytes)
	})
}
