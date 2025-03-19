package logs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

// Iter iterates over records in the provided decoder. All logs sections are
// iterated over in order.
func Iter(ctx context.Context, dec encoding.Decoder) result.Seq[Record] {
	return result.Iter(func(yield func(Record) bool) error {
		sections, err := dec.Sections(ctx)
		if err != nil {
			return err
		}

		logsDec := dec.LogsDecoder()

		for _, section := range sections {
			if section.Type != filemd.SECTION_TYPE_LOGS {
				continue
			}

			for result := range IterSection(ctx, logsDec, section) {
				if result.Err() != nil || !yield(result.MustValue()) {
					return result.Err()
				}
			}
		}

		return nil
	})
}

func IterSection(ctx context.Context, dec encoding.LogsDecoder, section *filemd.SectionInfo) result.Seq[Record] {
	return result.Iter(func(yield func(Record) bool) error {
		// We need to pull the columns twice: once from the dataset implementation
		// and once for the metadata to retrieve column type.
		//
		// TODO(rfratto): find a way to expose this information from
		// encoding.StreamsDataset to avoid the double call.
		streamsColumns, err := dec.Columns(ctx, section)
		if err != nil {
			return err
		}

		dset := encoding.LogsDataset(dec, section)

		columns, err := result.Collect(dset.ListColumns(ctx))
		if err != nil {
			return err
		}

		r := dataset.NewReader(dataset.ReaderOptions{
			Dataset: dset,
			Columns: columns,
		})

		var rows [1]dataset.Row
		for {
			n, err := r.Read(ctx, rows[:])
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			} else if n == 0 && errors.Is(err, io.EOF) {
				return nil
			}

			for _, row := range rows[:n] {
				record, err := Decode(streamsColumns, row)
				if err != nil || !yield(record) {
					return err
				}
			}
		}
	})
}

// Decode decodes a record from a [dataset.Row], using the provided columns to
// determine the column type. The list of columns must match the columns used
// to create the row.
func Decode(columns []*logsmd.ColumnDesc, row dataset.Row) (Record, error) {
	record := Record{
		// Preallocate metadata to exact number of metadata columns to avoid
		// oversizing.
		Metadata: make(labels.Labels, 0, metadataColumns(columns)),
	}

	for columnIndex, columnValue := range row.Values {
		if columnValue.IsNil() || columnValue.IsZero() {
			continue
		}

		column := columns[columnIndex]
		switch column.Type {
		case logsmd.COLUMN_TYPE_STREAM_ID:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_INT64 {
				return Record{}, fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			record.StreamID = columnValue.Int64()

		case logsmd.COLUMN_TYPE_TIMESTAMP:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_INT64 {
				return Record{}, fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			record.Timestamp = time.Unix(0, columnValue.Int64()).UTC()

		case logsmd.COLUMN_TYPE_METADATA:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_STRING {
				return Record{}, fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			record.Metadata = append(record.Metadata, labels.Label{
				Name:  column.Info.Name,
				Value: columnValue.String(),
			})

		case logsmd.COLUMN_TYPE_MESSAGE:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_BYTE_ARRAY {
				return Record{}, fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			record.Line = columnValue.ByteArray()
		}
	}

	// Metadata is originally sorted in received order; we sort it by key
	// per-record since it might not be obvious why keys appear in a certain
	// order.
	slices.SortFunc(record.Metadata, func(a, b labels.Label) int {
		if res := cmp.Compare(a.Name, b.Name); res != 0 {
			return res
		}
		return cmp.Compare(a.Value, b.Value)
	})

	return record, nil
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
