package logs

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/bufpool"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/windowing"
)

// newDecoder creates a new [decoder] for the given [dataobj.SectionReader].
func newDecoder(reader dataobj.SectionReader) *decoder {
	return &decoder{sr: reader}
}

type decoder struct {
	sr dataobj.SectionReader
}

// Columns describes the set of columns in the section.
func (rd *decoder) Columns(ctx context.Context) ([]*logsmd.ColumnDesc, error) {
	rc, err := rd.sr.Metadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading streams section metadata: %w", err)
	}
	defer rc.Close()

	br := bufpool.GetReader(rc)
	defer bufpool.PutReader(br)

	md, err := decodeLogsMetadata(br)
	if err != nil {
		return nil, err
	}
	return md.Columns, nil
}

// Pages retrieves the set of pages for the provided columns. The order of page
// lists emitted by the sequence matches the order of columns provided: the
// first page list corresponds to the first column, and so on.
func (rd *decoder) Pages(ctx context.Context, columns []*logsmd.ColumnDesc) result.Seq[[]*logsmd.PageDesc] {
	return result.Iter(func(yield func([]*logsmd.PageDesc) bool) error {
		results := make([][]*logsmd.PageDesc, len(columns))

		columnInfo := func(c *logsmd.ColumnDesc) (uint64, uint64) {
			return c.GetInfo().MetadataOffset, c.GetInfo().MetadataSize
		}

		for window := range windowing.Iter(columns, columnInfo, windowing.S3WindowSize) {
			if len(window) == 0 {
				continue
			}

			var (
				windowOffset = window.Start().GetInfo().MetadataOffset
				windowSize   = (window.End().GetInfo().MetadataOffset + window.End().GetInfo().MetadataSize) - windowOffset
			)

			rc, err := rd.sr.DataRange(ctx, int64(windowOffset), int64(windowSize))
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

				// wp.Index is the position of the column in the original pages slice;
				// this retains the proper order of data in results.
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

// ReadPages reads the provided set of pages, iterating over their data
// matching the argument order. If an error is encountered while retrieving
// pages, an error is emitted from the sequence and iteration stops.
func (rd *decoder) ReadPages(ctx context.Context, pages []*logsmd.PageDesc) result.Seq[dataset.PageData] {
	return result.Iter(func(yield func(dataset.PageData) bool) error {
		results := make([]dataset.PageData, len(pages))

		pageInfo := func(p *logsmd.PageDesc) (uint64, uint64) {
			return p.GetInfo().DataOffset, p.GetInfo().DataSize
		}

		// TODO(rfratto): If there are many windows, it may make sense to read them
		// in parallel.
		for window := range windowing.Iter(pages, pageInfo, windowing.S3WindowSize) {
			if len(window) == 0 {
				continue
			}

			var (
				windowOffset = window.Start().GetInfo().DataOffset
				windowSize   = (window.End().GetInfo().DataOffset + window.End().GetInfo().DataSize) - windowOffset
			)

			rc, err := rd.sr.DataRange(ctx, int64(windowOffset), int64(windowSize))
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

				// wp.Index is the position of the page in the original pages slice;
				// this retains the proper order of data in results.
				results[wp.Index] = dataset.PageData(data[dataOffset : dataOffset+wp.Data.GetInfo().DataSize])
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
