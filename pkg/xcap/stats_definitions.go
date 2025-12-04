package xcap

// Common pipeline statistics tracked across executor nodes.
var (
	StatRowsOut      = NewStatisticInt64("rows.out", AggregationTypeSum)
	StatReadCalls    = NewStatisticInt64("read.calls", AggregationTypeSum)
	StatReadDuration = NewStatisticInt64("read.duration.ns", AggregationTypeSum)
)

// ColumnCompat statistics.
var (
	StatCompatCollisionFound = NewStatisticFlag("collision.found")
)

var (
	// Dataset column statistics.
	StatPrimaryColumns       = NewStatisticInt64("primary.columns", AggregationTypeSum)
	StatSecondaryColumns     = NewStatisticInt64("secondary.columns", AggregationTypeSum)
	StatPrimaryColumnPages   = NewStatisticInt64("primary.column.pages", AggregationTypeSum)
	StatSecondaryColumnPages = NewStatisticInt64("secondary.column.pages", AggregationTypeSum)

	// Dataset row statistics.
	StatMaxRows           = NewStatisticInt64("row.max", AggregationTypeSum)
	StatRowsAfterPruning  = NewStatisticInt64("rows.after.pruning", AggregationTypeSum)
	StatPrimaryRowsRead   = NewStatisticInt64("primary.rows.read", AggregationTypeSum)
	StatSecondaryRowsRead = NewStatisticInt64("secondary.rows.read", AggregationTypeSum)
	StatPrimaryRowBytes   = NewStatisticInt64("primary.row.read.bytes", AggregationTypeSum)
	StatSecondaryRowBytes = NewStatisticInt64("secondary.row.read.bytes", AggregationTypeSum)

	// Dataset page scan statistics.
	StatPagesScanned         = NewStatisticInt64("pages.scanned", AggregationTypeSum)
	StatPagesFoundInCache    = NewStatisticInt64("pages.cache.hit", AggregationTypeSum)
	StatPageDownloadRequests = NewStatisticInt64("pages.download.requests", AggregationTypeSum)
	StatPageDownloadTime     = NewStatisticInt64("pages.download.duration.ns", AggregationTypeSum)

	// Dataset page download byte statistics.
	StatPrimaryPagesDownloaded           = NewStatisticInt64("primary.pages.downloaded", AggregationTypeSum)
	StatSecondaryPagesDownloaded         = NewStatisticInt64("secondary.pages.downloaded", AggregationTypeSum)
	StatPrimaryColumnBytes               = NewStatisticInt64("primary.pages.compressed.bytes", AggregationTypeSum)
	StatSecondaryColumnBytes             = NewStatisticInt64("secondary.pages.compressed.bytes", AggregationTypeSum)
	StatPrimaryColumnUncompressedBytes   = NewStatisticInt64("primary.column.uncompressed.bytes", AggregationTypeSum)
	StatSecondaryColumnUncompressedBytes = NewStatisticInt64("secondary.column.uncompressed.bytes", AggregationTypeSum)
)

// Dataset read operation statistics.
var (
	StatDatasetReadCalls = NewStatisticInt64("dataset.read.calls", AggregationTypeSum)
)

// Range IO statistics.
var (
	StatInputRangesCount     = NewStatisticInt64("input.ranges", AggregationTypeSum)
	StatInputRangesSize      = NewStatisticInt64("input.ranges.size.bytes", AggregationTypeSum)
	StatOptimizedRangesCount = NewStatisticInt64("optimized.ranges", AggregationTypeSum)
	StatOptimizedRangesSize  = NewStatisticInt64("optimized.ranges.size.bytes", AggregationTypeSum)
	StatOptimizedThroughput  = NewStatisticFloat64("optimized.ranges.min.throughput", AggregationTypeMin)
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
	StatIndexObjects     = NewStatisticInt64("metastore.index.objects", AggregationTypeSum)
	StatResolvedSections = NewStatisticInt64("metastore.resolved.sections", AggregationTypeSum)
)
