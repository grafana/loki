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
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/windowing"
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

		results := make([][]*datasetmd.PageDesc, len(columns))

		columnInfo := func(c *datasetmd.ColumnDesc) (uint64, uint64) {
			return c.ColumnMetadataOffset, c.ColumnMetadataLength
		}

		for window := range windowing.Iter(columns, columnInfo, windowing.S3WindowSize) {
			if len(window) == 0 {
				continue
			}

			var (
				windowOffset = window.Start().ColumnMetadataOffset
				windowSize   = (window.End().ColumnMetadataOffset + window.End().ColumnMetadataLength) - windowOffset
			)

			rc, err := dec.sr.MetadataRange(ctx, int64(windowOffset), int64(windowSize))
			if err != nil {
				return fmt.Errorf("reading column metadata: %w", err)
			}
			data, err := readAndClose(rc, windowSize)
			if err != nil {
				return fmt.Errorf("reading column metadata: %w", err)
			}

			for _, wp := range window {
				// Find the slice in the data for this column.
				var (
					columnOffset = wp.Data.ColumnMetadataOffset
					dataOffset   = columnOffset - windowOffset
				)

				r := bytes.NewReader(data[dataOffset : dataOffset+wp.Data.ColumnMetadataLength])

				var md datasetmd.ColumnMetadata
				if err := protocodec.Decode(r, &md); err != nil {
					return fmt.Errorf("decoding column metadata: %w", err)
				}

				// wp.Index is the position of the column in the original pages
				// slice; this retains the proper order of data in results.
				results[wp.Index] = md.Pages
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

		results := make([]dataset.PageData, len(pages))

		pageInfo := func(p *datasetmd.PageDesc) (uint64, uint64) {
			return p.DataOffset, p.DataSize
		}

		// TODO(rfratto): If there are many windows, it may make sense to read them
		// in parallel.
		for window := range windowing.Iter(pages, pageInfo, windowing.S3WindowSize) {
			if len(window) == 0 {
				continue
			}

			var (
				windowOffset = window.Start().DataOffset
				windowSize   = (window.End().DataOffset + window.End().DataSize) - windowOffset
			)

			rc, err := dec.sr.DataRange(ctx, int64(windowOffset), int64(windowSize))
			if err != nil {
				return fmt.Errorf("reading page data: %w", err)
			}

			buffer := bufpool.Get(int(windowSize))
			if err := copyAndClose(buffer, rc); err != nil {
				bufpool.Put(buffer)
				return fmt.Errorf("reading page data: %w", err)
			}
			data := buffer.Bytes()

			for _, wp := range window {
				// Find the slice in the data for this page.
				var (
					pageOffset = wp.Data.DataOffset
					dataOffset = pageOffset - windowOffset
				)

				// wp.Index is the position of the page in the original pages slice;
				// this retains the proper order of data in results.
				//
				// We need to make a copy here of the slice since data is pooled
				// (and we don't want to hold on to the entire window if we
				// don't need to).
				results[wp.Index] = dataset.PageData(bytes.Clone(data[dataOffset : dataOffset+wp.Data.DataSize]))
			}

			bufpool.Put(buffer)
		}

		for _, data := range results {
			if !yield(data) {
				return nil
			}
		}

		return nil
	})
}

// copyAndClose copies the data from rc into the destination writer w and then
// closes rc.
func copyAndClose(dst io.Writer, rc io.ReadCloser) error {
	defer rc.Close()

	if _, err := io.Copy(dst, rc); err != nil {
		return fmt.Errorf("copying data: %w", err)
	}
	return nil
}
