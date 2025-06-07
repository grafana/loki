package streams

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

// columnsDataset is a [dataset.Dataset] that reads from a set of [Column]s.
type columnsDataset struct {
	dec  *decoder
	cols []dataset.Column
}

var _ dataset.Dataset = (*columnsDataset)(nil)

// newColumnsDataset returns a new [columnsDataset] from a set of [Column]s.
// newColumnsDataset returns an error if not all columns are from the same
// section.
func newColumnsDataset(columns []*Column) (*columnsDataset, error) {
	if len(columns) == 0 {
		return &columnsDataset{}, nil
	}

	section := columns[0].Section
	for _, col := range columns[1:] {
		if col.Section != section {
			return nil, fmt.Errorf("all columns must be from the same section: got=%p want=%p", col.Section, section)
		}
	}

	dec := newDecoder(section.reader)

	var cols []dataset.Column
	for _, col := range columns {
		cols = append(cols, newColumnDataset(dec, col))
	}

	return &columnsDataset{dec: dec, cols: cols}, nil
}

// Columns returns the set of [dataset.Column]s in the dataset. The order of
// returned columns matches the order from [newColumnsDataset]. The returned
// slice must not be modified.
func (ds *columnsDataset) Columns() []dataset.Column { return ds.cols }

func (ds *columnsDataset) ListColumns(_ context.Context) result.Seq[dataset.Column] {
	return result.Iter(func(yield func(dataset.Column) bool) error {
		for _, col := range ds.cols {
			if !yield(col) {
				return nil
			}
		}
		return nil
	})
}

func (ds *columnsDataset) ListPages(ctx context.Context, columns []dataset.Column) result.Seq[dataset.Pages] {
	// We want to make a single request to the decoder here to allow it to
	// perform optimizations, so we need to unwrap our columns to get the
	// metadata per column.
	return result.Iter(func(yield func(dataset.Pages) bool) error {
		columnDescs := make([]*streamsmd.ColumnDesc, len(columns))
		for i, column := range columns {
			column, ok := column.(*columnDataset)
			if !ok {
				return fmt.Errorf("unexpected column type: got=%T want=*columnDataset", column)
			}
			columnDescs[i] = column.col.desc
		}

		for result := range ds.dec.Pages(ctx, columnDescs) {
			pageDescs, err := result.Value()

			pages := make([]dataset.Page, len(pageDescs))
			for i, pageDesc := range pageDescs {
				pages[i] = newDatasetPage(ds.dec, pageDesc)
			}
			if err != nil || !yield(pages) {
				return err
			}
		}

		return nil
	})
}

func (ds *columnsDataset) ReadPages(ctx context.Context, pages []dataset.Page) result.Seq[dataset.PageData] {
	// List with [columnsDataset.ListPages], we unwrap pages so we can pass them
	// down to our decoder in a single batch.
	return result.Iter(func(yield func(dataset.PageData) bool) error {
		pageDescs := make([]*streamsmd.PageDesc, len(pages))
		for i, page := range pages {
			page, ok := page.(*datasetPage)
			if !ok {
				return fmt.Errorf("unexpected page type: got=%T want=*datasetPage", page)
			}
			pageDescs[i] = page.desc
		}

		for result := range ds.dec.ReadPages(ctx, pageDescs) {
			data, err := result.Value()
			if err != nil || !yield(data) {
				return err
			}
		}

		return nil
	})
}

type columnDataset struct {
	dec *decoder

	col  *Column
	info *dataset.ColumnInfo
}

func newColumnDataset(dec *decoder, col *Column) *columnDataset {
	info := col.desc.Info

	return &columnDataset{
		dec: dec,
		col: col,
		info: &dataset.ColumnInfo{
			Name:        info.Name,
			Type:        info.ValueType,
			Compression: info.Compression,

			RowsCount:        int(info.RowsCount),
			ValuesCount:      int(info.ValuesCount),
			CompressedSize:   int(info.CompressedSize),
			UncompressedSize: int(info.UncompressedSize),

			Statistics: info.Statistics,
		},
	}
}

var _ dataset.Column = (*columnDataset)(nil)

func (ds *columnDataset) ColumnInfo() *dataset.ColumnInfo { return ds.info }

func (ds *columnDataset) ListPages(ctx context.Context) result.Seq[dataset.Page] {
	return result.Iter(func(yield func(dataset.Page) bool) error {
		pageSets, err := result.Collect(ds.dec.Pages(ctx, []*streamsmd.ColumnDesc{ds.col.desc}))
		if err != nil {
			return err
		} else if len(pageSets) != 1 {
			return fmt.Errorf("unexpected number of page sets: got=%d want=1", len(pageSets))
		}

		for _, page := range pageSets[0] {
			if !yield(newDatasetPage(ds.dec, page)) {
				return nil
			}
		}

		return nil
	})
}

type datasetPage struct {
	dec *decoder

	desc *streamsmd.PageDesc
	info *dataset.PageInfo
}

var _ dataset.Page = (*datasetPage)(nil)

func newDatasetPage(dec *decoder, desc *streamsmd.PageDesc) *datasetPage {
	info := desc.Info

	return &datasetPage{
		dec:  dec,
		desc: desc,
		info: &dataset.PageInfo{
			UncompressedSize: int(info.UncompressedSize),
			CompressedSize:   int(info.CompressedSize),
			CRC32:            info.Crc32,
			RowCount:         int(info.RowsCount),
			ValuesCount:      int(info.ValuesCount),

			Encoding: info.Encoding,
			Stats:    info.Statistics,
		},
	}
}

func (p *datasetPage) PageInfo() *dataset.PageInfo { return p.info }

func (p *datasetPage) ReadPage(ctx context.Context) (dataset.PageData, error) {
	pages, err := result.Collect(p.dec.ReadPages(ctx, []*streamsmd.PageDesc{p.desc}))
	if err != nil {
		return nil, err
	} else if len(pages) != 1 {
		return nil, fmt.Errorf("unexpected number of pages: got=%d want=1", len(pages))
	}

	return pages[0], nil
}
