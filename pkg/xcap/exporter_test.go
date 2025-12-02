package xcap

import (
	"bytes"
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
)

func TestCollectQuerySummary_NilCapture(t *testing.T) {
	stats := CollectQuerySummary(nil)
	require.Nil(t, stats)
}

func TestCollectQuerySummary_EmptyCapture(t *testing.T) {
	_, capture := NewCapture(context.Background(), nil)
	stats := CollectQuerySummary(capture)

	require.NotNil(t, stats)
	require.Equal(t, 0, len(stats.data))
}

func TestCollectQuerySummary_MetastoreStats(t *testing.T) {
	ctx, capture := NewCapture(context.Background(), nil)

	indexObjects := NewStatisticInt64("metastore.index.objects", AggregationTypeSum)
	resolvedSections := NewStatisticInt64("metastore.resolved.sections", AggregationTypeSum)
	bucketGet := NewStatisticInt64("bucket.get", AggregationTypeSum)
	primaryColumnPages := NewStatisticInt64("primary.column.pages", AggregationTypeSum)

	_, region := StartRegion(ctx, RegionMetastore)
	region.Record(indexObjects.Observe(5))
	region.Record(resolvedSections.Observe(10))
	region.Record(bucketGet.Observe(3))
	region.Record(primaryColumnPages.Observe(20))
	region.End()

	stats := toMap(CollectQuerySummary(capture))

	require.Equal(t, int64(5), stats["metastore_index_objects"])
	require.Equal(t, int64(10), stats["metastore_resolved_sections"])
	require.Equal(t, int64(3), stats["metastore_bucket_get"])
	require.Equal(t, int64(20), stats["metastore_primary_column_pages"])
}

func TestCollectQuerySummary_BucketStatsGroupedByOperation(t *testing.T) {
	ctx, capture := NewCapture(context.Background(), nil)

	bucketGet := NewStatisticInt64("bucket.get", AggregationTypeSum)
	bucketGetRange := NewStatisticInt64("bucket.get.range", AggregationTypeSum)
	bucketAttributes := NewStatisticInt64("bucket.attributes", AggregationTypeSum)

	// Root region (Engine.Execute)
	ctx, rootRegion := StartRegion(ctx, "Engine.Execute")

	// Metastore region (child of root)
	ctx1, metastoreRegion := StartRegion(ctx, RegionMetastore)
	metastoreRegion.Record(bucketGet.Observe(2))
	metastoreRegion.Record(bucketGetRange.Observe(3))
	metastoreRegion.Record(bucketAttributes.Observe(1))
	metastoreRegion.End()

	// Scan region (logs/dataobj) - child of root
	ctx2, scanRegion := StartRegion(ctx, RegionScan)
	scanRegion.Record(bucketGetRange.Observe(10))
	scanRegion.Record(bucketAttributes.Observe(2))

	// dataset.Reader - child of Scan
	_, readerRegion := StartRegion(ctx2, "dataset.Reader")
	readerRegion.Record(bucketGetRange.Observe(5))
	readerRegion.End()

	// streamsView - child of Scan (excluded from Scan rollup, tracked separately)
	ctx3, streamsviewRegion := StartRegion(ctx2, RegionStreamsView)
	streamsviewRegion.Record(bucketGetRange.Observe(7))

	// streamsView's own reader - child of streamsView
	_, streamsReaderRegion := StartRegion(ctx3, "dataset.Reader")
	streamsReaderRegion.Record(bucketGetRange.Observe(3))
	streamsReaderRegion.End()

	streamsviewRegion.End()
	scanRegion.End()

	rootRegion.End()
	_ = ctx1 // silence unused warning

	stats := toMap(CollectQuerySummary(capture))

	// Verify metastore bucket stats (metastore_*)
	require.Equal(t, int64(2), stats["metastore_bucket_get"])
	require.Equal(t, int64(3), stats["metastore_bucket_get_range"])
	require.Equal(t, int64(1), stats["metastore_bucket_attributes"])

	// Verify logs/dataobj bucket stats (logs_dataset_*)
	// Should be 10 (Scan) + 5 (dataset.Reader) = 15
	// streamsView (7) and its reader (3) are EXCLUDED
	require.Equal(t, int64(15), stats["logs_dataset_bucket_get_range"])
	require.Equal(t, int64(2), stats["logs_dataset_bucket_attributes"])

	// Verify streams bucket stats (streams_*)
	// Should be 7 (streamsView) + 3 (its reader) = 10
	require.Equal(t, int64(10), stats["streams_bucket_get_range"])
}

func TestCollectQuerySummary_LogsDatasetStats(t *testing.T) {
	ctx, capture := NewCapture(context.Background(), nil)

	primaryColumnPages := NewStatisticInt64("primary.column.pages", AggregationTypeSum)
	secondaryColumnPages := NewStatisticInt64("secondary.column.pages", AggregationTypeSum)
	pagesScanned := NewStatisticInt64("pages.scanned", AggregationTypeSum)
	rowsMax := NewStatisticInt64("rows.max", AggregationTypeSum)
	primaryBytesCompressed := NewStatisticInt64("primary.bytes.compressed", AggregationTypeSum)
	downloadDuration := NewStatisticInt64("download.duration.ns", AggregationTypeSum)

	// Root region
	ctx, root := StartRegion(ctx, "Engine.Execute")

	// Scan region (logs/dataobj)
	ctx, scan := StartRegion(ctx, RegionScan)

	// Reader region with stats
	_, reader := StartRegion(ctx, "dataset.Reader")
	reader.Record(primaryColumnPages.Observe(10))
	reader.Record(secondaryColumnPages.Observe(5))
	reader.Record(pagesScanned.Observe(100))
	reader.Record(rowsMax.Observe(1000))
	reader.Record(primaryBytesCompressed.Observe(50000))
	reader.Record(downloadDuration.Observe(5000000)) // 5ms
	reader.End()

	scan.End()
	root.End()

	stats := toMap(CollectQuerySummary(capture))

	// Logs dataset stats should be rolled up to Scan and prefixed with logs_dataset_
	require.Equal(t, int64(10), stats["logs_dataset_primary_column_pages"])
	require.Equal(t, int64(5), stats["logs_dataset_secondary_column_pages"])
	require.Equal(t, int64(100), stats["logs_dataset_pages_scanned"])
	require.Equal(t, int64(1000), stats["logs_dataset_rows_max"])
	require.Equal(t, int64(50000), stats["logs_dataset_primary_bytes_compressed"])
	require.Equal(t, int64(5000000), stats["logs_dataset_download_duration_ns"])
}

func TestCollectQuerySummary_StreamsStats(t *testing.T) {
	ctx, capture := NewCapture(context.Background(), nil)

	bucketGetRange := NewStatisticInt64("bucket.get.range", AggregationTypeSum)
	primaryColumnPages := NewStatisticInt64("primary.column.pages", AggregationTypeSum)
	secondaryBytesCompressed := NewStatisticInt64("secondary.bytes.compressed", AggregationTypeSum)

	// Root region
	ctx, root := StartRegion(ctx, "Engine.Execute")

	// Streamsview region
	ctx, streams := StartRegion(ctx, RegionStreamsView)
	streams.Record(bucketGetRange.Observe(3))

	// Child reader region
	_, reader := StartRegion(ctx, "dataset.Reader")
	reader.Record(primaryColumnPages.Observe(8))
	reader.Record(secondaryBytesCompressed.Observe(25000))
	reader.End()

	streams.End()
	root.End()

	stats := toMap(CollectQuerySummary(capture))

	// Streams stats should be rolled up and prefixed with streams_
	require.Equal(t, int64(3), stats["streams_bucket_get_range"])
	require.Equal(t, int64(8), stats["streams_primary_column_pages"])
	require.Equal(t, int64(25000), stats["streams_secondary_bytes_compressed"])
}

func TestExportLog_NilCapture(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogfmtLogger(&buf)

	ExportLog(nil, logger)

	require.Empty(t, buf.String())
}

func TestExportLog_NilLogger(t *testing.T) {
	ctx, capture := NewCapture(context.Background(), nil)
	_, region := StartRegion(ctx, "test")
	region.End()

	// Should not panic
	ExportLog(capture, nil)
}

func TestExportLog_OutputsLogLine(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogfmtLogger(&buf)

	ctx, capture := NewCapture(context.Background(), nil)

	indexObjects := NewStatisticInt64("metastore.index.objects", AggregationTypeSum)
	bucketGet := NewStatisticInt64("bucket.get", AggregationTypeSum)
	primaryColumnPages := NewStatisticInt64("primary.column.pages", AggregationTypeSum)

	_, region := StartRegion(ctx, RegionMetastore)
	region.Record(indexObjects.Observe(5))
	region.Record(bucketGet.Observe(2))
	region.Record(primaryColumnPages.Observe(10))
	region.End()

	ExportLog(capture, logger)

	output := buf.String()
	require.Contains(t, output, "metastore_index_objects=5")
	require.Contains(t, output, "metastore_bucket_get=2")
	require.Contains(t, output, "metastore_primary_column_pages=10")
}

func TestExportLog_SortedKeys(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewLogfmtLogger(&buf)

	ctx, capture := NewCapture(context.Background(), nil)

	// Create stats with names that will sort in specific order
	zStat := NewStatisticInt64("metastore.z_stat", AggregationTypeSum)
	aStat := NewStatisticInt64("metastore.a_stat", AggregationTypeSum)
	mStat := NewStatisticInt64("metastore.m_stat", AggregationTypeSum)

	_, region := StartRegion(ctx, RegionMetastore)
	region.Record(zStat.Observe(1))
	region.Record(aStat.Observe(2))
	region.Record(mStat.Observe(3))
	region.End()

	ExportLog(capture, logger)

	output := buf.String()
	// Keys should appear in sorted order
	aIndex := bytes.Index([]byte(output), []byte("metastore_a_stat"))
	mIndex := bytes.Index([]byte(output), []byte("metastore_m_stat"))
	zIndex := bytes.Index([]byte(output), []byte("metastore_z_stat"))

	require.Less(t, aIndex, mIndex, "a_stat should appear before m_stat")
	require.Less(t, mIndex, zIndex, "m_stat should appear before z_stat")
}
