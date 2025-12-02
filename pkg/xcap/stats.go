package xcap

// This file contains all statistic definitions for xcap.
// Centralizing stat definitions here ensures consistent naming and makes
// it easier to reference stats in the exporter and other places.

// Bucket operation statistic names.
const (
	StatNameBucketGet        = "bucket.get"
	StatNameBucketGetRange   = "bucket.get.range"
	StatNameBucketIter       = "bucket.iter"
	StatNameBucketExists     = "bucket.exists"
	StatNameBucketUpload     = "bucket.upload"
	StatNameBucketAttributes = "bucket.attributes"
)

// Bucket operation statistics.
var (
	StatBucketGet        = NewStatisticInt64(StatNameBucketGet, AggregationTypeSum)
	StatBucketGetRange   = NewStatisticInt64(StatNameBucketGetRange, AggregationTypeSum)
	StatBucketIter       = NewStatisticInt64(StatNameBucketIter, AggregationTypeSum)
	StatBucketExists     = NewStatisticInt64(StatNameBucketExists, AggregationTypeSum)
	StatBucketUpload     = NewStatisticInt64(StatNameBucketUpload, AggregationTypeSum)
	StatBucketAttributes = NewStatisticInt64(StatNameBucketAttributes, AggregationTypeSum)
)

// Dataset reader statistic names.
const (
	// Column statistics
	StatNamePrimaryColumns   = "primary.columns"
	StatNameSecondaryColumns = "secondary.columns"

	// Page statistics
	StatNamePrimaryColumnPages   = "primary.column.pages"
	StatNameSecondaryColumnPages = "secondary.column.pages"

	// Row statistics
	StatNameMaxRows           = "rows.max"
	StatNameRowsAfterPruning  = "rows.after.pruning"
	StatNamePrimaryRowsRead   = "primary.rows.read"
	StatNameSecondaryRowsRead = "secondary.rows.read"
	StatNamePrimaryRowBytes   = "primary.rows.read.bytes"
	StatNameSecondaryRowBytes = "secondary.rows.read.bytes"

	// Download/Page scan statistics
	StatNamePagesScanned          = "pages.scanned"
	StatNamePagesFoundInCache     = "pages.cache.hit"
	StatNameBatchDownloadRequests = "pages.download.requests"
	StatNamePageDownloadTime      = "pages.download.duration.ns"

	// Page download byte statistics
	StatNamePrimaryColumnPagesDownloaded     = "primary.pages.downloaded"
	StatNameSecondaryColumnPagesDownloaded   = "secondary.pages.downloaded"
	StatNamePrimaryColumnBytes               = "primary.pages.compressed.bytes"
	StatNameSecondaryColumnBytes             = "secondary.pages.compressed.bytes"
	StatNamePrimaryColumnUncompressedBytes   = "primary.pages.uncompressed.bytes"
	StatNameSecondaryColumnUncompressedBytes = "secondary.pages.uncompressed.bytes"

	// Read operation statistics
	StatNameReadCalls = "read.calls"
)

// Dataset reader statistics.
var (
	// Column statistics
	StatPrimaryColumns   = NewStatisticInt64(StatNamePrimaryColumns, AggregationTypeSum)
	StatSecondaryColumns = NewStatisticInt64(StatNameSecondaryColumns, AggregationTypeSum)

	// Page statistics
	StatPrimaryColumnPages   = NewStatisticInt64(StatNamePrimaryColumnPages, AggregationTypeSum)
	StatSecondaryColumnPages = NewStatisticInt64(StatNameSecondaryColumnPages, AggregationTypeSum)

	// Row statistics
	StatMaxRows           = NewStatisticInt64(StatNameMaxRows, AggregationTypeSum)
	StatRowsAfterPruning  = NewStatisticInt64(StatNameRowsAfterPruning, AggregationTypeSum)
	StatPrimaryRowsRead   = NewStatisticInt64(StatNamePrimaryRowsRead, AggregationTypeSum)
	StatSecondaryRowsRead = NewStatisticInt64(StatNameSecondaryRowsRead, AggregationTypeSum)
	StatPrimaryRowBytes   = NewStatisticInt64(StatNamePrimaryRowBytes, AggregationTypeSum)
	StatSecondaryRowBytes = NewStatisticInt64(StatNameSecondaryRowBytes, AggregationTypeSum)

	// Download/Page scan statistics
	StatPagesScanned          = NewStatisticInt64(StatNamePagesScanned, AggregationTypeSum)
	StatPagesFoundInCache     = NewStatisticInt64(StatNamePagesFoundInCache, AggregationTypeSum)
	StatBatchDownloadRequests = NewStatisticInt64(StatNameBatchDownloadRequests, AggregationTypeSum)
	StatPageDownloadTime      = NewStatisticInt64(StatNamePageDownloadTime, AggregationTypeSum)

	// Page download byte statistics
	StatPrimaryColumnPagesDownloaded     = NewStatisticInt64(StatNamePrimaryColumnPagesDownloaded, AggregationTypeSum)
	StatSecondaryColumnPagesDownloaded   = NewStatisticInt64(StatNameSecondaryColumnPagesDownloaded, AggregationTypeSum)
	StatPrimaryColumnBytes               = NewStatisticInt64(StatNamePrimaryColumnBytes, AggregationTypeSum)
	StatSecondaryColumnBytes             = NewStatisticInt64(StatNameSecondaryColumnBytes, AggregationTypeSum)
	StatPrimaryColumnUncompressedBytes   = NewStatisticInt64(StatNamePrimaryColumnUncompressedBytes, AggregationTypeSum)
	StatSecondaryColumnUncompressedBytes = NewStatisticInt64(StatNameSecondaryColumnUncompressedBytes, AggregationTypeSum)

	// Read operation statistics
	StatReadCalls = NewStatisticInt64(StatNameReadCalls, AggregationTypeSum)
)

// Metastore statistic names.
const (
	StatNameMetastoreIndexObjects     = "metastore.index.objects"
	StatNameMetastoreResolvedSections = "metastore.resolved.sections"
)

// Metastore statistics.
var (
	StatMetastoreIndexObjects     = NewStatisticInt64(StatNameMetastoreIndexObjects, AggregationTypeSum)
	StatMetastoreResolvedSections = NewStatisticInt64(StatNameMetastoreResolvedSections, AggregationTypeSum)
)

