package dataobj

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"

	"github.com/grafana/loki/v3/pkg/dataobj/bucketstat"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/metadata"
	"github.com/grafana/loki/v3/pkg/xcap"
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

	regionOffset := sr.startOff + int64(region.Offset)
	regionLength := int64(region.Length)
	if regionOffset < sr.startOff || regionOffset+regionLength < regionOffset {
		return nil, fmt.Errorf("section data region overflows: start offset=%d region offset=%d region length=%d", sr.startOff, region.Offset, regionLength)
	}

	// Validate bounds within the range of the section. Subtract instead of add, so a large offset or
	// length cannot overflow. An offset after the region makes the difference negative, and the
	// length check rejects it.
	if r.Length > regionLength-r.Offset {
		return nil, fmt.Errorf("section data range is invalid: offset=%d length=%d region length=%d", r.Offset, r.Length, regionLength)
	}

	// Return an empty reader for a range of zero length, without a read. An S3 range request cannot
	// have a length of zero, so the bucket rejects it.
	if r.Length == 0 {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}

	return sr.rr.ReadRange(ctx, regionOffset+r.Offset, r.Length)
}

// MetadataRanges returns the bytes of each range of the metadata region of the section. The offset
// of each range counts from the start of that region, not from the start of the object.
//
// It checks every range before it reads any. It records these statistics in the xcap region of ctx:
//   - [metadata.StatMetadataRequestsToStorage]
//   - [metadata.StatMetadataBytesFromStorage]
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
		return nil, fmt.Errorf("section metadata region overflows: start offset=%d region offset=%d region length=%d", sr.startOff, metadataRegion.Offset, regionLength)
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

	// The region of ctx also holds the statistics of concurrent work, such as the reads of pages. An
	// isolated region holds only the statistics of this call, so the readers record into it.
	return xcap.RunIsolated(ctx, "metadata_ranges",
		func(ctx context.Context) ([][]byte, error) {
			return sr.rr.ReadRanges(ctx, absolute)
		},
		// The instrumented bucket reader counts a request before it sends it, so the count includes the
		// requests of a failed read.
		xcap.MergeDerived(
			xcap.Derive(metadata.StatMetadataRequestsToStorage, bucketstat.StatObjectRequestsGetRange),
			xcap.Derive(metadata.StatMetadataBytesFromStorage, bucketstat.StatObjectBytesDownloaded),
		))
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
