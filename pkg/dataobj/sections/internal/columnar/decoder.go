package columnar

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/protocodec"
	"github.com/grafana/loki/v3/pkg/util/rangeio"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// A Decoder allows reading an encoded dataset-based section.
type Decoder struct {
	sr dataobj.SectionReader

	formatVersion uint32
}

// NewDecoder creates a new [Decoder] for the given [dataobj.SectionReader]. The
// formatVersion argument is the format version of the data being decoded.
//
// NewDecoder returns an error if the format version is not supported.
func NewDecoder(reader dataobj.SectionReader, formatVersion uint32) (*Decoder, error) {
	if !IsSupportedVersion(formatVersion) {
		return nil, fmt.Errorf("unsupported format version %d", formatVersion)
	}

	return &Decoder{
		sr:            reader,
		formatVersion: formatVersion,
	}, nil
}

// SectionMetadata returns the metadata for the section.
func (dec *Decoder) SectionMetadata(ctx context.Context) (*datasetmd.SectionMetadata, error) {
	info, err := dec.getSectionInfo()
	if err != nil {
		return nil, err
	}

	metadata, err := dec.sr.MetadataRanges(ctx, []dataobj.RelativeByteRange{{
		Offset: int64(info.SectionMetadataOffset),
		Length: int64(info.SectionMetadataLength),
	}})
	if err != nil {
		return nil, fmt.Errorf("reading section metadata: %w", err)
	}

	var md datasetmd.SectionMetadata
	if err := protocodec.DecodeBytes(metadata[0], &md); err != nil {
		return nil, fmt.Errorf("decoding section metadata: %w", err)
	}
	return &md, nil
}

func (dec *Decoder) getSectionInfo() (*datasetmd.SectionInfoExtension, error) {
	data := dec.sr.ExtensionData()
	if len(data) == 0 {
		return nil, fmt.Errorf("section is missing required extension_data")
	}

	var ext datasetmd.SectionInfoExtension
	if err := protocodec.Decode(bytes.NewReader(data), &ext); err != nil {
		return nil, err
	}
	return &ext, nil
}

// Pages returns the set of pages for the provided columns. The order of slices
// of pages emitted by the iterator matches the order of the columns slice: the
// first slice corresponds to the first column, and so on.
func (dec *Decoder) Pages(ctx context.Context, columns []*datasetmd.ColumnDesc) result.Seq[[]*datasetmd.PageDesc] {
	return result.Iter(func(yield func([]*datasetmd.PageDesc) bool) error {
		region := xcap.RegionFromContext(ctx)
		startTime := time.Now()
		defer func() {
			region.Record(dataobj.StatDatasetPageDownloadTime.Observe(time.Since(startTime).Seconds()))
		}()

		ranges := make([]dataobj.RelativeByteRange, len(columns))
		for i, column := range columns {
			ranges[i] = dataobj.RelativeByteRange{
				Offset: int64(column.ColumnMetadataOffset),
				Length: int64(column.ColumnMetadataLength),
			}
		}

		columnMetadata, err := dec.sr.MetadataRanges(ctx, ranges)
		if err != nil {
			return fmt.Errorf("reading the metadata of %d columns: %w", len(columns), err)
		}

		for _, encoded := range columnMetadata {
			var md datasetmd.ColumnMetadata
			if err := protocodec.DecodeBytes(encoded, &md); err != nil {
				return fmt.Errorf("decoding column metadata: %w", err)
			}

			if !yield(md.Pages) {
				return nil
			}
		}

		return nil
	})
}

// ReadPages reads the provided set of pages, iterating over their data matching
// the argument order. If an error is encountered while retrieving pages, an
// error is emitted from the sequence and iteration stops.
//
// ReadPages emits one error and no pages if a page does not fit in the data
// region of the section, or if its uncompressed size does not fit in an int.
func (dec *Decoder) ReadPages(ctx context.Context, pages []*datasetmd.PageDesc) result.Seq[dataset.PageData] {
	return result.Iter(func(yield func(dataset.PageData) bool) error {
		region := xcap.RegionFromContext(ctx)
		startTime := time.Now()
		defer func() {
			region.Record(dataobj.StatDatasetPageDownloadTime.Observe(time.Since(startTime).Seconds()))
		}()

		// A page descriptor can be corrupt. Check each page against the data region before the
		// loop allocates its buffer. A corrupt size would force a huge allocation or a panic.
		dataSize := dec.sr.DataSize()
		ranges := make([]rangeio.Range, 0, len(pages))
		for i, page := range pages {
			// Subtract instead of add, so a large offset or size cannot overflow. DataSize is
			// negative for a region larger than math.MaxInt64 bytes. Reject it first, because
			// uint64(dataSize) would wrap to a huge value.
			if dataSize < 0 || page.DataOffset > uint64(dataSize) || page.DataSize > uint64(dataSize)-page.DataOffset {
				return fmt.Errorf("page %d is outside the data region: offset=%d size=%d region length=%d", i, page.DataOffset, page.DataSize, dataSize)
			}

			// The page descriptor stores the size as an int. A larger value would
			// wrap to a negative size.
			if page.UncompressedSize > math.MaxInt {
				return fmt.Errorf("page %d has an uncompressed size that does not fit in an int: size=%d", i, page.UncompressedSize)
			}

			ranges = append(ranges, rangeio.Range{
				Data:   make([]byte, page.DataSize),
				Offset: int64(page.DataOffset),
			})
		}

		reader := dataRangeReader{Inner: dec.sr}
		err := rangeio.ReadRanges(ctx, reader, ranges)
		if err != nil {
			return err
		}

		for _, r := range ranges {
			if !yield(dataset.PageData(r.Data)) {
				return nil
			}
		}

		return nil
	})
}

type dataRangeReader struct {
	Inner dataobj.SectionReader
}

var _ rangeio.Reader = (*dataRangeReader)(nil)

func (rr dataRangeReader) ReadRange(ctx context.Context, r rangeio.Range) (int, error) {
	rc, err := rr.Inner.DataRange(ctx, dataobj.RelativeByteRange{Offset: r.Offset, Length: r.Len()})
	if err != nil {
		return 0, err
	}
	defer rc.Close()

	return io.ReadFull(rc, r.Data)
}
