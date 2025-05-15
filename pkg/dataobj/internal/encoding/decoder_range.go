package encoding

import (
	"context"
	"fmt"
	"io"
	"math"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/bufpool"
)

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

	br := bufpool.GetReader(rc)
	defer bufpool.PutReader(br)

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

	br := bufpool.GetReader(rc)
	defer bufpool.PutReader(br)

	metadataSize, err := decodeTailer(br)
	if err != nil {
		return tailer{}, fmt.Errorf("scanning tailer: %w", err)
	}

	return tailer{
		MetadataSize: uint64(metadataSize),
		FileSize:     uint64(size),
	}, nil
}

func (rd *rangeDecoder) SectionReader(metadata *filemd.Metadata, section *filemd.SectionInfo) SectionReader {
	return &rangeSectionReader{rr: rd.r, md: metadata, sec: section}
}

type rangeSectionReader struct {
	rr  rangeReader // Reader for absolute ranges within the file.
	md  *filemd.Metadata
	sec *filemd.SectionInfo
}

func (rd *rangeSectionReader) Type() (SectionType, error) {
	return GetSectionType(rd.md, rd.sec)
}

func (rd *rangeSectionReader) DataRange(ctx context.Context, offset, length int64) (io.ReadCloser, error) {
	if offset < 0 || length < 0 {
		return nil, fmt.Errorf("parameters must not be negative: offset=%d length=%d", offset, length)
	}

	// In newer versions of data objects, the offset to read is relative to the
	// beginning of the section. In older versions, it's an absolute offset.
	//
	// We default to the older interpretation and adjust to the correct relative
	// offset if a layout is provided.
	var absoluteOffset = offset

	if layout := rd.sec.Layout; layout != nil {
		if layout.Data == nil {
			return nil, fmt.Errorf("section has no data")
		} else if layout.Data.Offset > math.MaxInt64 {
			return nil, fmt.Errorf("section data offset is too large")
		} else if layout.Data.Length > math.MaxInt64 {
			return nil, fmt.Errorf("section data length is too large")
		}

		// Validate bounds within the range of the section.
		var (
			start = int64(offset)
			end   = int64(offset) + int64(length) - 1
		)
		if start > int64(layout.Data.Length) || end > int64(layout.Data.Length) {
			return nil, fmt.Errorf("section data is invalid: start=%d end=%d length=%d", start, end, layout.Data.Length)
		}

		absoluteOffset = int64(layout.Data.Offset) + offset
	}

	return rd.rr.ReadRange(ctx, absoluteOffset, length)
}

func (rd *rangeSectionReader) Metadata(ctx context.Context) (io.ReadCloser, error) {
	metadataRegion, err := findMetadataRegion(rd.sec)
	if err != nil {
		return nil, err
	} else if metadataRegion == nil {
		return nil, fmt.Errorf("section is missing metadata")
	}

	return rd.rr.ReadRange(ctx, int64(metadataRegion.Offset), int64(metadataRegion.Length))
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
