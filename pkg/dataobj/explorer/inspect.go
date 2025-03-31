package explorer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/sections/streams"
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

// Helper functions moved from service.go
func getEncodingName(encoding datasetmd.EncodingType) string {
	encodingName, ok := datasetmd.EncodingType_name[int32(encoding)]
	if !ok {
		return fmt.Sprintf("UNKNOWN(%d)", encoding)
	}
	return strings.TrimPrefix(encodingName, "ENCODING_TYPE_")
}

func getValueTypeName(valueType datasetmd.ValueType) string {
	valueTypeName, ok := datasetmd.ValueType_name[int32(valueType)]
	if !ok {
		return fmt.Sprintf("UNKNOWN(%d)", valueType)
	}
	return strings.TrimPrefix(valueTypeName, "VALUE_TYPE_")
}

func getCompressionName(compression datasetmd.CompressionType) string {
	compressionName, ok := datasetmd.CompressionType_name[int32(compression)]
	if !ok {
		return fmt.Sprintf("UNKNOWN(%d)", compression)
	}
	return strings.TrimPrefix(compressionName, "COMPRESSION_TYPE_")
}

func inspectFile(ctx context.Context, bucket objstore.BucketReader, path string) FileMetadata {
	reader := encoding.BucketDecoder(bucket, path)

	sections, err := reader.Sections(ctx)
	if err != nil {
		return FileMetadata{
			Error: fmt.Sprintf("failed to read sections: %v", err),
		}
	}

	result := FileMetadata{
		Sections: make([]SectionMetadata, 0, len(sections)),
	}

	for _, section := range sections {
		sectionMeta := SectionMetadata{
			Type: section.Type.String(),
		}

		switch section.Type {
		case filemd.SECTION_TYPE_LOGS:
			sectionMeta, err = inspectLogsSection(ctx, reader, section)
			if err != nil {
				return FileMetadata{
					Sections: make([]SectionMetadata, 0, len(sections)),
					Error:    fmt.Sprintf("failed to inspect logs section: %v", err),
				}
			}
		case filemd.SECTION_TYPE_STREAMS:
			sectionMeta, err = inspectStreamsSection(ctx, reader, section)
			if err != nil {
				return FileMetadata{
					Sections: make([]SectionMetadata, 0, len(sections)),
					Error:    fmt.Sprintf("failed to inspect streams section: %v", err),
				}
			}
		}

		result.Sections = append(result.Sections, sectionMeta)
	}

	return result
}

func inspectLogsSection(ctx context.Context, reader encoding.Decoder, section *filemd.SectionInfo) (SectionMetadata, error) {
	meta := SectionMetadata{
		Type: section.Type.String(),
	}

	dec := reader.LogsDecoder()
	cols, err := dec.Columns(ctx, section)
	if err != nil {
		return meta, err
	}

	meta.Columns = make([]ColumnWithPages, len(cols)) // Pre-allocate with final size
	meta.ColumnCount = len(cols)

	// Create error group for parallel execution
	g, ctx := errgroup.WithContext(ctx)

	// Process each column in parallel
	for i, col := range cols {
		meta.TotalCompressedSize += col.Info.CompressedSize
		meta.TotalUncompressedSize += col.Info.UncompressedSize

		g.Go(func() error {
			// Get pages for the column
			pageSets, err := result.Collect(dec.Pages(ctx, []*logsmd.ColumnDesc{col}))
			if err != nil {
				return err
			}

			var pageInfos []PageInfo
			for _, pages := range pageSets {
				for _, page := range pages {
					if page.Info != nil {
						pageInfos = append(pageInfos, PageInfo{
							UncompressedSize: page.Info.UncompressedSize,
							CompressedSize:   page.Info.CompressedSize,
							CRC32:            page.Info.Crc32,
							RowsCount:        page.Info.RowsCount,
							Encoding:         getEncodingName(page.Info.Encoding),
							DataOffset:       page.Info.DataOffset,
							DataSize:         page.Info.DataSize,
							ValuesCount:      page.Info.ValuesCount,
						})
					}
				}
			}

			// Safely assign to pre-allocated slice
			meta.Columns[i] = ColumnWithPages{
				Name:             col.Info.Name,
				Type:             col.Type.String(),
				ValueType:        getValueTypeName(col.Info.ValueType),
				RowsCount:        col.Info.RowsCount,
				Compression:      getCompressionName(col.Info.Compression),
				UncompressedSize: col.Info.UncompressedSize,
				CompressedSize:   col.Info.CompressedSize,
				MetadataOffset:   col.Info.MetadataOffset,
				MetadataSize:     col.Info.MetadataSize,
				ValuesCount:      col.Info.ValuesCount,
				Pages:            pageInfos,
				Statistics:       NewStatsFrom(col.Info.Statistics),
			}
			return nil
		})
	}

	// Wait for all goroutines to complete
	if err := g.Wait(); err != nil {
		return meta, err
	}

	return meta, nil
}

func inspectStreamsSection(ctx context.Context, reader encoding.Decoder, section *filemd.SectionInfo) (SectionMetadata, error) {
	meta := SectionMetadata{
		Type: section.Type.String(),
	}

	dec := reader.StreamsDecoder()
	cols, err := dec.Columns(ctx, section)
	if err != nil {
		return meta, err
	}

	meta.Columns = make([]ColumnWithPages, len(cols)) // Pre-allocate with final size
	meta.ColumnCount = len(cols)

	// Create error group for parallel execution
	g, _ := errgroup.WithContext(ctx)

	globalMaxTimestamp := time.Time{}
	globalMinTimestamp := time.Time{}
	// Process each column in parallel
	for i, col := range cols {
		meta.TotalCompressedSize += col.Info.CompressedSize
		meta.TotalUncompressedSize += col.Info.UncompressedSize

		g.Go(func() error {
			// Get pages for the column
			pageSets, err := result.Collect(dec.Pages(ctx, []*streamsmd.ColumnDesc{col}))
			if err != nil {
				return err
			}

			if col.Type == streamsmd.COLUMN_TYPE_MAX_TIMESTAMP && col.Info.Statistics != nil {
				var ts dataset.Value
				_ = ts.UnmarshalBinary(col.Info.Statistics.MaxValue)
				globalMaxTimestamp = time.Unix(0, ts.Int64()).UTC()
			}

			if col.Type == streamsmd.COLUMN_TYPE_MIN_TIMESTAMP && col.Info.Statistics != nil {
				var ts dataset.Value
				_ = ts.UnmarshalBinary(col.Info.Statistics.MinValue)
				globalMinTimestamp = time.Unix(0, ts.Int64()).UTC()
			}

			var pageInfos []PageInfo
			for _, pages := range pageSets {
				for _, page := range pages {
					if page.Info != nil {
						pageInfos = append(pageInfos, PageInfo{
							UncompressedSize: page.Info.UncompressedSize,
							CompressedSize:   page.Info.CompressedSize,
							CRC32:            page.Info.Crc32,
							RowsCount:        page.Info.RowsCount,
							Encoding:         getEncodingName(page.Info.Encoding),
							DataOffset:       page.Info.DataOffset,
							DataSize:         page.Info.DataSize,
							ValuesCount:      page.Info.ValuesCount,
						})
					}
				}
			}

			// Safely assign to pre-allocated slice
			meta.Columns[i] = ColumnWithPages{
				Name:             col.Info.Name,
				Type:             col.Type.String(),
				ValueType:        getValueTypeName(col.Info.ValueType),
				RowsCount:        col.Info.RowsCount,
				Compression:      getCompressionName(col.Info.Compression),
				UncompressedSize: col.Info.UncompressedSize,
				CompressedSize:   col.Info.CompressedSize,
				MetadataOffset:   col.Info.MetadataOffset,
				MetadataSize:     col.Info.MetadataSize,
				ValuesCount:      col.Info.ValuesCount,
				Pages:            pageInfos,
			}
			return nil
		})
	}

	// Wait for all goroutines to complete
	if err := g.Wait(); err != nil {
		return meta, err
	}

	if globalMaxTimestamp.IsZero() || globalMinTimestamp.IsZero() {
		// Short circuit if we don't have any timestamps
		return meta, nil
	}

	width := int(globalMaxTimestamp.Add(1 * time.Hour).Truncate(time.Hour).Sub(globalMinTimestamp.Truncate(time.Hour)).Hours())
	counts := make([]uint64, width)
	for streamVal := range streams.Iter(ctx, reader) {
		stream, err := streamVal.Value()
		if err != nil {
			return meta, err
		}
		for i := stream.MinTimestamp; !i.After(stream.MaxTimestamp); i = i.Add(time.Hour) {
			hoursBeforeMax := int(globalMaxTimestamp.Sub(i).Hours())
			counts[hoursBeforeMax]++
		}
	}

	meta.MinTimestamp = globalMinTimestamp
	meta.MaxTimestamp = globalMaxTimestamp
	meta.Distribution = counts

	return meta, nil
}
