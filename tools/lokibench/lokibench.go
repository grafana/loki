package lokibench

import (
	"fmt"

	"github.com/dustin/go-humanize"
)

// Response represents the top-level Loki API response
type Response struct {
	Status string       `json:"status"`
	Data   ResponseData `json:"data"`
}

// ResponseData represents the data field in Loki's response
type ResponseData struct {
	Stats ResponseStats `json:"stats"`
}

// ResponseStats represents the statistics returned by Loki
type ResponseStats struct {
	Summary struct {
		TotalBytesProcessed int64 `json:"totalBytesProcessed"`
		TotalLinesProcessed int64 `json:"totalLinesProcessed"`
	} `json:"summary"`
	Querier struct {
		Store struct {
			TotalChunksRef        int64 `json:"totalChunksRef"`
			TotalChunksDownloaded int64 `json:"totalChunksDownloaded"`
			Chunk                 struct {
				DecompressedLines int64 `json:"decompressedLines"`
				DecompressedBytes int64 `json:"decompressedBytes"`
				TotalDuplicates   int64 `json:"totalDuplicates"`
			} `json:"chunk"`
		} `json:"store"`
	} `json:"querier"`
	Index struct {
		TotalChunks      int64 `json:"totalChunks"`
		PostFilterChunks int64 `json:"postFilterChunks"`
	} `json:"index"`
}

type StatsDiff struct {
	BytesProcessedDiff        int64
	ChunksRefDiff             int64
	ChunksDownloadedDiff      int64
	BytesProcessedPercentage  float64
	ChunksRefPercentage       float64
	ChunksDownloadedPercent   float64
	PrimaryBytesProcessed     int64
	SecondaryBytesProcessed   int64
	PrimaryChunksRef          int64
	SecondaryChunksRef        int64
	PrimaryChunksDownloaded   int64
	SecondaryChunksDownloaded int64

	LinesProcessedDiff       int64
	LinesProcessedPercentage float64
	PrimaryLinesProcessed    int64
	SecondaryLinesProcessed  int64

	TotalChunksDiff       int64
	TotalChunksPercentage float64
	PrimaryTotalChunks    int64
	SecondaryTotalChunks  int64

	PostFilterChunksDiff       int64
	PostFilterChunksPercentage float64
	PrimaryPostFilterChunks    int64
	SecondaryPostFilterChunks  int64

	// New chunk stats
	DecompressedLinesDiff       int64
	DecompressedLinesPercentage float64
	PrimaryDecompressedLines    int64
	SecondaryDecompressedLines  int64

	DecompressedBytesDiff       int64
	DecompressedBytesPercentage float64
	PrimaryDecompressedBytes    int64
	SecondaryDecompressedBytes  int64

	TotalDuplicatesDiff       int64
	TotalDuplicatesPercentage float64
	PrimaryTotalDuplicates    int64
	SecondaryTotalDuplicates  int64
}

// Add this new type for accumulated stats
type StatsAccumulator struct {
	TotalQueries int64
	Primary      ResponseStats
	Secondary    ResponseStats
}

func NewStatsAccumulator() *StatsAccumulator {
	return &StatsAccumulator{}
}

func (a *StatsAccumulator) Add(primary, secondary *Response) {
	a.TotalQueries++

	// Accumulate primary stats
	a.Primary.Summary.TotalBytesProcessed += primary.Data.Stats.Summary.TotalBytesProcessed
	a.Primary.Summary.TotalLinesProcessed += primary.Data.Stats.Summary.TotalLinesProcessed
	a.Primary.Querier.Store.TotalChunksRef += primary.Data.Stats.Querier.Store.TotalChunksRef
	a.Primary.Querier.Store.TotalChunksDownloaded += primary.Data.Stats.Querier.Store.TotalChunksDownloaded
	a.Primary.Index.TotalChunks += primary.Data.Stats.Index.TotalChunks
	a.Primary.Index.PostFilterChunks += primary.Data.Stats.Index.PostFilterChunks

	// Accumulate secondary stats
	a.Secondary.Summary.TotalBytesProcessed += secondary.Data.Stats.Summary.TotalBytesProcessed
	a.Secondary.Summary.TotalLinesProcessed += secondary.Data.Stats.Summary.TotalLinesProcessed
	a.Secondary.Querier.Store.TotalChunksRef += secondary.Data.Stats.Querier.Store.TotalChunksRef
	a.Secondary.Querier.Store.TotalChunksDownloaded += secondary.Data.Stats.Querier.Store.TotalChunksDownloaded
	a.Secondary.Index.TotalChunks += secondary.Data.Stats.Index.TotalChunks
	a.Secondary.Index.PostFilterChunks += secondary.Data.Stats.Index.PostFilterChunks

	// Accumulate primary chunk stats
	a.Primary.Querier.Store.Chunk.DecompressedLines += primary.Data.Stats.Querier.Store.Chunk.DecompressedLines
	a.Primary.Querier.Store.Chunk.DecompressedBytes += primary.Data.Stats.Querier.Store.Chunk.DecompressedBytes
	a.Primary.Querier.Store.Chunk.TotalDuplicates += primary.Data.Stats.Querier.Store.Chunk.TotalDuplicates

	// Accumulate secondary chunk stats
	a.Secondary.Querier.Store.Chunk.DecompressedLines += secondary.Data.Stats.Querier.Store.Chunk.DecompressedLines
	a.Secondary.Querier.Store.Chunk.DecompressedBytes += secondary.Data.Stats.Querier.Store.Chunk.DecompressedBytes
	a.Secondary.Querier.Store.Chunk.TotalDuplicates += secondary.Data.Stats.Querier.Store.Chunk.TotalDuplicates
}

func (a *StatsAccumulator) String() string {
	// Create mock Response objects to reuse existing comparison logic
	primary := &Response{
		Data: ResponseData{
			Stats: a.Primary,
		},
	}
	secondary := &Response{
		Data: ResponseData{
			Stats: a.Secondary,
		},
	}

	diff := CompareStats(primary, secondary)

	// Calculate duplicate percentages for accumulated stats
	primaryDuplicatePercent := float64(0)
	if a.Primary.Querier.Store.Chunk.DecompressedLines > 0 {
		primaryDuplicatePercent = float64(a.Primary.Querier.Store.Chunk.TotalDuplicates) / float64(a.Primary.Querier.Store.Chunk.DecompressedLines) * 100
	}
	secondaryDuplicatePercent := float64(0)
	if a.Secondary.Querier.Store.Chunk.DecompressedLines > 0 {
		secondaryDuplicatePercent = float64(a.Secondary.Querier.Store.Chunk.TotalDuplicates) / float64(a.Secondary.Querier.Store.Chunk.DecompressedLines) * 100
	}

	return fmt.Sprintf(`
Accumulated Statistics Over %d Queries:
----------------------------------------
Bytes Processed:      %+.2f%% [Primary: %s, Secondary: %s]
Lines Processed:      %+.2f%% [Primary: %d, Secondary: %d]
Chunks Referenced:    %+.2f%% [Primary: %d, Secondary: %d]
Chunks Downloaded:    %+.2f%% [Primary: %d, Secondary: %d]
Total Index Chunks:   %+.2f%% [Primary: %d, Secondary: %d]
Post-Filter Chunks:   %+.2f%% [Primary: %d, Secondary: %d]
Decompressed Lines:   %+.2f%% [Primary: %d, Secondary: %d]
Decompressed Bytes:   %+.2f%% [Primary: %s, Secondary: %s]
Duplicate Lines:      [Primary: %.2f%%, Secondary: %.2f%%]`,
		a.TotalQueries,
		diff.BytesProcessedPercentage,
		humanize.Bytes(uint64(diff.PrimaryBytesProcessed)), humanize.Bytes(uint64(diff.SecondaryBytesProcessed)),
		diff.LinesProcessedPercentage,
		diff.PrimaryLinesProcessed, diff.SecondaryLinesProcessed,
		diff.ChunksRefPercentage,
		diff.PrimaryChunksRef, diff.SecondaryChunksRef,
		diff.ChunksDownloadedPercent,
		diff.PrimaryChunksDownloaded, diff.SecondaryChunksDownloaded,
		diff.TotalChunksPercentage,
		diff.PrimaryTotalChunks, diff.SecondaryTotalChunks,
		diff.PostFilterChunksPercentage,
		diff.PrimaryPostFilterChunks, diff.SecondaryPostFilterChunks,
		diff.DecompressedLinesPercentage,
		diff.PrimaryDecompressedLines, diff.SecondaryDecompressedLines,
		diff.DecompressedBytesPercentage,
		humanize.Bytes(uint64(diff.PrimaryDecompressedBytes)), humanize.Bytes(uint64(diff.SecondaryDecompressedBytes)),
		primaryDuplicatePercent, secondaryDuplicatePercent)
}

func CompareStats(primary, secondary *Response) StatsDiff {
	diff := StatsDiff{
		BytesProcessedDiff:        secondary.Data.Stats.Summary.TotalBytesProcessed - primary.Data.Stats.Summary.TotalBytesProcessed,
		ChunksRefDiff:             secondary.Data.Stats.Querier.Store.TotalChunksRef - primary.Data.Stats.Querier.Store.TotalChunksRef,
		ChunksDownloadedDiff:      secondary.Data.Stats.Querier.Store.TotalChunksDownloaded - primary.Data.Stats.Querier.Store.TotalChunksDownloaded,
		PrimaryBytesProcessed:     primary.Data.Stats.Summary.TotalBytesProcessed,
		SecondaryBytesProcessed:   secondary.Data.Stats.Summary.TotalBytesProcessed,
		PrimaryChunksRef:          primary.Data.Stats.Querier.Store.TotalChunksRef,
		SecondaryChunksRef:        secondary.Data.Stats.Querier.Store.TotalChunksRef,
		PrimaryChunksDownloaded:   primary.Data.Stats.Querier.Store.TotalChunksDownloaded,
		SecondaryChunksDownloaded: secondary.Data.Stats.Querier.Store.TotalChunksDownloaded,

		LinesProcessedDiff:      secondary.Data.Stats.Summary.TotalLinesProcessed - primary.Data.Stats.Summary.TotalLinesProcessed,
		PrimaryLinesProcessed:   primary.Data.Stats.Summary.TotalLinesProcessed,
		SecondaryLinesProcessed: secondary.Data.Stats.Summary.TotalLinesProcessed,

		TotalChunksDiff:      secondary.Data.Stats.Index.TotalChunks - primary.Data.Stats.Index.TotalChunks,
		PrimaryTotalChunks:   primary.Data.Stats.Index.TotalChunks,
		SecondaryTotalChunks: secondary.Data.Stats.Index.TotalChunks,

		PostFilterChunksDiff:      secondary.Data.Stats.Index.PostFilterChunks - primary.Data.Stats.Index.PostFilterChunks,
		PrimaryPostFilterChunks:   primary.Data.Stats.Index.PostFilterChunks,
		SecondaryPostFilterChunks: secondary.Data.Stats.Index.PostFilterChunks,

		// New chunk comparisons
		DecompressedLinesDiff:      secondary.Data.Stats.Querier.Store.Chunk.DecompressedLines - primary.Data.Stats.Querier.Store.Chunk.DecompressedLines,
		PrimaryDecompressedLines:   primary.Data.Stats.Querier.Store.Chunk.DecompressedLines,
		SecondaryDecompressedLines: secondary.Data.Stats.Querier.Store.Chunk.DecompressedLines,

		DecompressedBytesDiff:      secondary.Data.Stats.Querier.Store.Chunk.DecompressedBytes - primary.Data.Stats.Querier.Store.Chunk.DecompressedBytes,
		PrimaryDecompressedBytes:   primary.Data.Stats.Querier.Store.Chunk.DecompressedBytes,
		SecondaryDecompressedBytes: secondary.Data.Stats.Querier.Store.Chunk.DecompressedBytes,

		TotalDuplicatesDiff:      secondary.Data.Stats.Querier.Store.Chunk.TotalDuplicates - primary.Data.Stats.Querier.Store.Chunk.TotalDuplicates,
		PrimaryTotalDuplicates:   primary.Data.Stats.Querier.Store.Chunk.TotalDuplicates,
		SecondaryTotalDuplicates: secondary.Data.Stats.Querier.Store.Chunk.TotalDuplicates,
	}

	// Calculate percentages using primary as reference
	if primary.Data.Stats.Summary.TotalBytesProcessed > 0 {
		diff.BytesProcessedPercentage = float64(diff.BytesProcessedDiff) / float64(primary.Data.Stats.Summary.TotalBytesProcessed) * 100
	}
	if primary.Data.Stats.Querier.Store.TotalChunksRef > 0 {
		diff.ChunksRefPercentage = float64(diff.ChunksRefDiff) / float64(primary.Data.Stats.Querier.Store.TotalChunksRef) * 100
	}
	if primary.Data.Stats.Querier.Store.TotalChunksDownloaded > 0 {
		diff.ChunksDownloadedPercent = float64(diff.ChunksDownloadedDiff) / float64(primary.Data.Stats.Querier.Store.TotalChunksDownloaded) * 100
	}
	if primary.Data.Stats.Summary.TotalLinesProcessed > 0 {
		diff.LinesProcessedPercentage = float64(diff.LinesProcessedDiff) / float64(primary.Data.Stats.Summary.TotalLinesProcessed) * 100
	}
	if primary.Data.Stats.Index.TotalChunks > 0 {
		diff.TotalChunksPercentage = float64(diff.TotalChunksDiff) / float64(primary.Data.Stats.Index.TotalChunks) * 100
	}
	if primary.Data.Stats.Index.PostFilterChunks > 0 {
		diff.PostFilterChunksPercentage = float64(diff.PostFilterChunksDiff) / float64(primary.Data.Stats.Index.PostFilterChunks) * 100
	}

	// Calculate percentages for new chunk stats
	if primary.Data.Stats.Querier.Store.Chunk.DecompressedLines > 0 {
		diff.DecompressedLinesPercentage = float64(diff.DecompressedLinesDiff) / float64(primary.Data.Stats.Querier.Store.Chunk.DecompressedLines) * 100
	}
	if primary.Data.Stats.Querier.Store.Chunk.DecompressedBytes > 0 {
		diff.DecompressedBytesPercentage = float64(diff.DecompressedBytesDiff) / float64(primary.Data.Stats.Querier.Store.Chunk.DecompressedBytes) * 100
	}
	if primary.Data.Stats.Querier.Store.Chunk.TotalDuplicates > 0 {
		diff.TotalDuplicatesPercentage = float64(diff.TotalDuplicatesDiff) / float64(primary.Data.Stats.Querier.Store.Chunk.TotalDuplicates) * 100
	}

	return diff
}

func (s StatsDiff) String() string {
	// Calculate duplicate percentages for each backend
	primaryDuplicatePercent := float64(0)
	if s.PrimaryDecompressedLines > 0 {
		primaryDuplicatePercent = float64(s.PrimaryTotalDuplicates) / float64(s.PrimaryDecompressedLines) * 100
	}
	secondaryDuplicatePercent := float64(0)
	if s.SecondaryDecompressedLines > 0 {
		secondaryDuplicatePercent = float64(s.SecondaryTotalDuplicates) / float64(s.SecondaryDecompressedLines) * 100
	}

	return fmt.Sprintf(`Statistics Comparison (Primary vs Secondary):
Bytes Processed:      %+.2f%% [Primary: %s, Secondary: %s]
Lines Processed:      %+.2f%% [Primary: %d, Secondary: %d]
Chunks Referenced:    %+.2f%% [Primary: %d, Secondary: %d]
Chunks Downloaded:    %+.2f%% [Primary: %d, Secondary: %d]
Total Index Chunks:   %+.2f%% [Primary: %d, Secondary: %d]
Post-Filter Chunks:   %+.2f%% [Primary: %d, Secondary: %d]
Decompressed Lines:   %+.2f%% [Primary: %d, Secondary: %d]
Decompressed Bytes:   %+.2f%% [Primary: %s, Secondary: %s]
Duplicate Lines:      [Primary: %.2f%%, Secondary: %.2f%%]`,
		s.BytesProcessedPercentage,
		humanize.Bytes(uint64(s.PrimaryBytesProcessed)), humanize.Bytes(uint64(s.SecondaryBytesProcessed)),
		s.LinesProcessedPercentage,
		s.PrimaryLinesProcessed, s.SecondaryLinesProcessed,
		s.ChunksRefPercentage,
		s.PrimaryChunksRef, s.SecondaryChunksRef,
		s.ChunksDownloadedPercent,
		s.PrimaryChunksDownloaded, s.SecondaryChunksDownloaded,
		s.TotalChunksPercentage,
		s.PrimaryTotalChunks, s.SecondaryTotalChunks,
		s.PostFilterChunksPercentage,
		s.PrimaryPostFilterChunks, s.SecondaryPostFilterChunks,
		s.DecompressedLinesPercentage,
		s.PrimaryDecompressedLines, s.SecondaryDecompressedLines,
		s.DecompressedBytesPercentage,
		humanize.Bytes(uint64(s.PrimaryDecompressedBytes)), humanize.Bytes(uint64(s.SecondaryDecompressedBytes)),
		primaryDuplicatePercent, secondaryDuplicatePercent)
}
