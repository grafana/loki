package dataobj

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/util/rangeio"
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

	absoluteOffset := sr.startOff + int64(region.Offset) + offset
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

	absoluteOffset := sr.startOff + int64(metadataRegion.Offset) + offset
	return sr.rr.ReadRange(ctx, absoluteOffset, length)
}

func (sr *sectionReader) DataRanges(ctx context.Context, ranges []rangeio.Range) error {
	region := sr.sec.GetLayout().GetData()
	if region == nil {
		return fmt.Errorf("section has no data")
	} else if region.Offset > math.MaxInt64 {
		return fmt.Errorf("section data offset is too large")
	} else if region.Length > math.MaxInt64 {
		return fmt.Errorf("section data length is too large")
	}

	absolute, err := sr.absoluteRanges(ranges, int64(region.Offset), int64(region.Length))
	if err != nil {
		return err
	}
	return sr.readRanges(ctx, absolute)
}

func (sr *sectionReader) MetadataRanges(ctx context.Context, ranges []rangeio.Range) error {
	region := sr.sec.GetLayout().GetMetadata()
	if region == nil {
		return fmt.Errorf("section has no metadata")
	}

	absolute, err := sr.absoluteRanges(ranges, int64(region.Offset), int64(region.Length))
	if err != nil {
		return err
	}
	return sr.readRanges(ctx, absolute)
}

// readRanges reads the ranges, and returns an unexpected EOF instead of an EOF. absoluteRanges checked that every
// range lies in the section, so an EOF means a short read. The column readers take a bare EOF as the end of a column,
// and would return fewer rows without an error.
func (sr *sectionReader) readRanges(ctx context.Context, ranges []rangeio.Range) error {
	err := sr.rr.ReadRanges(ctx, ranges)
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("reading section ranges: %w (cause: %v)", io.ErrUnexpectedEOF, err)
	}
	return err
}

// absoluteRanges checks that each range lies in the region, and returns the ranges with offsets from the start of the
// file. The ranges share their buffers with the input.
func (sr *sectionReader) absoluteRanges(ranges []rangeio.Range, regionOffset, regionLength int64) ([]rangeio.Range, error) {
	absolute := make([]rangeio.Range, len(ranges))
	for i, r := range ranges {
		var (
			start = r.Offset
			end   = r.Offset + r.Len() - 1
		)
		if start < 0 {
			return nil, fmt.Errorf("parameters must not be negative: offset=%d length=%d", r.Offset, r.Len())
		} else if start > regionLength || end > regionLength {
			return nil, fmt.Errorf("section data is invalid: start=%d end=%d length=%d", start, end, regionLength)
		}
		absolute[i] = rangeio.Range{Data: r.Data, Offset: sr.startOff + regionOffset + r.Offset}
	}
	return absolute, nil
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
