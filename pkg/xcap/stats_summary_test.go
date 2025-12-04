package xcap

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSummaryFromCapture(t *testing.T) {
	t.Run("nil capture returns empty summary with provided values", func(t *testing.T) {
		execTime := 2 * time.Second
		queueTime := 100 * time.Millisecond
		entriesReturned := 42

		summary := SummaryFromCapture(nil, execTime, queueTime, entriesReturned)

		require.Equal(t, execTime.Seconds(), summary.ExecTime)
		require.Equal(t, queueTime.Seconds(), summary.QueueTime)
		require.Equal(t, int64(entriesReturned), summary.TotalEntriesReturned)
		require.Equal(t, int64(0), summary.TotalBytesProcessed)
		require.Equal(t, int64(0), summary.TotalLinesProcessed)
		require.Equal(t, int64(0), summary.TotalPostFilterLines)
	})

	t.Run("computes bytes and lines from DataObjScan regions", func(t *testing.T) {
		ctx, capture := NewCapture(context.Background(), nil)

		// Define statistics matching the executor package names
		primaryBytes := NewStatisticInt64("primary_column_uncompressed_bytes", AggregationTypeSum)
		secondaryBytes := NewStatisticInt64("secondary_column_uncompressed_bytes", AggregationTypeSum)
		primaryRows := NewStatisticInt64("primary_rows_read", AggregationTypeSum)
		secondaryRows := NewStatisticInt64("secondary_rows_read", AggregationTypeSum)
		rowsOut := NewStatisticInt64("rows_out", AggregationTypeSum)

		// Create DataObjScan regions with observations
		_, region1 := StartRegion(ctx, "DataObjScan")
		region1.Record(primaryBytes.Observe(1000))
		region1.Record(secondaryBytes.Observe(500))
		region1.Record(primaryRows.Observe(100))
		region1.Record(secondaryRows.Observe(50))
		region1.Record(rowsOut.Observe(80))
		region1.End()

		_, region2 := StartRegion(ctx, "DataObjScan")
		region2.Record(primaryBytes.Observe(2000))
		region2.Record(secondaryBytes.Observe(1000))
		region2.Record(primaryRows.Observe(200))
		region2.Record(secondaryRows.Observe(100))
		region2.Record(rowsOut.Observe(150))
		region2.End()

		capture.End()

		execTime := 2 * time.Second
		queueTime := 100 * time.Millisecond
		entriesReturned := 42

		summary := SummaryFromCapture(capture, execTime, queueTime, entriesReturned)

		require.Equal(t, execTime.Seconds(), summary.ExecTime)
		require.Equal(t, queueTime.Seconds(), summary.QueueTime)
		require.Equal(t, int64(entriesReturned), summary.TotalEntriesReturned)

		// TotalBytesProcessed = primary + secondary = (1000+2000) + (500+1000) = 4500
		require.Equal(t, int64(4500), summary.TotalBytesProcessed)

		// TotalLinesProcessed = primary + secondary = (100+200) + (50+100) = 450
		require.Equal(t, int64(450), summary.TotalLinesProcessed)

		// TotalPostFilterLines = rows_out = 80 + 150 = 230
		require.Equal(t, int64(230), summary.TotalPostFilterLines)

		// BytesProcessedPerSecond = 4500 / 2 = 2250
		require.Equal(t, int64(2250), summary.BytesProcessedPerSecond)

		// LinesProcessedPerSecond = 450 / 2 = 225
		require.Equal(t, int64(225), summary.LinesProcessedPerSecond)
	})

	t.Run("ignores non-DataObjScan regions", func(t *testing.T) {
		ctx, capture := NewCapture(context.Background(), nil)

		primaryBytes := NewStatisticInt64("primary_column_uncompressed_bytes", AggregationTypeSum)

		// DataObjScan region - should be counted
		_, scanRegion := StartRegion(ctx, "DataObjScan")
		scanRegion.Record(primaryBytes.Observe(1000))
		scanRegion.End()

		// Other region - should be ignored
		_, otherRegion := StartRegion(ctx, "OtherRegion")
		otherRegion.Record(primaryBytes.Observe(5000))
		otherRegion.End()

		capture.End()

		summary := SummaryFromCapture(capture, time.Second, 0, 0)

		// Only DataObjScan region bytes counted
		require.Equal(t, int64(1000), summary.TotalBytesProcessed)
	})

	t.Run("zero exec time results in zero per-second rates", func(t *testing.T) {
		ctx, capture := NewCapture(context.Background(), nil)

		primaryBytes := NewStatisticInt64("primary_column_uncompressed_bytes", AggregationTypeSum)
		_, region := StartRegion(ctx, "DataObjScan")
		region.Record(primaryBytes.Observe(1000))
		region.End()
		capture.End()

		summary := SummaryFromCapture(capture, 0, 0, 10)

		require.Equal(t, int64(1000), summary.TotalBytesProcessed)
		require.Equal(t, int64(0), summary.BytesProcessedPerSecond)
		require.Equal(t, int64(0), summary.LinesProcessedPerSecond)
	})

	t.Run("missing statistics result in zero values", func(t *testing.T) {
		ctx, capture := NewCapture(context.Background(), nil)

		// Only record some statistics
		primaryBytes := NewStatisticInt64("primary_column_uncompressed_bytes", AggregationTypeSum)
		_, region := StartRegion(ctx, "DataObjScan")
		region.Record(primaryBytes.Observe(1000))
		region.End()
		capture.End()

		execTime := 1 * time.Second
		summary := SummaryFromCapture(capture, execTime, 0, 0)

		// Only primary bytes recorded
		require.Equal(t, int64(1000), summary.TotalBytesProcessed)
		// No rows recorded
		require.Equal(t, int64(0), summary.TotalLinesProcessed)
		require.Equal(t, int64(0), summary.TotalPostFilterLines)
	})

	t.Run("rolls up child region observations into DataObjScan", func(t *testing.T) {
		ctx, capture := NewCapture(context.Background(), nil)

		primaryBytes := NewStatisticInt64("primary_column_uncompressed_bytes", AggregationTypeSum)

		// Parent DataObjScan region
		ctx, parent := StartRegion(ctx, "DataObjScan")
		parent.Record(primaryBytes.Observe(500))

		// Child region (should be rolled up into parent)
		_, child := StartRegion(ctx, "child_operation")
		child.Record(primaryBytes.Observe(300))
		child.End()

		parent.End()
		capture.End()

		summary := SummaryFromCapture(capture, time.Second, 0, 0)

		// Child observations rolled up into DataObjScan: 500 + 300 = 800
		require.Equal(t, int64(800), summary.TotalBytesProcessed)
	})

	t.Run("empty capture returns zero stats", func(t *testing.T) {
		_, capture := NewCapture(context.Background(), nil)
		capture.End()

		summary := SummaryFromCapture(capture, time.Second, time.Millisecond, 10)

		require.Equal(t, float64(1), summary.ExecTime)
		require.Equal(t, float64(0.001), summary.QueueTime)
		require.Equal(t, int64(10), summary.TotalEntriesReturned)
		require.Equal(t, int64(0), summary.TotalBytesProcessed)
		require.Equal(t, int64(0), summary.TotalLinesProcessed)
	})
}
