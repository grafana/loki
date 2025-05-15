package streams

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
	"unsafe"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
)

// Iter iterates over streams in the provided decoder. All streams sections are
// iterated over in order.
func Iter(ctx context.Context, dec encoding.Decoder) result.Seq[Stream] {
	return result.Iter(func(yield func(Stream) bool) error {
		metadata, err := dec.Metadata(ctx)
		if err != nil {
			return err
		}

		for _, section := range metadata.Sections {
			sectionReader := dec.SectionReader(metadata, section)

			typ, err := sectionReader.Type()
			if err != nil {
				return fmt.Errorf("getting section type: %w", err)
			} else if typ != encoding.SectionTypeStreams {
				continue
			}

			// We ignore the error here because we know the section is a streams
			// section.
			streamsDec, _ := encoding.NewStreamsDecoder(sectionReader)

			for result := range IterSection(ctx, streamsDec) {
				if result.Err() != nil || !yield(result.MustValue()) {
					return result.Err()
				}
			}
		}

		return nil
	})
}

func IterSection(ctx context.Context, dec encoding.StreamsDecoder) result.Seq[Stream] {
	return result.Iter(func(yield func(Stream) bool) error {
		// We need to pull the columns twice: once from the dataset implementation
		// and once for the metadata to retrieve column type.
		//
		// TODO(rfratto): find a way to expose this information from
		// encoding.StreamsDataset to avoid the double call.
		streamsColumns, err := dec.Columns(ctx)
		if err != nil {
			return err
		}

		dset := encoding.StreamsDataset(dec)

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
		for {
			n, err := r.Read(ctx, rows[:])
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			} else if n == 0 && errors.Is(err, io.EOF) {
				return nil
			}

			var stream Stream
			for _, row := range rows[:n] {
				if err := Decode(streamsColumns, row, &stream); err != nil {
					return err
				}

				if !yield(stream) {
					return nil
				}
			}
		}
	})
}

// Decode decodes a stream from a [dataset.Row], using the provided columns to
// determine the column type. The list of columns must match the columns used
// to create the row.
func Decode(columns []*streamsmd.ColumnDesc, row dataset.Row, stream *Stream) error {
	labelColumns := labelColumns(columns)
	stream.Labels = slicegrow.GrowToCap(stream.Labels, labelColumns)
	stream.Labels = stream.Labels[:labelColumns]
	stream.LbValueCaps = slicegrow.GrowToCap(stream.LbValueCaps, labelColumns)
	stream.LbValueCaps = stream.LbValueCaps[:labelColumns]
	nextLabelIdx := 0

	for columnIndex, columnValue := range row.Values {
		if columnValue.IsNil() || columnValue.IsZero() {
			continue
		}

		column := columns[columnIndex]
		switch column.Type {
		case streamsmd.COLUMN_TYPE_STREAM_ID:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			stream.ID = columnValue.Int64()

		case streamsmd.COLUMN_TYPE_MIN_TIMESTAMP:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			stream.MinTimestamp = time.Unix(0, columnValue.Int64())

		case streamsmd.COLUMN_TYPE_MAX_TIMESTAMP:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			stream.MaxTimestamp = time.Unix(0, columnValue.Int64())

		case streamsmd.COLUMN_TYPE_ROWS:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			stream.Rows = int(columnValue.Int64())

		case streamsmd.COLUMN_TYPE_UNCOMPRESSED_SIZE:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			stream.UncompressedSize = columnValue.Int64()

		case streamsmd.COLUMN_TYPE_LABEL:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_BYTE_ARRAY {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}

			// Convert the target pointer to a byte slice and grow it if necessary.
			target := unsafeSlice(stream.Labels[nextLabelIdx].Value, stream.LbValueCaps[nextLabelIdx])
			target = slicegrow.Copy(target, columnValue.ByteArray())
			stream.LbValueCaps[nextLabelIdx] = cap(target)

			stream.Labels[nextLabelIdx].Name = column.Info.Name
			stream.Labels[nextLabelIdx].Value = unsafeString(target)
			nextLabelIdx++

		default:
			// TODO(rfratto): We probably don't want to return an error on unexpected
			// columns because it breaks forward compatibility. Should we log
			// something here?
		}
	}

	stream.Labels = stream.Labels[:nextLabelIdx]

	return nil
}

func labelColumns(columns []*streamsmd.ColumnDesc) int {
	var count int
	for _, column := range columns {
		if column.Type == streamsmd.COLUMN_TYPE_LABEL {
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
