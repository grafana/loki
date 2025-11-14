package executor

import "github.com/grafana/loki/v3/pkg/xcap"

// xcap statistics used in executor pkg.
var (
	// Common statistics tracked for all pipeline nodes.
	statRowsOut      = xcap.NewStatisticInt64("rows_out", xcap.AggregationTypeSum)
	statReadCalls    = xcap.NewStatisticInt64("read_calls", xcap.AggregationTypeSum)
	statReadDuration = xcap.NewStatisticInt64("read_duration_ns", xcap.AggregationTypeSum)

	// [ColumnCompat] statistics.
	statCompatCollisionFound = xcap.NewStatisticFlag("collision_found")

	// [DataObjScan] statistics
	statDatasetPrimaryColumns       = xcap.NewStatisticInt64("dataset_primary_columns", xcap.AggregationTypeSum)
	statDatasetSecondaryColumns     = xcap.NewStatisticInt64("dataset_secondary_columns", xcap.AggregationTypeSum)
	statDatasetPrimaryColumnPages   = xcap.NewStatisticInt64("dataset_primary_column_pages", xcap.AggregationTypeSum)
	statDatasetSecondaryColumnPages = xcap.NewStatisticInt64("dataset_secondary_column_pages", xcap.AggregationTypeSum)
	statDatasetMaxRows              = xcap.NewStatisticInt64("dataset_max_rows", xcap.AggregationTypeSum)
	statDatasetRowsAfterPruning     = xcap.NewStatisticInt64("dataset_rows_after_pruning", xcap.AggregationTypeSum)

	statDatasetPrimaryRowsRead   = xcap.NewStatisticInt64("primary_rows_read", xcap.AggregationTypeSum)
	statDatasetSecondaryRowsRead = xcap.NewStatisticInt64("secondary_rows_read", xcap.AggregationTypeSum)
	statDatasetPrimaryRowBytes   = xcap.NewStatisticInt64("primary_row_bytes_read", xcap.AggregationTypeSum)
	statDatasetSecondaryRowBytes = xcap.NewStatisticInt64("secondary_row_bytes_read", xcap.AggregationTypeSum)

	statDatasetPagesScanned          = xcap.NewStatisticInt64("pages_scanned", xcap.AggregationTypeSum)
	statDatasetPagesFoundInCache     = xcap.NewStatisticInt64("pages_found_in_cache", xcap.AggregationTypeSum)
	statDatasetBatchDownloadRequests = xcap.NewStatisticInt64("batch_download_requests", xcap.AggregationTypeSum)
	statDatasetPageDownloadTime      = xcap.NewStatisticInt64("page_download_time_nanos", xcap.AggregationTypeSum)

	statDatasetPrimaryColumnBytes               = xcap.NewStatisticInt64("primary_column_bytes", xcap.AggregationTypeSum)
	statDatasetSecondaryColumnBytes             = xcap.NewStatisticInt64("secondary_column_bytes", xcap.AggregationTypeSum)
	statDatasetPrimaryColumnUncompressedBytes   = xcap.NewStatisticInt64("primary_column_uncompressed_bytes", xcap.AggregationTypeSum)
	statDatasetSecondaryColumnUncompressedBytes = xcap.NewStatisticInt64("secondary_column_uncompressed_bytes", xcap.AggregationTypeSum)
)
