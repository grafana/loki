package columnar

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd/v2"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

// Dataset is a [dataset.Dataset] implementation that reads from a set
// of [Columns].
type Dataset struct {
	dec  *Decoder
	cols []dataset.Column
}

var _ dataset.Dataset = (*Dataset)(nil)

// MakeDataset returns a [Dataset] from a section and a set of columns.
// MakeDataset returns an error if not all columns are from the provided
// section.
func MakeDataset(section *Section, columns []*Column) (*Dataset, error) {
	if len(columns) == 0 {
		return &Dataset{}, nil
	}

	for _, col := range columns {
		if col.Section != section {
			return nil, fmt.Errorf("all columns must be from the same section: got=%p want=%p", col.Section, section)
		}
	}

	var cols []dataset.Column
	for _, col := range columns {
		cols = append(cols, MakeDatasetColumn(section.decoder, col))
	}

	return &Dataset{dec: section.decoder, cols: cols}, nil
}

// Columns returns the set of [dataset.Column]s in the dataset. The order of
// returned columns matches the order from [newColumnsDataset]. The returned
// slice must not be modified.
func (ds *Dataset) Columns() []dataset.Column { return ds.cols }

// ListColumns returns an iterator over the columns in the dataset.
func (ds *Dataset) ListColumns(_ context.Context) result.Seq[dataset.Column] {
	return result.Iter(func(yield func(dataset.Column) bool) error {
		for _, col := range ds.cols {
			if !yield(col) {
				return nil
			}
		}
		return nil
	})
}

// ListPages returns an iterator over the pages in the dataset.
func (ds *Dataset) ListPages(ctx context.Context, columns []dataset.Column) result.Seq[dataset.Pages] {
	// We want to make a single request to the decoder here to allow it to
	// perform optimizations, so we need to unwrap our columns to get the
	// metadata per column.
	return result.Iter(func(yield func(dataset.Pages) bool) error {
		columnDescs := make([]*datasetmd.ColumnDesc, len(columns))
		for i, column := range columns {
			column, ok := column.(*DatasetColumn)
			if !ok {
				return fmt.Errorf("unexpected column type: got=%T want=*columnDataset", column)
			}
			columnDescs[i] = column.col.desc
		}

		for result := range ds.dec.Pages(ctx, columnDescs) {
			pageDescs, err := result.Value()

			pages := make([]dataset.Page, len(pageDescs))
			for i, pageDesc := range pageDescs {
				pages[i] = MakeDatasetPage(ds.dec, pageDesc)
			}
			if err != nil || !yield(pages) {
				return err
			}
		}

		return nil
	})
}

// ReadPages returns an iterator over page data for the given pages.
func (ds *Dataset) ReadPages(ctx context.Context, pages []dataset.Page) result.Seq[dataset.PageData] {
	// List with [columnsDataset.ListPages], we unwrap pages so we can pass them
	// down to our decoder in a single batch.
	return result.Iter(func(yield func(dataset.PageData) bool) error {
		pageDescs := make([]*datasetmd.PageDesc, len(pages))
		for i, page := range pages {
			page, ok := page.(*DatasetPage)
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

// DatasetColumn is a [dataset.Column] implementation that reads from a
// [Column]. It is automatically created when using [MakeDataset].
type DatasetColumn struct {
	dec *Decoder

	col  *Column
	info *dataset.ColumnInfo
}

// MakeDatasetColumn returns a [DatasetColumn] from a decoder and a column.
func MakeDatasetColumn(dec *Decoder, col *Column) *DatasetColumn {
	info := col.desc

	return &DatasetColumn{
		dec: dec,
		col: col,
		info: &dataset.ColumnInfo{
			Name:        col.Tag,
			Type:        datasetmd.ToV1ValueType(info.Type.Physical),
			Compression: datasetmd.ToV1CompressionType(info.Compression),

			PagesCount:       int(info.PagesCount),
			RowsCount:        int(info.RowsCount),
			ValuesCount:      int(info.ValuesCount),
			CompressedSize:   int(info.CompressedSize),
			UncompressedSize: int(info.UncompressedSize),

			Statistics: datasetmd.ToV1Statistics(info.Statistics),
		},
	}
}

var _ dataset.Column = (*DatasetColumn)(nil)

// ColumnInfo returns the [dataset.ColumnInfo] for the column.
func (ds *DatasetColumn) ColumnInfo() *dataset.ColumnInfo { return ds.info }

// ListPages returns a sequence of pages for the column.
func (ds *DatasetColumn) ListPages(ctx context.Context) result.Seq[dataset.Page] {
	return result.Iter(func(yield func(dataset.Page) bool) error {
		pageSets, err := result.Collect(ds.dec.Pages(ctx, []*datasetmd.ColumnDesc{ds.col.desc}))
		if err != nil {
			return err
		} else if len(pageSets) != 1 {
			return fmt.Errorf("unexpected number of page sets: got=%d want=1", len(pageSets))
		}

		for _, page := range pageSets[0] {
			if !yield(MakeDatasetPage(ds.dec, page)) {
				return nil
			}
		}

		return nil
	})
}

// DatasetPage is a [dataset.Page] implementation that reads from an individual
// page within a column.
type DatasetPage struct {
	dec *Decoder

	desc *datasetmd.PageDesc
	info *dataset.PageInfo
}

var _ dataset.Page = (*DatasetPage)(nil)

// MakeDatasetPage returns a [DatasetPage] from a decoder and page description.
func MakeDatasetPage(dec *Decoder, desc *datasetmd.PageDesc) *DatasetPage {
	return &DatasetPage{
		dec:  dec,
		desc: desc,
		info: &dataset.PageInfo{
			UncompressedSize: int(desc.UncompressedSize),
			CompressedSize:   int(desc.CompressedSize),
			CRC32:            desc.Crc32,
			RowCount:         int(desc.RowsCount),
			ValuesCount:      int(desc.ValuesCount),

			Encoding: datasetmd.ToV1Encoding(desc.Encoding),
			Stats:    datasetmd.ToV1Statistics(desc.Statistics),
		},
	}
}

// PageInfo returns the page information.
func (p *DatasetPage) PageInfo() *dataset.PageInfo { return p.info }

// ReadPage reads the page data.
func (p *DatasetPage) ReadPage(ctx context.Context) (dataset.PageData, error) {
	pages, err := result.Collect(p.dec.ReadPages(ctx, []*datasetmd.PageDesc{p.desc}))
	if err != nil {
		return nil, err
	} else if len(pages) != 1 {
		return nil, fmt.Errorf("unexpected number of pages: got=%d want=1", len(pages))
	}

	return pages[0], nil
}
