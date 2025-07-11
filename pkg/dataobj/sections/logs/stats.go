package logs

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

type (
	// Stats provides statistics about a streams section.
	Stats struct {
		UncompressedSize uint64
		CompressedSize   uint64

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
		ColumnIndex      int64

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

// ReadStats returns statistics about the logs section. ReadStats returns an
// error if the streams section couldn't be inspected or if the provided ctx is
// canceled.
func ReadStats(ctx context.Context, section *Section) (Stats, error) {
	var stats Stats

	dec := newDecoder(section.reader)
	cols, err := dec.Columns(ctx)
	if err != nil {
		return stats, fmt.Errorf("reading columns")
	}

	pageSets, err := result.Collect(dec.Pages(ctx, cols))
	if err != nil {
		return stats, fmt.Errorf("reading pages: %w", err)
	}

	for i, col := range cols {
		stats.CompressedSize += col.Info.CompressedSize
		stats.UncompressedSize += col.Info.UncompressedSize

		columnStats := ColumnStats{
			Name:             col.Info.Name,
			Type:             col.Type.String(),
			ValueType:        col.Info.ValueType.String(),
			RowsCount:        col.Info.RowsCount,
			Compression:      col.Info.Compression.String(),
			UncompressedSize: col.Info.UncompressedSize,
			CompressedSize:   col.Info.CompressedSize,
			MetadataOffset:   col.Info.MetadataOffset,
			MetadataSize:     col.Info.MetadataSize,
			ValuesCount:      col.Info.ValuesCount,
			Cardinality:      col.Info.Statistics.GetCardinalityCount(),
			ColumnIndex:      int64(i),
		}

		for _, pages := range pageSets[i] {
			columnStats.Pages = append(columnStats.Pages, PageStats{
				UncompressedSize: pages.Info.UncompressedSize,
				CompressedSize:   pages.Info.CompressedSize,
				CRC32:            pages.Info.Crc32,
				RowsCount:        pages.Info.RowsCount,
				Encoding:         pages.Info.Encoding.String(),
				DataOffset:       pages.Info.DataOffset,
				DataSize:         pages.Info.DataSize,
				ValuesCount:      pages.Info.ValuesCount,
			})
		}

		stats.Columns = append(stats.Columns, columnStats)
	}

	return stats, nil
}
