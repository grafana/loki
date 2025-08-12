package dataobj

import (
	"context"
	"fmt"
	"io"
	"math"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
)

type sectionReader struct {
	rr  rangeReader // Reader for absolute ranges within the file.
	md  *filemd.Metadata
	sec *filemd.SectionInfo
}

func (sr *sectionReader) DataRange(ctx context.Context, offset, length int64) (io.ReadCloser, error) {
	if offset < 0 || length < 0 {
		return nil, fmt.Errorf("parameters must not be negative: offset=%d length=%d", offset, length)
	}

	// In newer versions of data objects, the offset to read is relative to the
	// beginning of the section. In older versions, it's an absolute offset.
	//
	// We default to the older interpretation and adjust to the correct relative
	// offset if a layout is provided.
	var absoluteOffset = offset

	if layout := sr.sec.Layout; layout != nil {
		if layout.Data == nil {
			return nil, fmt.Errorf("section has no data")
		} else if layout.Data.Offset > math.MaxInt64 {
			return nil, fmt.Errorf("section data offset is too large")
		} else if layout.Data.Length > math.MaxInt64 {
			return nil, fmt.Errorf("section data length is too large")
		}

		// Validate bounds within the range of the section.
		var (
			start = offset
			end   = offset + length - 1
		)
		if start > int64(layout.Data.Length) || end > int64(layout.Data.Length) {
			return nil, fmt.Errorf("section data is invalid: start=%d end=%d length=%d", start, end, layout.Data.Length)
		}

		absoluteOffset = int64(layout.Data.Offset) + offset
	}

	return sr.rr.ReadRange(ctx, absoluteOffset, length)
}

func (sr *sectionReader) MetadataRange(ctx context.Context, offset, length int64) (io.ReadCloser, error) {
	if offset < 0 || length < 0 {
		return nil, fmt.Errorf("parameters must not be negative: offset=%d length=%d", offset, length)
	}

	metadataRegion, err := findMetadataRegion(sr.sec)
	if err != nil {
		return nil, err
	} else if metadataRegion == nil {
		return nil, fmt.Errorf("section is missing metadata")
	}

	// Validate bounds within the range of the section.
	var (
		start = offset
		end   = offset + length - 1
	)
	if start > int64(metadataRegion.Length) || end > int64(metadataRegion.Length) {
		return nil, fmt.Errorf("section data is invalid: start=%d end=%d length=%d", start, end, metadataRegion.Length)
	}

	// Unlike DataRange, MetadataRange was only introduced in newer versions of
	// data objects. Previously, callers always had to read the entire metadata
	// region at once. Because of this, we can always treat offset as relative
	// to the beginning of the region.
	absoluteOffset := int64(metadataRegion.Offset) + offset
	return sr.rr.ReadRange(ctx, absoluteOffset, length)
}

func (sr *sectionReader) MetadataSize() int64 {
	metadataRegion, err := findMetadataRegion(sr.sec)
	if err != nil {
		return 0
	} else if metadataRegion == nil {
		return 0
	}
	return int64(metadataRegion.Length)
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
