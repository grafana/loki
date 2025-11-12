package executor

import "github.com/grafana/loki/v3/pkg/xcap"

// Statistics for all executor pipeline nodes.
// These are defined as package-level variables to ensure they are created once
// and can be compared with pointer equality (though the current implementation
// uses string-based UniqueIdentifier for comparison).
var (
	// Common statistics tracked for all pipeline nodes.
	statRowsOut      = xcap.NewStatisticInt64("rows_out", xcap.AggregationTypeSum)
	statReadCalls    = xcap.NewStatisticInt64("read_calls", xcap.AggregationTypeSum)
	statExecDuration = xcap.NewStatisticInt64("exec_duration_ns", xcap.AggregationTypeSum)

	// Column compatibility statistics.
	statCollisionFound = xcap.NewStatisticFlag("collision_found")

	// Dataset reader statistics - column counts and pages.
	statPrimaryColumns       = xcap.NewStatisticInt64("dataset_primary_columns", xcap.AggregationTypeSum)
	statSecondaryColumns     = xcap.NewStatisticInt64("dataset_secondary_columns", xcap.AggregationTypeSum)
	statPrimaryColumnPages   = xcap.NewStatisticInt64("dataset_primary_column_pages", xcap.AggregationTypeSum)
	statSecondaryColumnPages = xcap.NewStatisticInt64("dataset_secondary_column_pages", xcap.AggregationTypeSum)
	statMaxRows              = xcap.NewStatisticInt64("dataset_max_rows", xcap.AggregationTypeSum)

	// Dataset reader statistics - row processing.
	statRowsToReadAfterPruning = xcap.NewStatisticInt64("rows_to_read_after_pruning", xcap.AggregationTypeSum)
	statPrimaryRowsRead        = xcap.NewStatisticInt64("primary_rows_read", xcap.AggregationTypeSum)
	statSecondaryRowsRead      = xcap.NewStatisticInt64("secondary_rows_read", xcap.AggregationTypeSum)
	statPrimaryRowBytes        = xcap.NewStatisticInt64("primary_row_bytes_read", xcap.AggregationTypeSum)
	statSecondaryRowBytes      = xcap.NewStatisticInt64("secondary_row_bytes_read", xcap.AggregationTypeSum)

	// Dataset reader statistics - download and caching.
	statPagesScanned                     = xcap.NewStatisticInt64("pages_scanned", xcap.AggregationTypeSum)
	statPagesFoundInCache                = xcap.NewStatisticInt64("pages_found_in_cache", xcap.AggregationTypeSum)
	statBatchDownloadRequests            = xcap.NewStatisticInt64("batch_download_requests", xcap.AggregationTypeSum)
	statPageDownloadTime                 = xcap.NewStatisticInt64("page_download_time_nanos", xcap.AggregationTypeSum)
	statPrimaryColumnBytes               = xcap.NewStatisticInt64("primary_column_bytes", xcap.AggregationTypeSum)
	statSecondaryColumnBytes             = xcap.NewStatisticInt64("secondary_column_bytes", xcap.AggregationTypeSum)
	statPrimaryColumnUncompressedBytes   = xcap.NewStatisticInt64("primary_column_uncompressed_bytes", xcap.AggregationTypeSum)
	statSecondaryColumnUncompressedBytes = xcap.NewStatisticInt64("secondary_column_uncompressed_bytes", xcap.AggregationTypeSum)
)
