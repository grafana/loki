package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/xcap"
)

func TestStatsSummary(t *testing.T) {
	t.Run("nil capture returns empty summary with provided values", func(t *testing.T) {
		execTime := 2 * time.Second
		queueTime := 100 * time.Millisecond
		entriesReturned := 42

		result := statsSummary(nil, execTime, queueTime, entriesReturned)

		require.Equal(t, execTime.Seconds(), result.Summary.ExecTime)
		require.Equal(t, queueTime.Seconds(), result.Summary.QueueTime)
		require.Equal(t, int64(entriesReturned), result.Summary.TotalEntriesReturned)
		require.Equal(t, int64(0), result.Querier.Store.Dataobj.PrePredicateDecompressedRows)
		require.Equal(t, int64(0), result.Querier.Store.Dataobj.PrePredicateDecompressedBytes)
		require.Equal(t, int64(0), result.Querier.Store.Dataobj.PostPredicateDecompressedBytes)
		require.Equal(t, int64(0), result.Querier.Store.Dataobj.PostFilterRows)
	})

	t.Run("computes bytes and lines from logs.Reader regions", func(t *testing.T) {
		ctx, capture := xcap.NewCapture(context.Background(), nil)

		_, region1 := xcap.StartRegion(ctx, "logs.Reader.Read")
		region1.Record(dataobj.StatDatasetPrimaryRowBytes.Observe(1000))
		region1.Record(dataobj.StatDatasetSecondaryRowBytes.Observe(500))
		region1.Record(dataobj.StatDatasetPrimaryRowsRead.Observe(100))
		region1.Record(dataobj.StatDatasetSecondaryRowsRead.Observe(80))
		region1.End()

		_, region2 := xcap.StartRegion(ctx, "logs.Reader.Read")
		region2.Record(dataobj.StatDatasetPrimaryRowBytes.Observe(2000))
		region2.Record(dataobj.StatDatasetSecondaryRowBytes.Observe(1000))
		region2.Record(dataobj.StatDatasetPrimaryRowsRead.Observe(200))
		region2.Record(dataobj.StatDatasetSecondaryRowsRead.Observe(150))
		region2.End()

		// Other region - should be ignored.
		_, otherRegion := xcap.StartRegion(ctx, "OtherRegion")
		otherRegion.Record(dataobj.StatDatasetPrimaryRowBytes.Observe(5000))
		otherRegion.End()

		capture.End()

		execTime := 2 * time.Second
		queueTime := 100 * time.Millisecond
		entriesReturned := 42

		result := statsSummary(capture, execTime, queueTime, entriesReturned)

		require.Equal(t, execTime.Seconds(), result.Summary.ExecTime)
		require.Equal(t, queueTime.Seconds(), result.Summary.QueueTime)
		require.Equal(t, int64(entriesReturned), result.Summary.TotalEntriesReturned)

		require.Equal(t, int64(300), result.Querier.Store.Dataobj.PrePredicateDecompressedRows)
		require.Equal(t, int64(3000), result.Querier.Store.Dataobj.PrePredicateDecompressedBytes)
		require.Equal(t, int64(1500), result.Querier.Store.Dataobj.PostPredicateDecompressedBytes)
		require.Equal(t, int64(230), result.Querier.Store.Dataobj.PostFilterRows)
	})

	t.Run("computes cache stats from all regions", func(t *testing.T) {
		ctx, capture := xcap.NewCapture(context.Background(), nil)

		_, region := xcap.StartRegion(ctx, "thread.runJob")
		region.Record(executor.TaskCacheHits.Observe(1))
		region.Record(executor.TaskCacheMisses.Observe(2))
		region.Record(executor.TaskCacheBatches.Observe(3))
		region.Record(executor.TaskCacheBytes.Observe(4))
		region.Record(executor.DataObjScanCacheHits.Observe(5))
		region.Record(executor.DataObjScanCacheMisses.Observe(6))
		region.Record(executor.DataObjScanCacheBatches.Observe(7))
		region.Record(executor.DataObjScanCacheBytes.Observe(8))
		region.End()

		capture.End()

		result := statsSummary(capture, time.Second, 0, 0)

		require.Equal(t, int32(6), result.Caches.TaskResult.EntriesFound)
		require.Equal(t, int32(14), result.Caches.TaskResult.EntriesRequested)
		require.Equal(t, int32(10), result.Caches.TaskResult.Requests)
		require.Equal(t, int64(12), result.Caches.TaskResult.BytesReceived)
	})

	t.Run("missing statistics result in zero values", func(t *testing.T) {
		ctx, capture := xcap.NewCapture(context.Background(), nil)

		_, region := xcap.StartRegion(ctx, "logs.Reader.Read")
		region.Record(dataobj.StatDatasetPrimaryRowBytes.Observe(1000))
		region.End()
		capture.End()

		result := statsSummary(capture, time.Second, 0, 0)

		require.Equal(t, int64(0), result.Summary.TotalPostFilterLines)
		require.Equal(t, int64(0), result.Querier.Store.Dataobj.PrePredicateDecompressedRows)
		require.Equal(t, int64(1000), result.Querier.Store.Dataobj.PrePredicateDecompressedBytes)
		require.Equal(t, int64(0), result.Querier.Store.Dataobj.PostPredicateDecompressedBytes)
		require.Equal(t, int64(0), result.Querier.Store.Dataobj.PostFilterRows)
	})
}
