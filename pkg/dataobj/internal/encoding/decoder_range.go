package encoding

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

// windowSize specifies the maximum amount of data to download at once from
// object storage. 16MB is chosen based on S3's [recommendations] for
// Byte-Range fetches, which recommends either 8MB or 16MB.
//
// As windowing is designed to reduce the number of requests made to object
// storage, 16MB is chosen over 8MB, as it will lead to fewer requests.
//
// [recommendations]: https://docs.aws.amazon.com/whitepapers/latest/s3-optimizing-performance-best-practices/use-byte-range-fetches.html
const windowSize = 16_000_000

// rangeReader is an interface that can read a range of bytes from an object.
type rangeReader interface {
	// Size returns the full size of the object.
	Size(ctx context.Context) (int64, error)

	// ReadRange returns a reader over a range of bytes. Callers may create
	// multiple current instance of ReadRange.
	ReadRange(ctx context.Context, offset int64, length int64) (io.ReadCloser, error)
}

type rangeDecoder struct {
	r rangeReader
}

func (rd *rangeDecoder) Metadata(ctx context.Context) (*filemd.Metadata, error) {
	tailer, err := rd.tailer(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading tailer: %w", err)
	}

	rc, err := rd.r.ReadRange(ctx, int64(tailer.FileSize-tailer.MetadataSize-8), int64(tailer.MetadataSize))
	if err != nil {
		return nil, fmt.Errorf("getting metadata: %w", err)
	}
	defer rc.Close()

	br, release := getBufioReader(rc)
	defer release()

	return decodeFileMetadata(br)
}

type tailer struct {
	MetadataSize uint64
	FileSize     uint64
}

func (rd *rangeDecoder) tailer(ctx context.Context) (tailer, error) {
	size, err := rd.r.Size(ctx)
	if err != nil {
		return tailer{}, fmt.Errorf("reading attributes: %w", err)
	}

	// Read the last 8 bytes of the object to get the metadata size and magic.
	rc, err := rd.r.ReadRange(ctx, size-8, 8)
	if err != nil {
		return tailer{}, fmt.Errorf("getting file tailer: %w", err)
	}
	defer rc.Close()

	br, release := getBufioReader(rc)
	defer release()

	metadataSize, err := decodeTailer(br)
	if err != nil {
		return tailer{}, fmt.Errorf("scanning tailer: %w", err)
	}

	return tailer{
		MetadataSize: uint64(metadataSize),
		FileSize:     uint64(size),
	}, nil
}

func (rd *rangeDecoder) StreamsDecoder(metadata *filemd.Metadata, section *filemd.SectionInfo) StreamsDecoder {
	return &rangeStreamsDecoder{rr: rd.r, md: metadata, sec: section}
}

func (rd *rangeDecoder) LogsDecoder(metadata *filemd.Metadata, section *filemd.SectionInfo) LogsDecoder {
	return &rangeLogsDecoder{rr: rd.r, md: metadata, sec: section}
}

type rangeStreamsDecoder struct {
	// TODO(rfratto): restrict sections from reading outside of their regions.

	rr  rangeReader // Reader for absolute ranges within the file.
	md  *filemd.Metadata
	sec *filemd.SectionInfo
}

func (rd *rangeStreamsDecoder) Columns(ctx context.Context) ([]*streamsmd.ColumnDesc, error) {
	typ, err := GetSectionType(rd.md, rd.sec)
	if err != nil {
		return nil, fmt.Errorf("failed to read section type: %w", err)
	} else if got, want := typ, SectionTypeStreams; got != want {
		return nil, fmt.Errorf("unexpected section type: got=%s want=%s", got, want)
	}

	metadataRegion, err := findMetadataRegion(rd.sec)
	if err != nil {
		return nil, err
	} else if metadataRegion == nil {
		return nil, fmt.Errorf("section is missing metadata")
	}

	rc, err := rd.rr.ReadRange(ctx, int64(metadataRegion.Offset), int64(metadataRegion.Length))
	if err != nil {
		return nil, fmt.Errorf("reading streams section metadata: %w", err)
	}
	defer rc.Close()

	br, release := getBufioReader(rc)
	defer release()

	md, err := decodeStreamsMetadata(br)
	if err != nil {
		return nil, err
	}
	return md.Columns, nil
}

// findMetadataRegion returns the region where a section's metadata is stored.
// If section specifies the new [filemd.SectionLayout] field, then the region
// from tha layout is returned. Otherwise, it returns the deprecated
// MetadataOffset and MetadataSize fields.
//
// findMetadataRegion returns an error if both the layout and metadata fields
// are set.
//
// findMetadtaRegion returns nil for sections without metadata.
func findMetadataRegion(section *filemd.SectionInfo) (*filemd.Region, error) {
	// Fallbacks to deprecated fields if the layout is not set.
	var (
		deprecatedOffset = section.MetadataOffset //nolint:staticcheck // Ignore deprecation warning
		deprecatedSize   = section.MetadataSize   //nolint:staticcheck // Ignore deprecation warning
	)

	if section.Layout != nil {
		if deprecatedOffset != 0 || deprecatedSize != 0 {
			return nil, fmt.Errorf("invalid section: both layout and deprecated metadata fields are set")
		}
		return section.Layout.Metadata, nil
	}

	return &filemd.Region{
		Offset: deprecatedOffset,
		Length: deprecatedSize,
	}, nil
}

func (rd *rangeStreamsDecoder) Pages(ctx context.Context, columns []*streamsmd.ColumnDesc) result.Seq[[]*streamsmd.PageDesc] {
	return result.Iter(func(yield func([]*streamsmd.PageDesc) bool) error {
		baseOffset, err := findDataOffset(rd.sec)
		if err != nil {
			return err
		}

		results := make([][]*streamsmd.PageDesc, len(columns))

		columnInfo := func(c *streamsmd.ColumnDesc) (uint64, uint64) {
			return c.GetInfo().MetadataOffset, c.GetInfo().MetadataSize
		}

		for window := range iterWindows(columns, columnInfo, windowSize) {
			if len(window) == 0 {
				continue
			}

			var (
				windowOffset = window.Start().GetInfo().MetadataOffset
				windowSize   = (window.End().GetInfo().MetadataOffset + window.End().GetInfo().MetadataSize) - windowOffset
			)

			rc, err := rd.rr.ReadRange(ctx, int64(baseOffset+windowOffset), int64(windowSize))
			if err != nil {
				return fmt.Errorf("reading column data: %w", err)
			}
			data, err := readAndClose(rc, windowSize)
			if err != nil {
				return fmt.Errorf("read column data: %w", err)
			}

			for _, wp := range window {
				// Find the slice in the data for this column.
				var (
					columnOffset = wp.Data.GetInfo().MetadataOffset
					dataOffset   = columnOffset - windowOffset
				)

				r := bytes.NewReader(data[dataOffset : dataOffset+wp.Data.GetInfo().MetadataSize])

				md, err := decodeStreamsColumnMetadata(r)
				if err != nil {
					return err
				}

				// wp.Position is the position of the column in the original pages
				// slice; this retains the proper order of data in results.
				results[wp.Position] = md.Pages
			}
		}

		for _, data := range results {
			if !yield(data) {
				return nil
			}
		}

		return nil
	})
}

// findDataOffset returns the base byte offset from where all reads of a
// section start.
//
// Older versions of data objects use absolute offsets for page data. Newer
// versions (where [filemd.SectionLayout] is provided) use offsets relative to
// the start of a section's data region.
//
// If a section specifies a layout but has no data region, then the section has
// no data for reading, and findDataOffset returns an error.
func findDataOffset(section *filemd.SectionInfo) (uint64, error) {
	if section.Layout != nil {
		if section.Layout.Data == nil {
			return 0, fmt.Errorf("section has no data")
		}
		return section.Layout.Data.Offset, nil
	}
	return 0, nil
}

// readAndClose reads exactly size bytes from rc and then closes it.
func readAndClose(rc io.ReadCloser, size uint64) ([]byte, error) {
	defer rc.Close()

	data := make([]byte, size)
	if _, err := io.ReadFull(rc, data); err != nil {
		return nil, fmt.Errorf("read column data: %w", err)
	}
	return data, nil
}

func (rd *rangeStreamsDecoder) ReadPages(ctx context.Context, pages []*streamsmd.PageDesc) result.Seq[dataset.PageData] {
	return result.Iter(func(yield func(dataset.PageData) bool) error {
		baseOffset, err := findDataOffset(rd.sec)
		if err != nil {
			return err
		}

		results := make([]dataset.PageData, len(pages))

		pageInfo := func(p *streamsmd.PageDesc) (uint64, uint64) {
			return p.GetInfo().DataOffset, p.GetInfo().DataSize
		}

		// TODO(rfratto): If there are many windows, it may make sense to read them
		// in parallel.
		for window := range iterWindows(pages, pageInfo, windowSize) {
			if len(window) == 0 {
				continue
			}

			var (
				windowOffset = window.Start().GetInfo().DataOffset
				windowSize   = (window.End().GetInfo().DataOffset + window.End().GetInfo().DataSize) - windowOffset
			)

			rc, err := rd.rr.ReadRange(ctx, int64(baseOffset+windowOffset), int64(windowSize))
			if err != nil {
				return fmt.Errorf("reading page data: %w", err)
			}
			data, err := readAndClose(rc, windowSize)
			if err != nil {
				return fmt.Errorf("read page data: %w", err)
			}

			for _, wp := range window {
				// Find the slice in the data for this page.
				var (
					pageOffset = wp.Data.GetInfo().DataOffset
					dataOffset = pageOffset - windowOffset
				)

				// wp.Position is the position of the page in the original pages slice;
				// this retains the proper order of data in results.
				results[wp.Position] = dataset.PageData(data[dataOffset : dataOffset+wp.Data.GetInfo().DataSize])
			}
		}

		for _, data := range results {
			if !yield(data) {
				return nil
			}
		}

		return nil
	})
}

type rangeLogsDecoder struct {
	// TODO(rfratto): restrict sections from reading outside of their regions.

	rr  rangeReader // Reader for absolute ranges within the file.
	md  *filemd.Metadata
	sec *filemd.SectionInfo
}

func (rd *rangeLogsDecoder) Columns(ctx context.Context) ([]*logsmd.ColumnDesc, error) {
	typ, err := GetSectionType(rd.md, rd.sec)
	if err != nil {
		return nil, fmt.Errorf("failed to read section type: %w", err)
	} else if got, want := typ, SectionTypeLogs; got != want {
		return nil, fmt.Errorf("unexpected section type: got=%s want=%s", got, want)
	}

	metadataRegion, err := findMetadataRegion(rd.sec)
	if err != nil {
		return nil, err
	} else if metadataRegion == nil {
		return nil, fmt.Errorf("section is missing metadata")
	}

	rc, err := rd.rr.ReadRange(ctx, int64(metadataRegion.Offset), int64(metadataRegion.Length))
	if err != nil {
		return nil, fmt.Errorf("reading logs section metadata: %w", err)
	}
	defer rc.Close()

	br, release := getBufioReader(rc)
	defer release()

	md, err := decodeLogsMetadata(br)
	if err != nil {
		return nil, err
	}
	return md.Columns, nil
}

func (rd *rangeLogsDecoder) Pages(ctx context.Context, columns []*logsmd.ColumnDesc) result.Seq[[]*logsmd.PageDesc] {
	return result.Iter(func(yield func([]*logsmd.PageDesc) bool) error {
		baseOffset, err := findDataOffset(rd.sec)
		if err != nil {
			return err
		}

		results := make([][]*logsmd.PageDesc, len(columns))

		columnInfo := func(c *logsmd.ColumnDesc) (uint64, uint64) {
			return c.GetInfo().MetadataOffset, c.GetInfo().MetadataSize
		}

		for window := range iterWindows(columns, columnInfo, windowSize) {
			if len(window) == 0 {
				continue
			}

			var (
				windowOffset = window.Start().GetInfo().MetadataOffset
				windowSize   = (window.End().GetInfo().MetadataOffset + window.End().GetInfo().MetadataSize) - windowOffset
			)

			rc, err := rd.rr.ReadRange(ctx, int64(baseOffset+windowOffset), int64(windowSize))
			if err != nil {
				return fmt.Errorf("reading column data: %w", err)
			}
			data, err := readAndClose(rc, windowSize)
			if err != nil {
				return fmt.Errorf("read page data: %w", err)
			}

			for _, wp := range window {
				// Find the slice in the data for this column.
				var (
					columnOffset = wp.Data.GetInfo().MetadataOffset
					dataOffset   = columnOffset - windowOffset
				)

				r := bytes.NewReader(data[dataOffset : dataOffset+wp.Data.GetInfo().MetadataSize])

				md, err := decodeLogsColumnMetadata(r)
				if err != nil {
					return err
				}

				// wp.Position is the position of the column in the original pages
				// slice; this retains the proper order of data in results.
				results[wp.Position] = md.Pages
			}
		}

		for _, data := range results {
			if !yield(data) {
				return nil
			}
		}

		return nil
	})
}

func (rd *rangeLogsDecoder) ReadPages(ctx context.Context, pages []*logsmd.PageDesc) result.Seq[dataset.PageData] {
	return result.Iter(func(yield func(dataset.PageData) bool) error {
		baseOffset, err := findDataOffset(rd.sec)
		if err != nil {
			return err
		}

		results := make([]dataset.PageData, len(pages))

		pageInfo := func(p *logsmd.PageDesc) (uint64, uint64) {
			return p.GetInfo().DataOffset, p.GetInfo().DataSize
		}

		// TODO(rfratto): If there are many windows, it may make sense to read them
		// in parallel.
		for window := range iterWindows(pages, pageInfo, windowSize) {
			if len(window) == 0 {
				continue
			}

			var (
				windowOffset = window.Start().GetInfo().DataOffset
				windowSize   = (window.End().GetInfo().DataOffset + window.End().GetInfo().DataSize) - windowOffset
			)

			rc, err := rd.rr.ReadRange(ctx, int64(baseOffset+windowOffset), int64(windowSize))
			if err != nil {
				return fmt.Errorf("reading page data: %w", err)
			}
			data, err := readAndClose(rc, windowSize)
			if err != nil {
				return fmt.Errorf("read page data: %w", err)
			}

			for _, wp := range window {
				// Find the slice in the data for this page.
				var (
					pageOffset = wp.Data.GetInfo().DataOffset
					dataOffset = pageOffset - windowOffset
				)

				// wp.Position is the position of the page in the original pages slice;
				// this retains the proper order of data in results.
				results[wp.Position] = dataset.PageData(data[dataOffset : dataOffset+wp.Data.GetInfo().DataSize])
			}
		}

		for _, data := range results {
			if !yield(data) {
				return nil
			}
		}

		return nil
	})
}
