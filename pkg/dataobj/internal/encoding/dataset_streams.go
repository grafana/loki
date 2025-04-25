package encoding

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

// StreamsDataset implements returns a [dataset.Dataset] from a
// [StreamsDecoder] for the given section.
func StreamsDataset(dec StreamsDecoder, sec *filemd.SectionInfo) dataset.Dataset {
	return &streamsDataset{dec: dec, sec: sec}
}

type streamsDataset struct {
	dec StreamsDecoder
	sec *filemd.SectionInfo
}

func (ds *streamsDataset) ListColumns(ctx context.Context) result.Seq[dataset.Column] {
	return result.Iter(func(yield func(dataset.Column) bool) error {
		columns, err := ds.dec.Columns(ctx, ds.sec)
		if err != nil {
			return err
		}

		for _, column := range columns {
			if !yield(&streamsDatasetColumn{dec: ds.dec, desc: column}) {
				return nil
			}
		}

		return err
	})

}

func (ds *streamsDataset) ListPages(ctx context.Context, columns []dataset.Column) result.Seq[dataset.Pages] {
	// We unwrap columns to get the underlying metadata and rewrap to
	// dataset.Page to be able to allow the underlying decoder to read multiple
	// column metadatas in a single call.
	return result.Iter(func(yield func(dataset.Pages) bool) error {
		descs := make([]*streamsmd.ColumnDesc, len(columns))
		for i, column := range columns {
			column, ok := column.(*streamsDatasetColumn)
			if !ok {
				return fmt.Errorf("unexpected column type: got=%T want=*streamsDatasetColumn", column)
			}
			descs[i] = column.desc
		}

		for result := range ds.dec.Pages(ctx, descs) {
			pagesDescs, err := result.Value()

			pages := make([]dataset.Page, len(pagesDescs))
			for i, pageDesc := range pagesDescs {
				pages[i] = &streamsDatasetPage{dec: ds.dec, desc: pageDesc}
			}
			if err != nil || !yield(pages) {
				return err
			}
		}
		return nil
	})
}

func (ds *streamsDataset) ReadPages(ctx context.Context, pages []dataset.Page) result.Seq[dataset.PageData] {
	// We unwrap columns to get the underlying metadata and rewrap to
	// dataset.Page to be able to allow the underlying decoder to read multiple
	// pages in a single call.
	return result.Iter(func(yield func(dataset.PageData) bool) error {
		descs := make([]*streamsmd.PageDesc, len(pages))
		for i, page := range pages {
			page, ok := page.(*streamsDatasetPage)
			if !ok {
				return fmt.Errorf("unexpected page type: got=%T want=*streamsDatasetPage", page)
			}
			descs[i] = page.desc
		}

		for result := range ds.dec.ReadPages(ctx, descs) {
			data, err := result.Value()
			if err != nil || !yield(data) {
				return err
			}
		}

		return nil
	})
}

type streamsDatasetColumn struct {
	dec  StreamsDecoder
	desc *streamsmd.ColumnDesc

	info *dataset.ColumnInfo
}

func (col *streamsDatasetColumn) ColumnInfo() *dataset.ColumnInfo {
	if col.info != nil {
		return col.info
	}

	col.info = &dataset.ColumnInfo{
		Name:        col.desc.Info.Name,
		Type:        col.desc.Info.ValueType,
		Compression: col.desc.Info.Compression,

		RowsCount:        int(col.desc.Info.RowsCount),
		ValuesCount:      int(col.desc.Info.ValuesCount),
		CompressedSize:   int(col.desc.Info.CompressedSize),
		UncompressedSize: int(col.desc.Info.UncompressedSize),

		Statistics: col.desc.Info.Statistics,
	}
	return col.info
}

func (col *streamsDatasetColumn) ListPages(ctx context.Context) result.Seq[dataset.Page] {
	return result.Iter(func(yield func(dataset.Page) bool) error {
		pageSets, err := result.Collect(col.dec.Pages(ctx, []*streamsmd.ColumnDesc{col.desc}))
		if err != nil {
			return err
		} else if len(pageSets) != 1 {
			return fmt.Errorf("unexpected number of page sets: got=%d want=1", len(pageSets))
		}

		for _, page := range pageSets[0] {
			if !yield(&streamsDatasetPage{dec: col.dec, desc: page}) {
				return nil
			}
		}

		return nil
	})
}

type streamsDatasetPage struct {
	dec  StreamsDecoder
	desc *streamsmd.PageDesc

	info *dataset.PageInfo
}

func (p *streamsDatasetPage) PageInfo() *dataset.PageInfo {
	if p.info != nil {
		return p.info
	}

	p.info = &dataset.PageInfo{
		UncompressedSize: int(p.desc.Info.UncompressedSize),
		CompressedSize:   int(p.desc.Info.CompressedSize),
		CRC32:            p.desc.Info.Crc32,
		RowCount:         int(p.desc.Info.RowsCount),
		ValuesCount:      int(p.desc.Info.ValuesCount),

		Encoding: p.desc.Info.Encoding,
		Stats:    p.desc.Info.Statistics,
	}
	return p.info
}

func (p *streamsDatasetPage) ReadPage(ctx context.Context) (dataset.PageData, error) {
	pages, err := result.Collect(p.dec.ReadPages(ctx, []*streamsmd.PageDesc{p.desc}))
	if err != nil {
		return nil, err
	} else if len(pages) != 1 {
		return nil, fmt.Errorf("unexpected number of pages: got=%d want=1", len(pages))
	}

	return pages[0], nil
}
