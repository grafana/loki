package dataset

import (
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

type DownloadStats struct {
	// Use the following three stats together.
	// 1. ReadPageCalls is the total number of ReadPage calls made to the downloader.
	// 2. PagesFoundInCache is the number of pages that were found in cache and
	//    did not require a download.
	// 3. BatchDownloadRequests is the number of batch download requests made by
	//    the downloader when a page is not found in cache.
	ReadPageCalls         uint64
	PagesFoundInCache     uint64
	BatchDownloadRequests uint64

	PrimaryColumnPages               uint64 // Number of pages downloaded for primary columns
	SecondaryColumnPages             uint64 // Number of pages downloaded for secondary columns
	PrimaryColumnBytes               uint64 // Total bytes downloaded for primary columns
	SecondaryColumnBytes             uint64 // Total bytes downloaded for secondary columns
	PrimaryColumnUncompressedBytes   uint64 // Total uncompressed bytes for primary columns
	SecondaryColumnUncompressedBytes uint64 // Total uncompressed bytes for secondary columns
}

func (ds *DownloadStats) Reset() {
	ds.ReadPageCalls = 0
	ds.PagesFoundInCache = 0
	ds.BatchDownloadRequests = 0

	ds.PrimaryColumnPages = 0
	ds.SecondaryColumnPages = 0
	ds.PrimaryColumnBytes = 0
	ds.SecondaryColumnBytes = 0
	ds.PrimaryColumnUncompressedBytes = 0
	ds.SecondaryColumnUncompressedBytes = 0
}

// ReadStats tracks statistics about dataset read operations.
type ReadStats struct {
	ReadCalls int // Total number of read calls made to the reader

	// Column statistics
	PrimaryColumns   int // Number of primary columns
	SecondaryColumns int // Number of secondary columns

	// Page statistics
	PrimaryColumnPages   uint64        // Total pages in primary columns
	SecondaryColumnPages uint64        // Total pages in secondary columns
	DownloadStats        DownloadStats // Download statistics for primary and secondary columns

	// Row statistics
	MaxRows                uint64 // Maximum number of rows across all columns
	RowsToReadAfterPruning uint64 // Total number of primary rows to read after page pruning

	PrimaryRowsRead   uint64
	SecondaryRowsRead uint64

	PrimaryRowBytes   uint64 // Total bytes read for primary rows
	SecondaryRowBytes uint64 // Total bytes read for secondary rows
}

func (s *ReadStats) Reset() {
	s.ReadCalls = 0
	s.PrimaryColumns = 0
	s.SecondaryColumns = 0
	s.MaxRows = 0
	s.RowsToReadAfterPruning = 0
	s.PrimaryRowsRead = 0
	s.SecondaryRowsRead = 0
	s.PrimaryRowBytes = 0
	s.SecondaryRowBytes = 0
	s.PrimaryColumnPages = 0
	s.SecondaryColumnPages = 0
	s.DownloadStats.Reset()
}

// LogSummary logs a summary of the read statistics to the provided logger.
func (s *ReadStats) LogSummary(logger log.Logger, duration time.Duration) {
	logValues := make([]any, 0, 30)
	logValues = append(logValues, "msg", "dataset reader stats",
		"duration", duration,
		"read_calls", s.ReadCalls,
		"primary_columns", s.PrimaryColumns,
		"secondary_columns", s.SecondaryColumns,

		"max_rows", s.MaxRows,
		"rows_to_read_after_pruning", s.RowsToReadAfterPruning,
		"primary_rows_read", s.PrimaryRowsRead,
		"secondary_rows_read", s.SecondaryRowsRead,

		"primary_column_pages", s.PrimaryColumnPages,
		"secondary_column_pages", s.SecondaryColumnPages,

		"total_pages_read", s.DownloadStats.ReadPageCalls,
		"pages_found_in_cache", s.DownloadStats.PagesFoundInCache,
		"batch_download_requests", s.DownloadStats.BatchDownloadRequests,

		"primary_pages_downloaded", s.DownloadStats.PrimaryColumnPages,
		"secondary_pages_downloaded", s.DownloadStats.SecondaryColumnPages,
		"primary_page_bytes_downloaded", humanize.Bytes(s.DownloadStats.PrimaryColumnBytes),
		"secondary_page_bytes_downloaded", humanize.Bytes(s.DownloadStats.SecondaryColumnBytes),
	)

	level.Debug(logger).Log(logValues...)
}
