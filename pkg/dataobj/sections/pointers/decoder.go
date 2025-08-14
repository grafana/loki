package pointers

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/pointersmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/bufpool"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/windowing"
)

// newDecoder creates a new [decoder] for the given [dataobj.SectionReader].
func newDecoder(reader dataobj.SectionReader) *decoder {
	return &decoder{sr: reader}
}

// decoder supports decoding the raw underlying data for a pointers section.
type decoder struct {
	sr dataobj.SectionReader
}

// Metadata returns the metadata for the pointers section.
func (rd *decoder) Metadata(ctx context.Context) (*pointersmd.Metadata, error) {
	rc, err := rd.sr.MetadataRange(ctx, 0, rd.sr.MetadataSize())
	if err != nil {
		return nil, fmt.Errorf("reading pointers section metadata: %w", err)
	}
	defer rc.Close()

	br := bufpool.GetReader(rc)
	defer bufpool.PutReader(br)

	return decodePointersMetadata(br)
}

// Pages retrieves the set of pages for the provided columns. The order of page
// lists emitted by the sequence matches the order of columns provided: the
// first page list corresponds to the first column, and so on.
func (rd *decoder) Pages(ctx context.Context, columns []*pointersmd.ColumnDesc) result.Seq[[]*pointersmd.PageDesc] {
	return result.Iter(func(yield func([]*pointersmd.PageDesc) bool) error {
		results := make([][]*pointersmd.PageDesc, len(columns))

		columnInfo := func(c *pointersmd.ColumnDesc) (uint64, uint64) {
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
				return fmt.Errorf("read column data: %w", err)
			}

			for _, wp := range window {
				// Find the slice in the data for this column.
				var (
					columnOffset = wp.Data.GetInfo().MetadataOffset
					dataOffset   = columnOffset - windowOffset
				)

				r := bytes.NewReader(data[dataOffset : dataOffset+wp.Data.GetInfo().MetadataSize])

				md, err := decodePointersColumnMetadata(r)
				if err != nil {
					return err
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

// ReadPages reads the provided set of pages, iterating over their data
// matching the argument order. If an error is encountered while retrieving
// pages, an error is emitted from the sequence and iteration stops.
func (rd *decoder) ReadPages(ctx context.Context, pages []*pointersmd.PageDesc) result.Seq[dataset.PageData] {
	return result.Iter(func(yield func(dataset.PageData) bool) error {
		results := make([]dataset.PageData, len(pages))

		pageInfo := func(p *pointersmd.PageDesc) (uint64, uint64) {
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

			buffer := bufpool.Get(int(windowSize))
			if err := copyAndClose(buffer, rc); err != nil {
				bufpool.Put(buffer)
				return fmt.Errorf("read page data: %w", err)
			}
			data := buffer.Bytes()

			for _, wp := range window {
				// Find the slice in the data for this page.
				var (
					pageOffset = wp.Data.GetInfo().DataOffset
					dataOffset = pageOffset - windowOffset
				)

				// wp.Index is the position of the page in the original pages slice;
				// this retains the proper order of data in results.
				//
				// We need to make a copy here of the slice since data is pooled (and
				// we don't want to hold on to the entire window if we don't need to).
				results[wp.Index] = dataset.PageData(bytes.Clone(data[dataOffset : dataOffset+wp.Data.GetInfo().DataSize]))
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
