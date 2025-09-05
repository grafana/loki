package indexpointers

import (
	"context"
	"fmt"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

type (
	// Stats provides statistics about a indexpointers section.
	Stats struct {
		UncompressedSize uint64
		CompressedSize   uint64

		MinTimestamp          time.Time
		MaxTimestamp          time.Time
		TimestampDistribution []uint64 // Indexpointer count per hour.

		Columns []ColumnStats
	}

	// ColumnStats provides statistics about a column in a section.
	ColumnStats struct {
		Name             string
		Type             string
		ValueType        string
		RowsCount        uint64
		Compression      string
		UncompressedSize uint64
		CompressedSize   uint64
		MetadataOffset   uint64
		MetadataSize     uint64
		ValuesCount      uint64
		Cardinality      uint64

		Pages []PageStats
	}

	// PageStats provides statistics about a page in a column.
	PageStats struct {
		UncompressedSize uint64
		CompressedSize   uint64
		CRC32            uint32
		RowsCount        uint64
		Encoding         string
		DataOffset       uint64
		DataSize         uint64
		ValuesCount      uint64
	}
)

// ReadStats returns statistics about the indexpointers section. ReadStats returns an
// error if the indexpointers section couldn't be inspected or if the provided ctx is
// canceled.
func ReadStats(ctx context.Context, section *Section) (Stats, error) {
	var stats Stats

	dec := section.inner.Decoder()
	metadata, err := dec.SectionMetadata(ctx)
	if err != nil {
		return stats, fmt.Errorf("reading metadata: %w", err)
	}

	// Collect all the page descriptions at once for quick stats calculation.
	pageSets, err := result.Collect(dec.Pages(ctx, metadata.GetColumns()))
	if err != nil {
		return stats, fmt.Errorf("reading pages: %w", err)
	}

	for i, col := range section.Columns() {
		md := col.inner.Metadata()

		stats.CompressedSize += md.CompressedSize
		stats.UncompressedSize += md.UncompressedSize

		switch {
		case col.Type == ColumnTypeMinTimestamp && md.Statistics != nil:
			var ts dataset.Value
			if err := ts.UnmarshalBinary(md.Statistics.MinValue); err != nil {
				return stats, fmt.Errorf("unmarshalling min timestamp: %w", err)
			}
			stats.MinTimestamp = time.Unix(0, ts.Int64())

		case col.Type == ColumnTypeMaxTimestamp && md.Statistics != nil:
			var ts dataset.Value
			if err := ts.UnmarshalBinary(md.Statistics.MaxValue); err != nil {
				return stats, fmt.Errorf("unmarshalling max timestamp: %w", err)
			}
			stats.MaxTimestamp = time.Unix(0, ts.Int64())
		}

		columnStats := ColumnStats{
			Name:             col.Name,
			Type:             col.Type.String(),
			ValueType:        col.inner.Type.Physical.String(),
			RowsCount:        md.RowsCount,
			Compression:      md.Compression.String(),
			UncompressedSize: md.UncompressedSize,
			CompressedSize:   md.CompressedSize,
			MetadataOffset:   md.ColumnMetadataOffset,
			MetadataSize:     md.ColumnMetadataLength,
			ValuesCount:      md.ValuesCount,
			Cardinality:      md.Statistics.GetCardinalityCount(),
		}

		for _, pages := range pageSets[i] {
			columnStats.Pages = append(columnStats.Pages, PageStats{
				UncompressedSize: pages.UncompressedSize,
				CompressedSize:   pages.CompressedSize,
				CRC32:            pages.Crc32,
				RowsCount:        pages.RowsCount,
				Encoding:         pages.Encoding.String(),
				DataOffset:       pages.DataOffset,
				DataSize:         pages.DataSize,
				ValuesCount:      pages.ValuesCount,
			})
		}

		stats.Columns = append(stats.Columns, columnStats)
	}

	return stats, nil
}
