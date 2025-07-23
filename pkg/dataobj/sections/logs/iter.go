package logs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/symbolizer"
	"github.com/grafana/loki/v3/pkg/util/labelpool"
)

// Iter iterates over records in the provided decoder. All logs sections are
// iterated over in order.
// Results objects returned to yield may be reused and must be copied for further use via DeepCopy().
func Iter(ctx context.Context, obj *dataobj.Object) result.Seq[Record] {
	return result.Iter(func(yield func(Record) bool) error {
		for i, section := range obj.Sections().Filter(CheckSection) {
			logsSection, err := Open(ctx, section)
			if err != nil {
				return fmt.Errorf("opening section %d: %w", i, err)
			}

			for result := range IterSection(ctx, logsSection) {
				if result.Err() != nil || !yield(result.MustValue()) {
					return result.Err()
				}
			}
		}

		return nil
	})
}

func IterSection(ctx context.Context, section *Section) result.Seq[Record] {
	return result.Iter(func(yield func(Record) bool) error {
		dec := newDecoder(section.reader)

		// We need to pull the columns twice: once from the dataset implementation
		// and once for the metadata to retrieve column type.
		//
		// TODO(rfratto): find a way to expose this information from
		// encoding.StreamsDataset to avoid the double call.
		streamsColumns, err := dec.Columns(ctx)
		if err != nil {
			return err
		}

		dset, err := newColumnsDataset(section.Columns())
		if err != nil {
			return fmt.Errorf("creating columns dataset: %w", err)
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

		var rows [1]dataset.Row
		var record Record
		for {
			n, err := r.Read(ctx, rows[:])
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			} else if n == 0 && errors.Is(err, io.EOF) {
				return nil
			}
			for _, row := range rows[:n] {
				err := decodeRow(streamsColumns, row, &record, nil)
				if err != nil || !yield(record) {
					return err
				}
			}
		}
	})
}

// decodeRow decodes a record from a [dataset.Row], using the provided columns
// to determine the column type. The list of columns must match the columns
// used to create the row.
//
// The sym argument is used for reusing metadata strings between calls to
// decodeRow. If sym is nil, metadata strings are always allocated.
func decodeRow(columns []*logsmd.ColumnDesc, row dataset.Row, record *Record, sym *symbolizer.Symbolizer) error {
	labelBuilder := labelpool.Get()
	defer labelpool.Put(labelBuilder)

	for columnIndex, columnValue := range row.Values {
		if columnValue.IsNil() || columnValue.IsZero() {
			continue
		}

		column := columns[columnIndex]
		switch column.Type {
		case logsmd.COLUMN_TYPE_STREAM_ID:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			record.StreamID = columnValue.Int64()

		case logsmd.COLUMN_TYPE_TIMESTAMP:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			record.Timestamp = time.Unix(0, columnValue.Int64())

		case logsmd.COLUMN_TYPE_METADATA:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_BYTE_ARRAY {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}

			if sym != nil {
				labelBuilder.Add(column.Info.Name, sym.Get(unsafeString(columnValue.ByteArray())))
			} else {
				labelBuilder.Add(column.Info.Name, string(columnValue.ByteArray()))
			}

		case logsmd.COLUMN_TYPE_MESSAGE:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_BYTE_ARRAY {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			line := columnValue.ByteArray()
			record.Line = slicegrow.Copy(record.Line, line)
		}
	}

	// Commit the final metadata to the record.
	labelBuilder.Sort()
	record.Metadata = labelBuilder.Labels()
	return nil
}

func metadataColumns(columns []*logsmd.ColumnDesc) int {
	var count int
	for _, column := range columns {
		if column.Type == logsmd.COLUMN_TYPE_METADATA {
			count++
		}
	}
	return count
}
