package columnar

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/bufpool"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/protocodec"
	"github.com/grafana/loki/v3/pkg/util/rangeio"
)

// A Decoder allows reading an encoded dataset-based section.
type Decoder struct {
	sr dataobj.SectionReader
}

// NewDecoder creates a new [Decoder] for the given [dataobj.SectionReader]. The
// formatVersion argument must denote the format version of the data being
// decoded.
//
// NewDecoder returns an error if the format version is not supported.
func NewDecoder(reader dataobj.SectionReader, formatVersion uint32) (*Decoder, error) {
	if formatVersion != FormatVersion {
		return nil, fmt.Errorf("unsupported format version %d", formatVersion)
	}

	return &Decoder{sr: reader}, nil
}

// SectionMetadata returns the metadata for the section.
func (dec *Decoder) SectionMetadata(ctx context.Context) (*datasetmd.SectionMetadata, error) {
	info, err := dec.getSectionInfo()
	if err != nil {
		return nil, err
	}

	rc, err := dec.sr.MetadataRange(ctx, int64(info.SectionMetadataOffset), int64(info.SectionMetadataLength))
	if err != nil {
		return nil, fmt.Errorf("reading section metadata: %w", err)
	}
	defer rc.Close()

	br := bufpool.GetReader(rc)
	defer bufpool.PutReader(br)

	var md datasetmd.SectionMetadata
	if err := protocodec.Decode(br, &md); err != nil {
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
		stats := dataset.StatsFromContext(ctx)
		startTime := time.Now()
		defer func() { stats.AddPageDownloadTime(time.Since(startTime)) }()

		ranges := make([]rangeio.Range, 0, len(columns))
		for _, column := range columns {
			ranges = append(ranges, rangeio.Range{
				Data:   make([]byte, column.ColumnMetadataLength),
				Offset: int64(column.ColumnMetadataOffset),
			})
		}

		reader := metadataRangeReader{Inner: dec.sr}
		err := rangeio.ReadRanges(ctx, reader, ranges)
		if err != nil {
			return err
		}

		for _, r := range ranges {
			var md datasetmd.ColumnMetadata
			if err := protocodec.Decode(bytes.NewReader(r.Data), &md); err != nil {
				return fmt.Errorf("decoding column metadata: %w", err)
			}

			if !yield(md.Pages) {
				return nil
			}
		}

		return nil
	})
}

type metadataRangeReader struct {
	Inner dataobj.SectionReader
}

var _ rangeio.Reader = (*metadataRangeReader)(nil)

func (rr metadataRangeReader) ReadRange(ctx context.Context, r rangeio.Range) (int, error) {
	rc, err := rr.Inner.MetadataRange(ctx, r.Offset, r.Len())
	if err != nil {
		return 0, err
	}
	defer rc.Close()

	return io.ReadFull(rc, r.Data)
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

// ReadPages reads the provided set of pages, iterating over their data matching
// the argument order. If an error is encountered while retrieving pages, an
// error is emitted from the sequence and iteration stops.
func (dec *Decoder) ReadPages(ctx context.Context, pages []*datasetmd.PageDesc) result.Seq[dataset.PageData] {
	return result.Iter(func(yield func(dataset.PageData) bool) error {
		stats := dataset.StatsFromContext(ctx)
		startTime := time.Now()
		defer func() { stats.AddPageDownloadTime(time.Since(startTime)) }()

		ranges := make([]rangeio.Range, 0, len(pages))
		for _, page := range pages {
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
	rc, err := rr.Inner.DataRange(ctx, r.Offset, r.Len())
	if err != nil {
		return 0, err
	}
	defer rc.Close()

	return io.ReadFull(rc, r.Data)
}
