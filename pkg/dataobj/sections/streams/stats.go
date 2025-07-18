package streams

import (
	"context"
	"fmt"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

type (
	// Stats provides statistics about a streams section.
	Stats struct {
		UncompressedSize uint64
		CompressedSize   uint64

		MinTimestamp          time.Time
		MaxTimestamp          time.Time
		TimestampDistribution []uint64 // Stream count per hour.

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

// ReadStats returns statistics about the streams section. ReadStats returns an
// error if the streams section couldn't be inspected or if the provided ctx is
// canceled.
func ReadStats(ctx context.Context, section *Section) (Stats, error) {
	var stats Stats

	dec := newDecoder(section.reader)
	metadata, err := dec.Metadata(ctx)
	if err != nil {
		return stats, fmt.Errorf("reading metadata: %w", err)
	}
	cols := metadata.GetColumns()

	pageSets, err := result.Collect(dec.Pages(ctx, cols))
	if err != nil {
		return stats, fmt.Errorf("reading pages: %w", err)
	}

	for i, col := range cols {
		stats.CompressedSize += col.Info.CompressedSize
		stats.UncompressedSize += col.Info.UncompressedSize

		switch {
		case col.Type == streamsmd.COLUMN_TYPE_MIN_TIMESTAMP && col.Info.Statistics != nil:
			var ts dataset.Value
			if err := ts.UnmarshalBinary(col.Info.Statistics.MinValue); err != nil {
				return stats, fmt.Errorf("unmarshalling min timestamp: %w", err)
			}
			stats.MinTimestamp = time.Unix(0, ts.Int64())

		case col.Type == streamsmd.COLUMN_TYPE_MAX_TIMESTAMP && col.Info.Statistics != nil:
			var ts dataset.Value
			if err := ts.UnmarshalBinary(col.Info.Statistics.MaxValue); err != nil {
				return stats, fmt.Errorf("unmarshalling max timestamp: %w", err)
			}
			stats.MaxTimestamp = time.Unix(0, ts.Int64())
		}

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

	if stats.MinTimestamp.IsZero() || stats.MaxTimestamp.IsZero() {
		// Short sircuit if there's no timestamps.
		return stats, nil
	}

	width := int(stats.MaxTimestamp.Add(1 * time.Hour).Truncate(time.Hour).Sub(stats.MinTimestamp.Truncate(time.Hour)).Hours())
	counts := make([]uint64, width)
	for streamVal := range IterSection(ctx, section) {
		stream, err := streamVal.Value()
		if err != nil {
			return stats, err
		}
		for i := stream.MinTimestamp; !i.After(stream.MaxTimestamp); i = i.Add(time.Hour) {
			hoursBeforeMax := int(stats.MaxTimestamp.Sub(i).Hours())
			counts[hoursBeforeMax]++
		}
	}

	stats.TimestampDistribution = counts

	return stats, nil
}
