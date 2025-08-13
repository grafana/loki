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

	region := sr.sec.GetLayout().GetData()
	if region == nil {
		return nil, fmt.Errorf("section has no data")
	} else if region.Offset > math.MaxInt64 {
		return nil, fmt.Errorf("section data offset is too large")
	} else if region.Length > math.MaxInt64 {
		return nil, fmt.Errorf("section data length is too large")
	}

	// Validate bounds within the range of the section.
	var (
		start = offset
		end   = offset + length - 1
	)
	if start > int64(region.Length) || end > int64(region.Length) {
		return nil, fmt.Errorf("section data is invalid: start=%d end=%d length=%d", start, end, region.Length)
	}

	absoluteOffset := int64(region.Offset) + offset
	return sr.rr.ReadRange(ctx, absoluteOffset, length)
}

func (sr *sectionReader) MetadataRange(ctx context.Context, offset, length int64) (io.ReadCloser, error) {
	if offset < 0 || length < 0 {
		return nil, fmt.Errorf("parameters must not be negative: offset=%d length=%d", offset, length)
	}

	metadataRegion := sr.sec.GetLayout().GetMetadata()
	if metadataRegion == nil {
		return nil, fmt.Errorf("section has no metadata")
	}

	// Validate bounds within the range of the section.
	var (
		start = offset
		end   = offset + length - 1
	)
	if start > int64(metadataRegion.Length) || end > int64(metadataRegion.Length) {
		return nil, fmt.Errorf("section data is invalid: start=%d end=%d length=%d", start, end, metadataRegion.Length)
	}

	absoluteOffset := int64(metadataRegion.Offset) + offset
	return sr.rr.ReadRange(ctx, absoluteOffset, length)
}

func (sr *sectionReader) DataSize() int64 {
	dataRegion := sr.sec.GetLayout().GetData()
	if dataRegion == nil {
		return 0
	}
	return int64(dataRegion.Length)
}

func (sr *sectionReader) MetadataSize() int64 {
	metadataRegion := sr.sec.GetLayout().GetMetadata()
	if metadataRegion == nil {
		return 0
	}
	return int64(metadataRegion.Length)
}
