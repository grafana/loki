package explorer

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/thanos-io/objstore"
)

//go:embed dist
var uiFS embed.FS

type Service struct {
	*services.BasicService

	bucket objstore.Bucket
	logger log.Logger
	uiFS   fs.FS
}

type listResponse struct {
	Files   []fileInfo `json:"files"`
	Folders []string   `json:"folders"`
	Parent  string     `json:"parent"`
	Current string     `json:"current"`
}

type fileInfo struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type FileMetadata struct {
	Sections []SectionMetadata `json:"sections"`
	Error    string            `json:"error,omitempty"`
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
}

// Helper functions to convert enum values to strings
func getEncodingName(encoding datasetmd.EncodingType) string {
	switch encoding {
	case datasetmd.ENCODING_TYPE_PLAIN:
		return "PLAIN"
	case datasetmd.ENCODING_TYPE_DELTA:
		return "DELTA"
	case datasetmd.ENCODING_TYPE_BITMAP:
		return "BITMAP"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", encoding)
	}
}

func getValueTypeName(valueType datasetmd.ValueType) string {
	switch valueType {
	case datasetmd.VALUE_TYPE_INT64:
		return "INT64"
	case datasetmd.VALUE_TYPE_UINT64:
		return "UINT64"
	case datasetmd.VALUE_TYPE_STRING:
		return "STRING"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", valueType)
	}
}

func getCompressionName(compression datasetmd.CompressionType) string {
	switch compression {
	case datasetmd.COMPRESSION_TYPE_NONE:
		return "NONE"
	case datasetmd.COMPRESSION_TYPE_SNAPPY:
		return "SNAPPY"
	case datasetmd.COMPRESSION_TYPE_ZSTD:
		return "ZSTD"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", compression)
	}
}

func New(bucket objstore.Bucket, logger log.Logger) (*Service, error) {
	// Get the embedded UI filesystem
	ui, err := fs.Sub(uiFS, "dist")
	if err != nil {
		return nil, err
	}

	s := &Service{
		bucket: bucket,
		logger: logger,
		uiFS:   ui,
	}

	s.BasicService = services.NewBasicService(nil, s.running, nil)
	return s, nil
}

func (s *Service) running(ctx context.Context) error {
	level.Info(s.logger).Log("msg", "dataobj explorer is running")
	<-ctx.Done()
	return nil
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/dataobj/explorer/api/list", s.handleList)
	mux.HandleFunc("/dataobj/explorer/api/inspect", s.handleInspect)
	mux.HandleFunc("/dataobj/explorer/api/download", s.handleDownload)

	// Serve static files from embedded filesystem
	fsHandler := http.FileServer(http.FS(s.uiFS))
	mux.Handle("/dataobj/explorer/", http.StripPrefix("/dataobj/explorer/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If the path doesn't exist, serve index.html
		if _, err := s.uiFS.Open(strings.TrimPrefix(r.URL.Path, "/")); err != nil {
			r.URL.Path = "/"
		}
		fsHandler.ServeHTTP(w, r)
	})))

	return mux
}

func (s *Service) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get path from query parameter and ensure it's not null
	dir := r.URL.Query().Get("path")
	if dir == "" {
		dir = "/"
	}
	level.Debug(s.logger).Log("msg", "listing directory", "path", dir)

	// Initialize empty slices instead of null
	files := make([]fileInfo, 0)
	folders := make([]string, 0)

	// List objects in the bucket
	err := s.bucket.Iter(r.Context(), dir, func(name string) error {
		// Skip the current directory
		if name == dir {
			return nil
		}

		// Get the path relative to the current directory
		relativePath := strings.TrimPrefix(name, dir)
		relativePath = strings.TrimPrefix(relativePath, "/")

		if strings.HasSuffix(name, "/") {
			// This is a folder
			folderName := strings.TrimSuffix(relativePath, "/")
			if folderName != "" { // Only add non-empty folder names
				folders = append(folders, folderName)
			}
		} else {
			// This is a file
			attrs, err := s.bucket.Attributes(r.Context(), name)
			if err != nil {
				level.Error(s.logger).Log("msg", "failed to get object attributes", "name", name, "err", err)
				return nil
			}

			files = append(files, fileInfo{
				Name: relativePath,
				Size: attrs.Size,
			})
		}
		return nil
	})
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to list objects", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Calculate parent directory
	parent := path.Dir(dir)
	if parent == "." || parent == "/" {
		parent = ""
	}

	// Ensure current path is never empty
	current := dir
	if current == "/" {
		current = ""
	}

	resp := listResponse{
		Files:   files,
		Folders: folders,
		Parent:  parent,
		Current: current,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		level.Error(s.logger).Log("msg", "failed to encode response", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
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

	metadata := inspectFile(r.Context(), s.bucket, filename)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(metadata); err != nil {
		http.Error(w, fmt.Sprintf("failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
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
					Error: fmt.Sprintf("failed to inspect logs section: %v", err),
				}
			}
		case filemd.SECTION_TYPE_STREAMS:
			sectionMeta, err = inspectStreamsSection(ctx, reader, section)
			if err != nil {
				return FileMetadata{
					Error: fmt.Sprintf("failed to inspect streams section: %v", err),
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

	meta.Columns = make([]ColumnWithPages, 0, len(cols))
	for _, col := range cols {
		meta.TotalCompressedSize += uint64(col.Info.CompressedSize)
		meta.TotalUncompressedSize += uint64(col.Info.UncompressedSize)

		// Get pages for the column
		pageSets, err := result.Collect(dec.Pages(ctx, []*logsmd.ColumnDesc{col}))
		if err != nil {
			return meta, err
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
		meta.Columns = append(meta.Columns, ColumnWithPages{
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
		})
	}
	meta.ColumnCount = len(cols)

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

	meta.Columns = make([]ColumnWithPages, 0, len(cols))
	for _, col := range cols {
		meta.TotalCompressedSize += uint64(col.Info.CompressedSize)
		meta.TotalUncompressedSize += uint64(col.Info.UncompressedSize)

		// Get pages for the column
		pageSets, err := result.Collect(dec.Pages(ctx, []*streamsmd.ColumnDesc{col}))
		if err != nil {
			return meta, err
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
		meta.Columns = append(meta.Columns, ColumnWithPages{
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
		})
	}
	meta.ColumnCount = len(cols)

	return meta, nil
}

func (s *Service) handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	filename := r.URL.Query().Get("file")
	if filename == "" {
		http.Error(w, "file parameter is required", http.StatusBadRequest)
		return
	}

	// Get file attributes to check size and existence
	attrs, err := s.bucket.Attributes(r.Context(), filename)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to get file attributes", "file", filename, "err", err)
		http.Error(w, fmt.Sprintf("failed to get file: %v", err), http.StatusInternalServerError)
		return
	}

	// Get reader for the file
	reader, err := s.bucket.Get(r.Context(), filename)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to get file", "file", filename, "err", err)
		http.Error(w, fmt.Sprintf("failed to get file: %v", err), http.StatusInternalServerError)
		return
	}
	defer reader.Close()

	// Set response headers
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, path.Base(filename)))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", attrs.Size))

	// Stream the file to the response
	if _, err := io.Copy(w, reader); err != nil {
		level.Error(s.logger).Log("msg", "failed to stream file", "file", filename, "err", err)
		// Can't write error to response here as headers are already sent
		return
	}
}
