package dataset

import (
	"github.com/grafana/loki/v3/pkg/xcap"
)

// xcap statistics for dataset reader operations.
var (
	// Column statistics
	StatPrimaryColumns   = xcap.NewStatisticInt64("primary_columns", xcap.AggregationTypeSum)
	StatSecondaryColumns = xcap.NewStatisticInt64("secondary_columns", xcap.AggregationTypeSum)

	// Page statistics
	StatPrimaryColumnPages   = xcap.NewStatisticInt64("primary_column_pages", xcap.AggregationTypeSum)
	StatSecondaryColumnPages = xcap.NewStatisticInt64("secondary_column_pages", xcap.AggregationTypeSum)

	// Row statistics
	StatMaxRows           = xcap.NewStatisticInt64("max_rows", xcap.AggregationTypeSum)
	StatRowsAfterPruning  = xcap.NewStatisticInt64("rows_after_pruning", xcap.AggregationTypeSum)
	StatPrimaryRowsRead   = xcap.NewStatisticInt64("primary_rows_read", xcap.AggregationTypeSum)
	StatSecondaryRowsRead = xcap.NewStatisticInt64("secondary_rows_read", xcap.AggregationTypeSum)
	StatPrimaryRowBytes   = xcap.NewStatisticInt64("primary_row_bytes_read", xcap.AggregationTypeSum)
	StatSecondaryRowBytes = xcap.NewStatisticInt64("secondary_row_bytes_read", xcap.AggregationTypeSum)

	// Download/Page scan statistics
	StatPagesScanned          = xcap.NewStatisticInt64("pages_scanned", xcap.AggregationTypeSum)
	StatPagesFoundInCache     = xcap.NewStatisticInt64("pages_found_in_cache", xcap.AggregationTypeSum)
	StatBatchDownloadRequests = xcap.NewStatisticInt64("batch_download_requests", xcap.AggregationTypeSum)
	StatPageDownloadTime      = xcap.NewStatisticInt64("page_download_time_ns", xcap.AggregationTypeSum)

	// Page download byte statistics
	StatPrimaryColumnPagesDownloaded     = xcap.NewStatisticInt64("primary_column_pages_downloaded", xcap.AggregationTypeSum)
	StatSecondaryColumnPagesDownloaded   = xcap.NewStatisticInt64("secondary_column_pages_downloaded", xcap.AggregationTypeSum)
	StatPrimaryColumnBytes               = xcap.NewStatisticInt64("primary_column_bytes", xcap.AggregationTypeSum)
	StatSecondaryColumnBytes             = xcap.NewStatisticInt64("secondary_column_bytes", xcap.AggregationTypeSum)
	StatPrimaryColumnUncompressedBytes   = xcap.NewStatisticInt64("primary_column_uncompressed_bytes", xcap.AggregationTypeSum)
	StatSecondaryColumnUncompressedBytes = xcap.NewStatisticInt64("secondary_column_uncompressed_bytes", xcap.AggregationTypeSum)

	// Read operation statistics
	StatReadCalls = xcap.NewStatisticInt64("read_calls", xcap.AggregationTypeSum)
)
