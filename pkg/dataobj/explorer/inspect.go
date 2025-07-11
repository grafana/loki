package explorer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

type FileMetadata struct {
	Sections     []SectionMetadata `json:"sections"`
	Error        string            `json:"error,omitempty"`
	LastModified time.Time         `json:"lastModified,omitempty"`
}

type ColumnWithPages struct {
	Name             string     `json:"name,omitempty"`
	Type             string     `json:"type"`
	ValueType        string     `json:"value_type"`
	RowsCount        uint64     `json:"rows_count"`
	Compression      string     `json:"compression"`
	UncompressedSize uint64     `json:"uncompressed_size"`
	CompressedSize   uint64     `json:"compressed_size"`
	MetadataOffset   uint64     `json:"metadata_offset"`
	MetadataSize     uint64     `json:"metadata_size"`
	ValuesCount      uint64     `json:"values_count"`
	Pages            []PageInfo `json:"pages"`
	Statistics       Statistics `json:"statistics"`
}

type Statistics struct {
	CardinalityCount uint64 `json:"cardinality_count"`
}

func NewStatsFrom(md *datasetmd.Statistics) (res Statistics) {
	if md != nil {
		res.CardinalityCount = md.CardinalityCount
	}
	return
}

type PageInfo struct {
	UncompressedSize uint64 `json:"uncompressed_size"`
	CompressedSize   uint64 `json:"compressed_size"`
	CRC32            uint32 `json:"crc32"`
	RowsCount        uint64 `json:"rows_count"`
	Encoding         string `json:"encoding"`
	DataOffset       uint64 `json:"data_offset"`
	DataSize         uint64 `json:"data_size"`
	ValuesCount      uint64 `json:"values_count"`
}

type SectionMetadata struct {
	Type                  string            `json:"type"`
	TotalCompressedSize   uint64            `json:"totalCompressedSize"`
	TotalUncompressedSize uint64            `json:"totalUncompressedSize"`
	ColumnCount           int               `json:"columnCount"`
	Columns               []ColumnWithPages `json:"columns"`
	Distribution          []uint64          `json:"distribution"`
	MinTimestamp          time.Time         `json:"minTimestamp"`
	MaxTimestamp          time.Time         `json:"maxTimestamp"`
}

func (s *Service) handleInspect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	filename := r.URL.Query().Get("file")
	if filename == "" {
		http.Error(w, "file parameter is required", http.StatusBadRequest)
		return
	}

	attrs, err := s.bucket.Attributes(r.Context(), filename)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get file attributes: %v", err), http.StatusInternalServerError)
		return
	}

	metadata := inspectFile(r.Context(), s.bucket, filename)
	metadata.LastModified = attrs.LastModified.UTC()
	for _, section := range metadata.Sections {
		section.MinTimestamp = section.MinTimestamp.UTC().Truncate(time.Second)
		section.MaxTimestamp = section.MaxTimestamp.UTC().Truncate(time.Second)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(metadata); err != nil {
		http.Error(w, fmt.Sprintf("failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

func inspectFile(ctx context.Context, bucket objstore.BucketReader, path string) FileMetadata {
	obj, err := dataobj.FromBucket(ctx, bucket, path)
	if err != nil {
		return FileMetadata{
			Error: fmt.Sprintf("failed to read sections: %v", err),
		}
	}

	result := FileMetadata{
		Sections: make([]SectionMetadata, 0, len(obj.Sections())),
	}

	for _, section := range obj.Sections() {
		switch {
		case streams.CheckSection(section):
			streamsSection, err := streams.Open(ctx, section)
			if err != nil {
				return FileMetadata{
					Error: fmt.Sprintf("failed to open streams section: %v", err),
				}
			}
			meta, err := inspectStreamsSection(ctx, section.Type, streamsSection)
			if err != nil {
				return FileMetadata{
					Error: fmt.Sprintf("failed to inspect streams section: %v", err),
				}
			}
			result.Sections = append(result.Sections, meta)

		case logs.CheckSection(section):
			logsSection, err := logs.Open(ctx, section)
			if err != nil {
				return FileMetadata{
					Error: fmt.Sprintf("failed to open logs section: %v", err),
				}
			}
			meta, err := inspectLogsSection(ctx, section.Type, logsSection)
			if err != nil {
				return FileMetadata{
					Error: fmt.Sprintf("failed to inspect logs section: %v", err),
				}
			}
			result.Sections = append(result.Sections, meta)
		case pointers.CheckSection(section):
			pointersSection, err := pointers.Open(ctx, section)
			if err != nil {
				return FileMetadata{
					Error: fmt.Sprintf("failed to open pointers section: %v", err),
				}
			}
			meta, err := inspectPointersSection(ctx, section.Type, pointersSection)
			if err != nil {
				return FileMetadata{
					Error: fmt.Sprintf("failed to inspect pointers section: %v", err),
				}
			}
			result.Sections = append(result.Sections, meta)
		}
	}

	return result
}

func inspectPointersSection(ctx context.Context, ty dataobj.SectionType, sec *pointers.Section) (SectionMetadata, error) {
	stats, err := pointers.ReadStats(ctx, sec)
	if err != nil {
		return SectionMetadata{}, err
	}

	meta := SectionMetadata{
		Type:                  ty.String(),
		TotalCompressedSize:   stats.CompressedSize,
		TotalUncompressedSize: stats.UncompressedSize,
		ColumnCount:           len(stats.Columns),
	}

	for _, col := range stats.Columns {
		colMeta := ColumnWithPages{
			Name:             col.Name,
			Type:             col.Type,
			ValueType:        strings.TrimPrefix(col.ValueType, "VALUE_TYPE_"),
			RowsCount:        col.RowsCount,
			Compression:      strings.TrimPrefix(col.Compression, "COMPRESSION_TYPE_"),
			UncompressedSize: col.UncompressedSize,
			CompressedSize:   col.CompressedSize,
			MetadataOffset:   col.MetadataOffset,
			MetadataSize:     col.MetadataSize,
			ValuesCount:      col.ValuesCount,
			Statistics:       Statistics{CardinalityCount: col.Cardinality},
		}

		for _, page := range col.Pages {
			colMeta.Pages = append(colMeta.Pages, PageInfo{
				UncompressedSize: page.UncompressedSize,
				CompressedSize:   page.CompressedSize,
				CRC32:            page.CRC32,
				RowsCount:        page.RowsCount,
				Encoding:         strings.TrimPrefix(page.Encoding, "ENCODING_TYPE_"),
				DataOffset:       page.DataOffset,
				DataSize:         page.DataSize,
				ValuesCount:      page.ValuesCount,
			})
		}

		meta.Columns = append(meta.Columns, colMeta)
	}

	return meta, nil
}

func inspectLogsSection(ctx context.Context, ty dataobj.SectionType, sec *logs.Section) (SectionMetadata, error) {
	stats, err := logs.ReadStats(ctx, sec)
	if err != nil {
		return SectionMetadata{}, err
	}

	meta := SectionMetadata{
		Type:                  ty.String(),
		TotalCompressedSize:   stats.CompressedSize,
		TotalUncompressedSize: stats.UncompressedSize,
		ColumnCount:           len(stats.Columns),
	}

	for _, col := range stats.Columns {
		colMeta := ColumnWithPages{
			Name:             col.Name,
			Type:             col.Type,
			ValueType:        strings.TrimPrefix(col.ValueType, "VALUE_TYPE_"),
			RowsCount:        col.RowsCount,
			Compression:      strings.TrimPrefix(col.Compression, "COMPRESSION_TYPE_"),
			UncompressedSize: col.UncompressedSize,
			CompressedSize:   col.CompressedSize,
			MetadataOffset:   col.MetadataOffset,
			MetadataSize:     col.MetadataSize,
			ValuesCount:      col.ValuesCount,
			Statistics:       Statistics{CardinalityCount: col.Cardinality},
		}

		for _, page := range col.Pages {
			colMeta.Pages = append(colMeta.Pages, PageInfo{
				UncompressedSize: page.UncompressedSize,
				CompressedSize:   page.CompressedSize,
				CRC32:            page.CRC32,
				RowsCount:        page.RowsCount,
				Encoding:         strings.TrimPrefix(page.Encoding, "ENCODING_TYPE_"),
				DataOffset:       page.DataOffset,
				DataSize:         page.DataSize,
				ValuesCount:      page.ValuesCount,
			})
		}

		meta.Columns = append(meta.Columns, colMeta)
	}

	return meta, nil
}

func inspectStreamsSection(ctx context.Context, ty dataobj.SectionType, sec *streams.Section) (SectionMetadata, error) {
	stats, err := streams.ReadStats(ctx, sec)
	if err != nil {
		return SectionMetadata{}, err
	}

	meta := SectionMetadata{
		Type:                  ty.String(),
		TotalCompressedSize:   stats.CompressedSize,
		TotalUncompressedSize: stats.UncompressedSize,
		ColumnCount:           len(stats.Columns),
		Distribution:          stats.TimestampDistribution,
		MinTimestamp:          stats.MinTimestamp,
		MaxTimestamp:          stats.MaxTimestamp,
	}

	for _, col := range stats.Columns {
		colMeta := ColumnWithPages{
			Name:             col.Name,
			Type:             col.Type,
			ValueType:        strings.TrimPrefix(col.ValueType, "VALUE_TYPE_"),
			RowsCount:        col.RowsCount,
			Compression:      strings.TrimPrefix(col.Compression, "COMPRESSION_TYPE_"),
			UncompressedSize: col.UncompressedSize,
			CompressedSize:   col.CompressedSize,
			MetadataOffset:   col.MetadataOffset,
			MetadataSize:     col.MetadataSize,
			ValuesCount:      col.ValuesCount,
			Statistics:       Statistics{CardinalityCount: col.Cardinality},
		}

		for _, page := range col.Pages {
			colMeta.Pages = append(colMeta.Pages, PageInfo{
				UncompressedSize: page.UncompressedSize,
				CompressedSize:   page.CompressedSize,
				CRC32:            page.CRC32,
				RowsCount:        page.RowsCount,
				Encoding:         strings.TrimPrefix(page.Encoding, "ENCODING_TYPE_"),
				DataOffset:       page.DataOffset,
				DataSize:         page.DataSize,
				ValuesCount:      page.ValuesCount,
			})
		}

		meta.Columns = append(meta.Columns, colMeta)
	}

	return meta, nil
}
