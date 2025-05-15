package encoding

import (
	"bytes"
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

// NewSteramsDecoder creates a new StreamsDecoder for the given section reader.
// NewStreamsDecoder returns an error if the section reader is not a streams
// section.
func NewStreamsDecoder(reader SectionReader) (StreamsDecoder, error) {
	typ, err := reader.Type()
	if err != nil {
		return nil, fmt.Errorf("failed to read section type: %w", err)
	} else if got, want := typ, SectionTypeStreams; got != want {
		return nil, fmt.Errorf("unexpected section type: got=%s want=%s", got, want)
	}

	return &streamsDecoder{sr: reader}, nil
}

type streamsDecoder struct {
	// TODO(rfratto): restrict sections from reading outside of their regions.

	sr SectionReader
}

func (rd *streamsDecoder) Columns(ctx context.Context) ([]*streamsmd.ColumnDesc, error) {
	rc, err := rd.sr.Metadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading streams section metadata: %w", err)
	}
	defer rc.Close()

	br, release := getBufioReader(rc)
	defer release()

	md, err := decodeStreamsMetadata(br)
	if err != nil {
		return nil, err
	}
	return md.Columns, nil
}

func (rd *streamsDecoder) Pages(ctx context.Context, columns []*streamsmd.ColumnDesc) result.Seq[[]*streamsmd.PageDesc] {
	return result.Iter(func(yield func([]*streamsmd.PageDesc) bool) error {
		results := make([][]*streamsmd.PageDesc, len(columns))

		columnInfo := func(c *streamsmd.ColumnDesc) (uint64, uint64) {
			return c.GetInfo().MetadataOffset, c.GetInfo().MetadataSize
		}

		for window := range iterWindows(columns, columnInfo, windowSize) {
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

				md, err := decodeStreamsColumnMetadata(r)
				if err != nil {
					return err
				}

				// wp.Position is the position of the column in the original pages
				// slice; this retains the proper order of data in results.
				results[wp.Position] = md.Pages
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

func (rd *streamsDecoder) ReadPages(ctx context.Context, pages []*streamsmd.PageDesc) result.Seq[dataset.PageData] {
	return result.Iter(func(yield func(dataset.PageData) bool) error {
		results := make([]dataset.PageData, len(pages))

		pageInfo := func(p *streamsmd.PageDesc) (uint64, uint64) {
			return p.GetInfo().DataOffset, p.GetInfo().DataSize
		}

		// TODO(rfratto): If there are many windows, it may make sense to read them
		// in parallel.
		for window := range iterWindows(pages, pageInfo, windowSize) {
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

				// wp.Position is the position of the page in the original pages slice;
				// this retains the proper order of data in results.
				results[wp.Position] = dataset.PageData(data[dataOffset : dataOffset+wp.Data.GetInfo().DataSize])
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
