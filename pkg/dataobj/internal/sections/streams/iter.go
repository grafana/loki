package streams

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

// Iter iterates over streams in the provided decoder. All streams sections are
// iterated over in order.
func Iter(ctx context.Context, dec encoding.Decoder) result.Seq[Stream] {
	return result.Iter(func(yield func(Stream) bool) error {
		sections, err := dec.Sections(ctx)
		if err != nil {
			return err
		}

		streamsDec := dec.StreamsDecoder()

		for _, section := range sections {
			if section.Type != filemd.SECTION_TYPE_STREAMS {
				continue
			}

			for result := range iterSection(ctx, streamsDec, section) {
				if result.Err() != nil || !yield(result.MustValue()) {
					return result.Err()
				}
			}
		}

		return nil
	})
}

func iterSection(ctx context.Context, dec encoding.StreamsDecoder, section *filemd.SectionInfo) result.Seq[Stream] {
	return result.Iter(func(yield func(Stream) bool) error {
		// We need to pull the columns twice: once from the dataset implementation
		// and once for the metadata to retrieve column type.
		//
		// TODO(rfratto): find a way to expose this information from
		// encoding.StreamsDataset to avoid the double call.
		streamsColumns, err := dec.Columns(ctx, section)
		if err != nil {
			return err
		}

		dset := encoding.StreamsDataset(dec, section)

		columns, err := result.Collect(dset.ListColumns(ctx))
		if err != nil {
			return err
		}

		for result := range dataset.Iter(ctx, columns) {
			row, err := result.Value()
			if err != nil {
				return err
			}

			stream, err := decodeRow(streamsColumns, row)
			if err != nil {
				return err
			} else if !yield(stream) {
				return nil
			}
		}

		return nil
	})
}

func decodeRow(columns []*streamsmd.ColumnDesc, row dataset.Row) (Stream, error) {
	var stream Stream

	for columnIndex, columnValue := range row.Values {
		if columnValue.IsNil() || columnValue.IsZero() {
			continue
		}

		column := columns[columnIndex]
		switch column.Type {
		case streamsmd.COLUMN_TYPE_STREAM_ID:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_INT64 {
				return stream, fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			stream.ID = columnValue.Int64()

		case streamsmd.COLUMN_TYPE_MIN_TIMESTAMP:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_INT64 {
				return stream, fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			stream.MinTimestamp = time.Unix(0, columnValue.Int64()).UTC()

		case streamsmd.COLUMN_TYPE_MAX_TIMESTAMP:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_INT64 {
				return stream, fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			stream.MaxTimestamp = time.Unix(0, columnValue.Int64()).UTC()

		case streamsmd.COLUMN_TYPE_ROWS:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_INT64 {
				return stream, fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			stream.Rows = int(columnValue.Int64())

		case streamsmd.COLUMN_TYPE_LABEL:
			if ty := columnValue.Type(); ty != datasetmd.VALUE_TYPE_STRING {
				return stream, fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			stream.Labels = append(stream.Labels, labels.Label{
				Name:  column.Info.Name,
				Value: columnValue.String(),
			})

		default:
			// TODO(rfratto): We probably don't want to return an error on unexpected
			// columns because it breaks forward compatibility. Should we log
			// something here?
		}
	}

	return stream, nil
}
