package logs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"
	"unsafe"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	slicegrow "github.com/grafana/loki/v3/pkg/dataobj/internal/util"
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
	if record.Line == nil {
		record.Line = make([]byte, 0)
	}

	metadataColumns := metadataColumns(columns)
	record.Metadata = slicegrow.GrowToCap(record.Metadata, metadataColumns)
	record.Metadata = record.Metadata[:metadataColumns]
	record.MdValueCaps = slicegrow.GrowToCap(record.MdValueCaps, metadataColumns)
	record.MdValueCaps = record.MdValueCaps[:metadataColumns]
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
			record.Timestamp = time.Unix(0, columnValue.Int64()).UTC()

		case logsmd.COLUMN_TYPE_METADATA:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_STRING {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}

			// Convert the target pointer to a byte slice and grow it if necessary.
			target := unsafeSlice(record.Metadata[nextMetadataIdx].Value, record.MdValueCaps[nextMetadataIdx])
			target = slicegrow.CopyStringInto(target, columnValue.String())
			record.MdValueCaps[nextMetadataIdx] = max(record.MdValueCaps[nextMetadataIdx], len(target))

			record.Metadata[nextMetadataIdx].Name = column.Info.Name
			record.Metadata[nextMetadataIdx].Value = unsafeString(target)
			nextMetadataIdx++

		case logsmd.COLUMN_TYPE_MESSAGE:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_BYTE_ARRAY {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			line := columnValue.ByteArray()
			record.Line = slicegrow.CopyInto(record.Line, line)
		}
	}

	// Truncate the metadata slice to the number of metadata columns we found.
	record.Metadata = record.Metadata[:nextMetadataIdx]

	// Metadata is originally sorted in received order; we sort it by key
	// per-record since it might not be obvious why keys appear in a certain
	// order.
	slices.SortFunc(record.Metadata, func(a, b labels.Label) int {
		if res := cmp.Compare(a.Name, b.Name); res != 0 {
			return res
		}
		return cmp.Compare(a.Value, b.Value)
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

func unsafeSlice(data string, capacity int) []byte {
	if capacity <= 0 {
		capacity = len(data)
	}
	return unsafe.Slice(unsafe.StringData(data), capacity)
}

func unsafeString(data []byte) string {
	return unsafe.String(unsafe.SliceData(data), len(data))
}
