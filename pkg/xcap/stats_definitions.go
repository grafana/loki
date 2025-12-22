package xcap

// Common pipeline statistics tracked across executor nodes.
var (
	StatPipelineRowsOut      = NewStatisticInt64("rows.out", AggregationTypeSum)
	StatPipelineReadCalls    = NewStatisticInt64("read.calls", AggregationTypeSum)
	StatPipelineReadDuration = NewStatisticFloat64("read.duration", AggregationTypeSum)
)

// ColumnCompat statistics.
var (
	StatCompatCollisionFound = NewStatisticFlag("collision.found")
)

var (
	// Dataset column statistics.
	StatDatasetPrimaryColumns       = NewStatisticInt64("primary.columns", AggregationTypeSum)
	StatDatasetSecondaryColumns     = NewStatisticInt64("secondary.columns", AggregationTypeSum)
	StatDatasetPrimaryColumnPages   = NewStatisticInt64("primary.column.pages", AggregationTypeSum)
	StatDatasetSecondaryColumnPages = NewStatisticInt64("secondary.column.pages", AggregationTypeSum)

	// Dataset row statistics.
	StatDatasetMaxRows           = NewStatisticInt64("rows.max", AggregationTypeSum)
	StatDatasetRowsAfterPruning  = NewStatisticInt64("rows.after.pruning", AggregationTypeSum)
	StatDatasetPrimaryRowsRead   = NewStatisticInt64("primary.rows.read", AggregationTypeSum)
	StatDatasetSecondaryRowsRead = NewStatisticInt64("secondary.rows.read", AggregationTypeSum)
	StatDatasetPrimaryRowBytes   = NewStatisticInt64("primary.row.read.bytes", AggregationTypeSum)
	StatDatasetSecondaryRowBytes = NewStatisticInt64("secondary.row.read.bytes", AggregationTypeSum)

	// Dataset page scan statistics.
	StatDatasetPagesScanned         = NewStatisticInt64("pages.scanned", AggregationTypeSum)
	StatDatasetPagesFoundInCache    = NewStatisticInt64("pages.cache.hit", AggregationTypeSum)
	StatDatasetPageDownloadRequests = NewStatisticInt64("pages.download.requests", AggregationTypeSum)
	StatDatasetPageDownloadTime     = NewStatisticFloat64("pages.download.duration", AggregationTypeSum)

	// Dataset page download byte statistics.
	StatDatasetPrimaryPagesDownloaded           = NewStatisticInt64("primary.pages.downloaded", AggregationTypeSum)
	StatDatasetSecondaryPagesDownloaded         = NewStatisticInt64("secondary.pages.downloaded", AggregationTypeSum)
	StatDatasetPrimaryColumnBytes               = NewStatisticInt64("primary.pages.compressed.bytes", AggregationTypeSum)
	StatDatasetSecondaryColumnBytes             = NewStatisticInt64("secondary.pages.compressed.bytes", AggregationTypeSum)
	StatDatasetPrimaryColumnUncompressedBytes   = NewStatisticInt64("primary.column.uncompressed.bytes", AggregationTypeSum)
	StatDatasetSecondaryColumnUncompressedBytes = NewStatisticInt64("secondary.column.uncompressed.bytes", AggregationTypeSum)

	// Dataset read operation statistics.
	StatDatasetReadCalls = NewStatisticInt64("dataset.read.calls", AggregationTypeSum)
)

// Range IO statistics.
var (
	StatRangeIOInputCount     = NewStatisticInt64("input.ranges", AggregationTypeSum)
	StatRangeIOInputSize      = NewStatisticInt64("input.ranges.size.bytes", AggregationTypeSum)
	StatRangeIOOptimizedCount = NewStatisticInt64("optimized.ranges", AggregationTypeSum)
	StatRangeIOOptimizedSize  = NewStatisticInt64("optimized.ranges.size.bytes", AggregationTypeSum)
	StatRangeIOThroughput     = NewStatisticFloat64("optimized.ranges.min.throughput", AggregationTypeMin)
)

// Bucket operation statistics.
var (
	StatBucketGet        = NewStatisticInt64("bucket.get", AggregationTypeSum)
	StatBucketGetRange   = NewStatisticInt64("bucket.getrange", AggregationTypeSum)
	StatBucketIter       = NewStatisticInt64("bucket.iter", AggregationTypeSum)
	StatBucketAttributes = NewStatisticInt64("bucket.attributes", AggregationTypeSum)
)

// Metastore statistics.
var (
	StatMetastoreIndexObjects            = NewStatisticInt64("metastore.index.objects", AggregationTypeSum)
	StatMetastoreSectionsResolved        = NewStatisticInt64("metastore.sections.resolved", AggregationTypeSum)
	StatMetastoreStreamsRead             = NewStatisticInt64("metastore.sections.streams.read", AggregationTypeSum)
	StatMetastoreStreamsReadTime         = NewStatisticFloat64("metastore.sections.streams.read.duration", AggregationTypeSum)
	StatMetastoreSectionPointersRead     = NewStatisticInt64("metastore.sections.pointers.read", AggregationTypeSum)
	StatMetastoreSectionPointersReadTime = NewStatisticFloat64("metastore.sections.pointers.read.duration", AggregationTypeSum)
)
