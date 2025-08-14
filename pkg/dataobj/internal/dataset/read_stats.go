package dataset

import (
	"context"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
)

// DownloadStats holds statistics about the download operations performed by the dataset reader.
type DownloadStats struct {
	// Use the following three stats together.
	// 1. PagesScanned is the total number of ReadPage calls made to the downloader.
	// 2. PagesFoundInCache is the number of pages that were found in cache and
	//    did not require a download.
	// 3. BatchDownloadRequests is the number of batch download requests made by
	//    the downloader when a page is not found in cache.
	PagesScanned          uint64
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
	ds.PagesScanned = 0
	ds.PagesFoundInCache = 0
	ds.BatchDownloadRequests = 0

	ds.PrimaryColumnPages = 0
	ds.SecondaryColumnPages = 0
	ds.PrimaryColumnBytes = 0
	ds.SecondaryColumnBytes = 0
	ds.PrimaryColumnUncompressedBytes = 0
	ds.SecondaryColumnUncompressedBytes = 0
}

// ReaderStats tracks statistics about dataset read operations.
type ReaderStats struct {
	// TODO(ashwanth): global stats from [stats.Context] can be updated by the
	// engine at the end of the query execution once we have a stats collection
	// framework integrated with the execution pipeline.
	// Below reference is a temporary stop gap.
	//
	// Reference to [stats.Context] to update relevant global statistics.
	globalStats *stats.Context

	// Column statistics
	PrimaryColumns   uint64 // Number of primary columns to read from the dataset
	SecondaryColumns uint64 // Number of secondary columns to read from the dataset

	// Page statistics
	PrimaryColumnPages   uint64        // Total pages in primary columns
	SecondaryColumnPages uint64        // Total pages in secondary columns
	DownloadStats        DownloadStats // Download statistics for primary and secondary columns

	ReadCalls int64 // Total number of read calls made to the reader

	// Row statistics
	MaxRows                uint64 // Maximum number of rows across all columns
	RowsToReadAfterPruning uint64 // Total number of primary rows to read after page pruning

	PrimaryRowsRead   uint64 // Actual number of primary rows read.
	SecondaryRowsRead uint64 // Actual number of secondary rows read.

	PrimaryRowBytes   uint64 // Total bytes read for primary rows
	SecondaryRowBytes uint64 // Total bytes read for secondary rows
}

type ctxKeyType string

const (
	readerStatsKey ctxKeyType = "reader_stats"
)

func WithStats(ctx context.Context, stats *ReaderStats) context.Context {
	if stats == nil {
		return ctx
	}

	return context.WithValue(ctx, readerStatsKey, stats)
}

// StatsFromContext returns the reader stats from context.
func StatsFromContext(ctx context.Context) *ReaderStats {
	v, ok := ctx.Value(readerStatsKey).(*ReaderStats)
	if !ok {
		return &ReaderStats{}
	}
	return v
}

// IsStatsPresent returns true if reader stats are present in the context.
func IsStatsPresent(ctx context.Context) bool {
	_, ok := ctx.Value(readerStatsKey).(*ReaderStats)
	return ok
}

func (s *ReaderStats) LinkGlobalStats(stats *stats.Context) {
	// If the global stats are already set, we don't override it.
	if s.globalStats == nil {
		s.globalStats = stats
	}
}

func (s *ReaderStats) AddReadCalls(count int) {
	s.ReadCalls += int64(count)
}

func (s *ReaderStats) AddPrimaryColumns(count uint64) {
	s.PrimaryColumns += count
}

func (s *ReaderStats) AddSecondaryColumns(count uint64) {
	s.SecondaryColumns += count
}

func (s *ReaderStats) AddPrimaryColumnPages(count uint64) {
	s.PrimaryColumnPages += count
}

func (s *ReaderStats) AddSecondaryColumnPages(count uint64) {
	s.SecondaryColumnPages += count
}

func (s *ReaderStats) AddPrimaryRowsRead(count uint64) {
	s.PrimaryRowsRead += count
	if s.globalStats != nil {
		s.globalStats.AddPrePredicateDecompressedRows(int64(count))
	}
}

func (s *ReaderStats) AddSecondaryRowsRead(count uint64) {
	s.SecondaryRowsRead += count
	if s.globalStats != nil {
		s.globalStats.AddPostPredicateRows(int64(count))
	}
}

func (s *ReaderStats) AddPrimaryRowBytes(count uint64) {
	s.PrimaryRowBytes += count
	if s.globalStats != nil {
		s.globalStats.AddPrePredicateDecompressedBytes(int64(count))
	}
}

func (s *ReaderStats) AddSecondaryRowBytes(count uint64) {
	s.SecondaryRowBytes += count
	if s.globalStats != nil {
		s.globalStats.AddPostPredicateDecompressedBytes(int64(count))
	}
}

func (s *ReaderStats) AddMaxRows(count uint64) {
	s.MaxRows += count
}

func (s *ReaderStats) AddRowsToReadAfterPruning(count uint64) {
	s.RowsToReadAfterPruning += count
}

func (s *ReaderStats) AddTotalRowsAvailable(count int64) {
	s.MaxRows += uint64(count)
	if s.globalStats != nil {
		s.globalStats.AddTotalRowsAvailable(count)
	}
}

func (s *ReaderStats) AddPagesScanned(count uint64) {
	s.DownloadStats.PagesScanned += count
	if s.globalStats != nil {
		s.globalStats.AddPagesScanned(int64(count))
	}
}

func (s *ReaderStats) AddPagesFoundInCache(count uint64) {
	s.DownloadStats.PagesFoundInCache += count
}

func (s *ReaderStats) AddBatchDownloadRequests(count uint64) {
	s.DownloadStats.BatchDownloadRequests += count

	if s.globalStats != nil {
		s.globalStats.AddPageBatches(int64(count))
	}
}

func (s *ReaderStats) AddPrimaryColumnPagesDownloaded(count uint64) {
	s.DownloadStats.PrimaryColumnPages += count
	if s.globalStats != nil {
		s.globalStats.AddPagesDownloaded(int64(count))

	}
}

func (s *ReaderStats) AddSecondaryColumnPagesDownloaded(count uint64) {
	s.DownloadStats.SecondaryColumnPages += count
	if s.globalStats != nil {
		s.globalStats.AddPagesDownloaded(int64(count))

	}
}

func (s *ReaderStats) AddPrimaryColumnBytesDownloaded(bytes uint64) {
	s.DownloadStats.PrimaryColumnBytes += bytes
	if s.globalStats != nil {
		s.globalStats.AddPagesDownloadedBytes(int64(bytes))
	}
}

func (s *ReaderStats) AddSecondaryColumnBytesDownloaded(bytes uint64) {
	s.DownloadStats.SecondaryColumnBytes += bytes
	if s.globalStats != nil {
		s.globalStats.AddPagesDownloadedBytes(int64(bytes))
	}
}

func (s *ReaderStats) AddPrimaryColumnUncompressedBytes(count uint64) {
	s.DownloadStats.PrimaryColumnUncompressedBytes += count
}

func (s *ReaderStats) AddSecondaryColumnUncompressedBytes(count uint64) {
	s.DownloadStats.SecondaryColumnUncompressedBytes += count
}

func (s *ReaderStats) Reset() {
	s.PrimaryColumns = 0
	s.SecondaryColumns = 0

	s.PrimaryColumnPages = 0
	s.SecondaryColumnPages = 0
	s.DownloadStats.Reset()

	s.ReadCalls = 0

	s.MaxRows = 0
	s.RowsToReadAfterPruning = 0

	s.PrimaryRowsRead = 0
	s.SecondaryRowsRead = 0

	s.PrimaryRowBytes = 0
	s.SecondaryRowBytes = 0

	s.globalStats = nil // Reset the global stats reference
}

// LogSummary logs a summary of the read statistics to the provided logger.
func (s *ReaderStats) LogSummary(logger log.Logger, execDuration time.Duration) {
	logValues := make([]any, 0, 50)
	logValues = append(logValues, "msg", "dataset reader stats",
		"execution_duration", execDuration,
		"read_calls", s.ReadCalls,
		"max_rows", s.MaxRows,
		"rows_to_read_after_pruning", s.RowsToReadAfterPruning,
		"primary_column_rows_read", s.PrimaryRowsRead,
		"secondary_column_rows_read", s.SecondaryRowsRead,
		"primary_column_bytes_read", humanize.Bytes(s.PrimaryRowBytes),
		"secondary_column_bytes_read", humanize.Bytes(s.SecondaryRowBytes),

		"primary_columns", s.PrimaryColumns,
		"secondary_columns", s.SecondaryColumns,
		"primary_column_pages", s.PrimaryColumnPages,
		"secondary_column_pages", s.SecondaryColumnPages,

		"total_pages_read", s.DownloadStats.PagesScanned,
		"pages_found_in_cache", s.DownloadStats.PagesFoundInCache,
		"batch_download_requests", s.DownloadStats.BatchDownloadRequests,

		"primary_pages_downloaded", s.DownloadStats.PrimaryColumnPages,
		"secondary_pages_downloaded", s.DownloadStats.SecondaryColumnPages,
		"primary_page_bytes_downloaded", humanize.Bytes(s.DownloadStats.PrimaryColumnBytes),
		"secondary_page_bytes_downloaded", humanize.Bytes(s.DownloadStats.SecondaryColumnBytes),
		"primary_page_uncompressed_bytes", humanize.Bytes(s.DownloadStats.PrimaryColumnUncompressedBytes),
		"secondary_page_uncompressed_bytes", humanize.Bytes(s.DownloadStats.SecondaryColumnUncompressedBytes),
	)

	level.Debug(logger).Log(logValues...)
}
