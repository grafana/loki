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

	// startOff holds the offset to shift all other offsets by.
	startOff int64

	extensionData []byte
}

func (sr *sectionReader) ExtensionData() []byte { return sr.extensionData }

func (sr *sectionReader) DataRange(ctx context.Context, r RelativeByteRange) (io.ReadCloser, error) {
	if r.Offset < 0 || r.Length < 0 {
		return nil, fmt.Errorf("parameters must not be negative: offset=%d length=%d", r.Offset, r.Length)
	}

	region := sr.sec.GetLayout().GetData()
	if region == nil {
		return nil, fmt.Errorf("section has no data")
	} else if region.Offset > math.MaxInt64 {
		return nil, fmt.Errorf("section data offset is too large")
	} else if region.Length > math.MaxInt64 {
		return nil, fmt.Errorf("section data length is too large")
	}

	// Validate bounds within the range of the section. Subtract instead of add, so a large offset or
	// length cannot overflow. An offset after the region makes the difference negative, and the
	// length check rejects it.
	if r.Length > int64(region.Length)-r.Offset {
		return nil, fmt.Errorf("section data range is invalid: offset=%d length=%d region length=%d", r.Offset, r.Length, region.Length)
	}

	absoluteOffset := sr.startOff + int64(region.Offset) + r.Offset
	return sr.rr.ReadRange(ctx, absoluteOffset, r.Length)
}

// MetadataRanges returns the bytes of each range of the metadata region of the section. The offset
// of each range counts from the start of that region, not from the start of the object.
//
// It checks every range before it reads any.
func (sr *sectionReader) MetadataRanges(ctx context.Context, ranges []RelativeByteRange) ([][]byte, error) {
	metadataRegion := sr.sec.GetLayout().GetMetadata()
	if metadataRegion == nil {
		return nil, fmt.Errorf("section has no metadata")
	} else if metadataRegion.Offset > math.MaxInt64 || metadataRegion.Length > math.MaxInt64 {
		return nil, fmt.Errorf("section metadata region is too large")
	}

	regionOffset := sr.startOff + int64(metadataRegion.Offset)
	regionLength := int64(metadataRegion.Length)
	if regionOffset < sr.startOff || regionOffset+regionLength < regionOffset {
		return nil, fmt.Errorf("section metadata region overflows: offset=%d length=%d", regionOffset, regionLength)
	}

	absolute := make([]absoluteByteRange, len(ranges))
	for i, r := range ranges {
		// Subtract instead of add, so a large offset or length cannot overflow. An offset after the
		// region makes the difference negative, and the length check rejects it.
		if r.Offset < 0 || r.Length < 0 || r.Length > regionLength-r.Offset {
			return nil, fmt.Errorf("section metadata range %d is invalid: offset=%d length=%d region length=%d", i, r.Offset, r.Length, regionLength)
		}
		absolute[i] = absoluteByteRange{Offset: regionOffset + r.Offset, Length: r.Length}
	}
	return sr.rr.ReadRanges(ctx, absolute)
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
