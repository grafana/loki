package pointers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
	"unsafe"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/symbolizer"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// Iter iterates over pointers in the provided decoder. All pointers sections are
// iterated over in order.
func Iter(ctx context.Context, obj *dataobj.Object) result.Seq[SectionPointer] {
	return result.Iter(func(yield func(SectionPointer) bool) error {
		for i, section := range obj.Sections().Filter(CheckSection) {
			pointersSection, err := Open(ctx, section)
			if err != nil {
				return fmt.Errorf("opening section %d: %w", i, err)
			}

			for result := range IterSection(ctx, pointersSection) {
				if result.Err() != nil || !yield(result.MustValue()) {
					return result.Err()
				}
			}
		}

		return nil
	})
}

func IterSection(ctx context.Context, section *Section) result.Seq[SectionPointer] {
	return result.Iter(func(yield func(SectionPointer) bool) error {
		columnarSection := section.inner
		dset, err := columnar.MakeDataset(columnarSection, columnarSection.Columns())
		if err != nil {
			return fmt.Errorf("creating columns dataset: %w", err)
		}

		columns, err := result.Collect(dset.ListColumns(ctx))
		if err != nil {
			return err
		}

		r := dataset.NewRowReader(dataset.ReaderOptions{
			Dataset:  dset,
			Columns:  columns,
			Prefetch: true,
		})
		defer r.Close()

		sym := symbolizer.New(128, 1024)

		var rows [1]dataset.Row
		for {
			n, err := r.Read(ctx, rows[:])
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			} else if n == 0 && errors.Is(err, io.EOF) {
				return nil
			}

			var pointer SectionPointer
			for _, row := range rows[:n] {
				if err := decodeRow(section.Columns(), row, &pointer, sym); err != nil {
					return err
				}

				if !yield(pointer) {
					return nil
				}
			}
		}
	})
}

// decodeRow decodes a stream from a [dataset.Row], using the provided columns to
// determine the column type. The list of columns must match the columns used
// to create the row.
//
// The sym argument is used for reusing label values between calls to
// decodeRow. If sym is nil, label value strings are always allocated.
func decodeRow(columns []*Column, row dataset.Row, pointer *SectionPointer, sym *symbolizer.Symbolizer) error {
	pointer.Reset()

	for columnIndex, columnValue := range row.Values {
		if columnValue.IsNil() || columnValue.IsZero() {
			continue
		}

		column := columns[columnIndex]
		switch column.Type {
		case ColumnTypePath:
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_BINARY {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			pointer.Path = sym.Get(unsafeString(columnValue.Binary()))

		case ColumnTypeSection:
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			pointer.Section = columnValue.Int64()

		case ColumnTypePointerKind:
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			switch columnValue.Int64() {
			case int64(PointerKindStreamIndex):
				pointer.PointerKind = PointerKindStreamIndex
			case int64(PointerKindColumnIndex):
				pointer.PointerKind = PointerKindColumnIndex
			default:
				return fmt.Errorf("invalid pointer kind %d", columnValue.Int64())
			}

		case ColumnTypeStreamID:
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			pointer.StreamID = columnValue.Int64()

		case ColumnTypeStreamIDRef:
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			pointer.StreamIDRef = columnValue.Int64()

		case ColumnTypeMinTimestamp:
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			pointer.StartTs = time.Unix(0, columnValue.Int64())

		case ColumnTypeMaxTimestamp:
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			pointer.EndTs = time.Unix(0, columnValue.Int64())

		case ColumnTypeRowCount:
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			pointer.LineCount = columnValue.Int64()

		case ColumnTypeUncompressedSize:
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			pointer.UncompressedSize = columnValue.Int64()

		case ColumnTypeColumnName:
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_BINARY {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			pointer.ColumnName = sym.Get(unsafeString(columnValue.Binary()))

		case ColumnTypeColumnIndex:
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			pointer.ColumnIndex = columnValue.Int64()

		case ColumnTypeValuesBloomFilter:
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_BINARY {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			filterBytes := columnValue.Binary()
			pointer.ValuesBloomFilter = slicegrow.GrowToCap(pointer.ValuesBloomFilter, len(filterBytes))
			pointer.ValuesBloomFilter = pointer.ValuesBloomFilter[:len(filterBytes)]
			copy(pointer.ValuesBloomFilter, filterBytes)

		default:
			// TODO(rfratto): We probably don't want to return an error on unexpected
			// columns because it breaks forward compatibility. Should we log
			// something here?
		}
	}

	return nil
}

func unsafeString(data []byte) string {
	return unsafe.String(unsafe.SliceData(data), len(data))
}
