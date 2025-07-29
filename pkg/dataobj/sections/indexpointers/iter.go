package indexpointers

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
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/indexpointersmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/symbolizer"
)

// Iter iterates over indexpointers in the provided decoder. All indexpointers sections are
// iterated over in order.
func Iter(ctx context.Context, obj *dataobj.Object) result.Seq[IndexPointer] {
	return result.Iter(func(yield func(IndexPointer) bool) error {
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

func IterSection(ctx context.Context, section *Section) result.Seq[IndexPointer] {
	return result.Iter(func(yield func(IndexPointer) bool) error {
		dec := newDecoder(section.reader)

		// We need to pull the columns twice: once from the dataset implementation
		// and once for the metadata to retrieve column type.
		//
		// TODO(rfratto): find a way to expose this information from
		// encoding.StreamsDataset to avoid the double call.
		metadata, err := dec.Metadata(ctx)
		if err != nil {
			return err
		}

		dset, err := newColumnsDataset(section.Columns())
		if err != nil {
			return fmt.Errorf("creating section dataset: %w", err)
		}

		columns, err := result.Collect(dset.ListColumns(ctx))
		if err != nil {
			return err
		}

		r := dataset.NewReader(dataset.ReaderOptions{
			Dataset: dset,
			Columns: columns,
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

			var pointer IndexPointer
			for _, row := range rows[:n] {
				if err := decodeRow(metadata.GetColumns(), row, &pointer, sym); err != nil {
					return err
				}

				if !yield(pointer) {
					return nil
				}
			}
		}
	})
}

// decodeRow decodes an indexpointer from a [dataset.Row], using the provided columns to
// determine the column type. The list of columns must match the columns used
// to create the row.
//
// The sym argument is used for reusing label values between calls to
// decodeRow. If sym is nil, label value strings are always allocated.
func decodeRow(columns []*indexpointersmd.ColumnDesc, row dataset.Row, pointer *IndexPointer, sym *symbolizer.Symbolizer) error {
	for columnIndex, columnValue := range row.Values {
		column := columns[columnIndex]
		switch column.Type {
		case indexpointersmd.COLUMN_TYPE_PATH:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_BYTE_ARRAY {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}

			if columnValue.IsNil() || columnValue.IsZero() {
				return fmt.Errorf("nil or zero value for %s", column.Type)
			}

			if sym != nil {
				pointer.Path = sym.Get(unsafeString(columnValue.ByteArray()))
			} else {
				pointer.Path = string(columnValue.ByteArray())
			}

		case indexpointersmd.COLUMN_TYPE_MIN_TIMESTAMP:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}

			if columnValue.IsNil() || columnValue.IsZero() {
				return fmt.Errorf("nil or zero value for %s", column.Type)
			}

			pointer.StartTs = time.Unix(0, columnValue.Int64())

		case indexpointersmd.COLUMN_TYPE_MAX_TIMESTAMP:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}

			if columnValue.IsNil() || columnValue.IsZero() {
				return fmt.Errorf("nil or zero value for %s", column.Type)
			}

			pointer.EndTs = time.Unix(0, columnValue.Int64())

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
