package xcap

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestToStatsSummary(t *testing.T) {
	t.Run("nil capture returns empty summary with provided values", func(t *testing.T) {
		execTime := 2 * time.Second
		queueTime := 100 * time.Millisecond
		entriesReturned := 42

		var capture *Capture
		summary := capture.ToStatsSummary(execTime, queueTime, entriesReturned)

		require.Equal(t, execTime.Seconds(), summary.ExecTime)
		require.Equal(t, queueTime.Seconds(), summary.QueueTime)
		require.Equal(t, int64(entriesReturned), summary.TotalEntriesReturned)
		require.Equal(t, int64(0), summary.TotalBytesProcessed)
		require.Equal(t, int64(0), summary.TotalLinesProcessed)
		require.Equal(t, int64(0), summary.TotalPostFilterLines)
	})

	t.Run("computes bytes and lines from DataObjScan regions", func(t *testing.T) {
		ctx, capture := NewCapture(context.Background(), nil)

		// Create DataObjScan regions with observations using registry stats
		_, region1 := StartRegion(ctx, "DataObjScan")
		region1.Record(StatPrimaryColumnUncompressedBytes.Observe(1000))
		region1.Record(StatSecondaryColumnUncompressedBytes.Observe(500))
		region1.Record(StatPrimaryRowsRead.Observe(100))
		region1.Record(StatSecondaryRowsRead.Observe(50))
		region1.Record(StatRowsOut.Observe(80))
		region1.End()

		_, region2 := StartRegion(ctx, "DataObjScan")
		region2.Record(StatPrimaryColumnUncompressedBytes.Observe(2000))
		region2.Record(StatSecondaryColumnUncompressedBytes.Observe(1000))
		region2.Record(StatPrimaryRowsRead.Observe(200))
		region2.Record(StatSecondaryRowsRead.Observe(100))
		region2.Record(StatRowsOut.Observe(150))
		region2.End()

		capture.End()

		execTime := 2 * time.Second
		queueTime := 100 * time.Millisecond
		entriesReturned := 42

		summary := capture.ToStatsSummary(execTime, queueTime, entriesReturned)

		require.Equal(t, execTime.Seconds(), summary.ExecTime)
		require.Equal(t, queueTime.Seconds(), summary.QueueTime)
		require.Equal(t, int64(entriesReturned), summary.TotalEntriesReturned)

		// TotalBytesProcessed = primary + secondary = (1000+2000) + (500+1000) = 4500
		require.Equal(t, int64(4500), summary.TotalBytesProcessed)

		// TotalLinesProcessed = primary_rows_read = 100 + 200 = 300
		require.Equal(t, int64(300), summary.TotalLinesProcessed)

		// TotalPostFilterLines = rows_out = 80 + 150 = 230
		require.Equal(t, int64(230), summary.TotalPostFilterLines)

		// BytesProcessedPerSecond = 4500 / 2 = 2250
		require.Equal(t, int64(2250), summary.BytesProcessedPerSecond)

		// LinesProcessedPerSecond = 300 / 2 = 150
		require.Equal(t, int64(150), summary.LinesProcessedPerSecond)
	})

	t.Run("ignores non-DataObjScan regions", func(t *testing.T) {
		ctx, capture := NewCapture(context.Background(), nil)

		// DataObjScan region - should be counted
		_, scanRegion := StartRegion(ctx, "DataObjScan")
		scanRegion.Record(StatPrimaryColumnUncompressedBytes.Observe(1000))
		scanRegion.End()

		// Other region - should be ignored
		_, otherRegion := StartRegion(ctx, "OtherRegion")
		otherRegion.Record(StatPrimaryColumnUncompressedBytes.Observe(5000))
		otherRegion.End()

		capture.End()

		summary := capture.ToStatsSummary(time.Second, 0, 0)

		// Only DataObjScan region bytes counted
		require.Equal(t, int64(1000), summary.TotalBytesProcessed)
	})

	t.Run("zero exec time results in zero per-second rates", func(t *testing.T) {
		ctx, capture := NewCapture(context.Background(), nil)

		_, region := StartRegion(ctx, "DataObjScan")
		region.Record(StatPrimaryColumnUncompressedBytes.Observe(1000))
		region.End()
		capture.End()

		summary := capture.ToStatsSummary(0, 0, 10)

		require.Equal(t, int64(1000), summary.TotalBytesProcessed)
		require.Equal(t, int64(0), summary.BytesProcessedPerSecond)
		require.Equal(t, int64(0), summary.LinesProcessedPerSecond)
	})

	t.Run("missing statistics result in zero values", func(t *testing.T) {
		ctx, capture := NewCapture(context.Background(), nil)

		// Only record some statistics
		_, region := StartRegion(ctx, "DataObjScan")
		region.Record(StatPrimaryColumnUncompressedBytes.Observe(1000))
		region.End()
		capture.End()

		execTime := 1 * time.Second
		summary := capture.ToStatsSummary(execTime, 0, 0)

		// Only primary bytes recorded
		require.Equal(t, int64(1000), summary.TotalBytesProcessed)
		// No rows recorded
		require.Equal(t, int64(0), summary.TotalLinesProcessed)
		require.Equal(t, int64(0), summary.TotalPostFilterLines)
	})

	t.Run("rolls up child region observations into DataObjScan", func(t *testing.T) {
		ctx, capture := NewCapture(context.Background(), nil)

		// Parent DataObjScan region
		ctx, parent := StartRegion(ctx, "DataObjScan")
		parent.Record(StatPrimaryColumnUncompressedBytes.Observe(500))

		// Child region (should be rolled up into parent)
		_, child := StartRegion(ctx, "child_operation")
		child.Record(StatPrimaryColumnUncompressedBytes.Observe(300))
		child.End()

		parent.End()
		capture.End()

		summary := capture.ToStatsSummary(time.Second, 0, 0)

		// Child observations rolled up into DataObjScan: 500 + 300 = 800
		require.Equal(t, int64(800), summary.TotalBytesProcessed)
	})

	t.Run("empty capture returns zero stats", func(t *testing.T) {
		_, capture := NewCapture(context.Background(), nil)
		capture.End()

		summary := capture.ToStatsSummary(time.Second, time.Millisecond, 10)

		require.Equal(t, float64(1), summary.ExecTime)
		require.Equal(t, float64(0.001), summary.QueueTime)
		require.Equal(t, int64(10), summary.TotalEntriesReturned)
		require.Equal(t, int64(0), summary.TotalBytesProcessed)
		require.Equal(t, int64(0), summary.TotalLinesProcessed)
	})
}
