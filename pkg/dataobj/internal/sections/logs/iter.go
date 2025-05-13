package logs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
)

// Iter iterates over records in the provided decoder. All logs sections are
// iterated over in order.
// Results objects returned to yield may be reused and must be copied for further use via DeepCopy().
func Iter(ctx context.Context, dec encoding.Decoder) result.Seq[Record] {
	return result.Iter(func(yield func(Record) bool) error {
		sections, err := dec.Sections(ctx)
		if err != nil {
			return err
		}

		for _, section := range sections {
			if section.Kind != filemd.SECTION_KIND_LOGS {
				continue
			}

			for result := range IterSection(ctx, dec.LogsDecoder(section)) {
				if result.Err() != nil || !yield(result.MustValue()) {
					return result.Err()
				}
			}
		}

		return nil
	})
}

func IterSection(ctx context.Context, dec encoding.LogsDecoder) result.Seq[Record] {
	return result.Iter(func(yield func(Record) bool) error {
		// We need to pull the columns twice: once from the dataset implementation
		// and once for the metadata to retrieve column type.
		//
		// TODO(rfratto): find a way to expose this information from
		// encoding.StreamsDataset to avoid the double call.
		streamsColumns, err := dec.Columns(ctx)
		if err != nil {
			return err
		}

		dset := encoding.LogsDataset(dec)

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
				err := Decode(streamsColumns, row, &record)
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
func Decode(columns []*logsmd.ColumnDesc, row dataset.Row, record *Record) error {
	metadataColumns := metadataColumns(columns)
	record.Metadata = slicegrow.GrowToCap(record.Metadata, metadataColumns)
	record.Metadata = record.Metadata[:metadataColumns]
	nextMetadataIdx := 0

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

			// Convert the target pointer to a byte slice and grow it if necessary.
			target := slicegrow.Copy(record.Metadata[nextMetadataIdx].Value, columnValue.ByteArray())

			record.Metadata[nextMetadataIdx].Name = column.Info.Name
			record.Metadata[nextMetadataIdx].Value = target
			nextMetadataIdx++

		case logsmd.COLUMN_TYPE_MESSAGE:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_BYTE_ARRAY {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			line := columnValue.ByteArray()
			record.Line = slicegrow.Copy(record.Line, line)
		}
	}

	// Truncate the metadata slice to the number of metadata columns we found.
	record.Metadata = record.Metadata[:nextMetadataIdx]

	// Metadata is originally sorted in received order; we sort it by key
	// per-record since it might not be obvious why keys appear in a certain
	// order.
	slices.SortFunc(record.Metadata, func(a, b RecordMetadata) int {
		if res := strings.Compare(a.Name, b.Name); res != 0 {
			return res
		}
		return bytes.Compare(a.Value, b.Value)
	})

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
