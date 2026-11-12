package watermarks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// Iter iterates over the watermarks in the provided object. It visits the
// watermarks sections in object order.
func Iter(ctx context.Context, obj *dataobj.Object) result.Seq[Watermark] {
	return result.Iter(func(yield func(Watermark) bool) error {
		for i, section := range obj.Sections().Filter(CheckSection) {
			sec, err := Open(ctx, section)
			if err != nil {
				return fmt.Errorf("opening section %d: %w", i, err)
			}

			for res := range IterSection(ctx, sec) {
				if res.Err() != nil || !yield(res.MustValue()) {
					return res.Err()
				}
			}
		}

		return nil
	})
}

// IterSection iterates over the watermarks of one section.
func IterSection(ctx context.Context, section *Section) result.Seq[Watermark] {
	return result.Iter(func(yield func(Watermark) bool) error {
		if section.isEmpty() {
			return nil
		}

		dset, err := section.makeDataset()
		if err != nil {
			return fmt.Errorf("creating columns dataset: %w", err)
		}

		columns, err := result.Collect(dset.ListColumns(ctx))
		if err != nil {
			return err
		}

		r := dataset.NewRowReader(dataset.RowReaderOptions{
			Dataset:           dset,
			ProjectedColumns:  columns,
			PrefetchAllOnOpen: true,
		})
		defer r.Close()

		if err := r.Open(ctx); err != nil {
			return err
		}

		var rows [1024]dataset.Row
		for {
			n, err := r.Read(ctx, rows[:])
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			} else if n == 0 && errors.Is(err, io.EOF) {
				return nil
			}

			var wm Watermark
			for _, row := range rows[:n] {
				if err := decodeRow(section.Columns(), row, &wm); err != nil {
					return err
				}

				if !yield(wm) {
					return nil
				}
			}
		}
	})
}

// makeDataset builds a dataset from only the recognized columns, so rows stay
// aligned with Columns() and columns from a newer Loki are skipped, not decoded.
func (s *Section) makeDataset() (*columnar.Dataset, error) {
	recognized := s.Columns()
	inner := make([]*columnar.Column, len(recognized))
	for i, col := range recognized {
		inner[i] = col.inner
	}
	return columnar.MakeDataset(s.inner, inner)
}

// decodeRow decodes a [Watermark] from a [dataset.Row], using the provided
// columns to determine the column type. The list of columns must match the
// columns used to create the row.
func decodeRow(columns []*Column, row dataset.Row, wm *Watermark) error {
	if len(row.Values) > len(columns) {
		return fmt.Errorf("row has %d values for %d columns", len(row.Values), len(columns))
	}

	*wm = Watermark{}
	for columnIndex, columnValue := range row.Values {
		column := columns[columnIndex]
		switch column.Type {
		case ColumnTypeTopic:
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_BINARY {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			// An empty topic is invalid, so reject nil and empty values.
			if columnValue.IsNil() || columnValue.IsZero() {
				return fmt.Errorf("nil or zero value for %s", column.Type)
			}
			wm.Topic = string(columnValue.Binary())

		case ColumnTypePartition:
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			// Partition 0 is valid, so reject nil and negative values.
			if columnValue.IsNil() {
				return fmt.Errorf("nil value for %s", column.Type)
			}
			partition := columnValue.Int64()
			if partition < 0 || partition > math.MaxInt32 {
				return fmt.Errorf("value %d of %s is negative or overflows int32", partition, column.Type)
			}
			wm.Partition = int32(partition)

		case ColumnTypeWatermark:
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			// Offset 0 is valid, so reject nil and negative values.
			if columnValue.IsNil() {
				return fmt.Errorf("nil value for %s", column.Type)
			}
			wm.Watermark = columnValue.Int64()
			if wm.Watermark < 0 {
				return fmt.Errorf("value %d of %s is negative", wm.Watermark, column.Type)
			}

		default:
			return fmt.Errorf("unexpected column type %s", column.Type)
		}
	}

	return nil
}
