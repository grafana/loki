package streams

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
	"unsafe"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/symbolizer"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
	"github.com/grafana/loki/v3/pkg/util/labelpool"
)

// IterOption configures iteration behavior.
type IterOption func(*iterConfig)

type iterConfig struct {
	reuseLabelsBuffer bool
	symbolizer        *symbolizer.Symbolizer
}

// WithReuseLabelsBuffer enables label buffer reuse to reduce memory allocations.
// When enabled, Stream.Labels is overwritten on each iteration.
// WARNING: Callers must extract needed data (hash, specific label values)
// before continuing iteration - do not retain the Labels reference.
func WithReuseLabelsBuffer() IterOption {
	return func(c *iterConfig) {
		c.reuseLabelsBuffer = true
	}
}

// WithSymbolizer sets a symbolizer for deduplicating label value strings.
func WithSymbolizer(sym *symbolizer.Symbolizer) IterOption {
	return func(c *iterConfig) {
		c.symbolizer = sym
	}
}

// Iter iterates over streams in the provided decoder. All streams sections are
// iterated over in order.
func Iter(ctx context.Context, obj *dataobj.Object, opts ...IterOption) result.Seq[Stream] {
	var cfg iterConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	return result.Iter(func(yield func(Stream) bool) error {
		for i, section := range obj.Sections().Filter(CheckSection) {
			streamsSection, err := Open(ctx, section)
			if err != nil {
				return fmt.Errorf("opening section %d: %w", i, err)
			}

			for result := range iterSection(ctx, streamsSection, cfg) {
				if result.Err() != nil || !yield(result.MustValue()) {
					return result.Err()
				}
			}
		}

		return nil
	})
}

func IterSection(ctx context.Context, section *Section, opts ...IterOption) result.Seq[Stream] {
	var cfg iterConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	return iterSection(ctx, section, cfg)
}

func iterSection(ctx context.Context, section *Section, cfg iterConfig) result.Seq[Stream] {
	return result.Iter(func(yield func(Stream) bool) error {
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

		var rows [1024]dataset.Row
		labelBuilder := labels.NewScratchBuilder(8)

		for {
			n, err := r.Read(ctx, rows[:])
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			} else if n == 0 && errors.Is(err, io.EOF) {
				return nil
			}

			var stream Stream
			for _, row := range rows[:n] {
				labelBuilder.Reset()
				if err := decodeRow(section.Columns(), row, &stream, cfg.symbolizer, &labelBuilder, cfg.reuseLabelsBuffer); err != nil {
					return err
				}

				if !yield(stream) {
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
//
// The labelBuilder argument is used to reuse label builder between calls to decodeRow.
// If labelBuilder is nil, a builder is picked up from the pool and returned to the pool after use.
//
// The reuseLabelsBuffer argument controls whether the internal buffer used by the labelBuilder
// to build the labels is to be reused. Setting it to true would overwrite the previous labels
// built by the builder with the new ones.
//
// WARNING: When setting reuseLabelsBuffer, the caller should ideally pass
// a non-nil labelBuilder instance unless it does not care to read the labels.
func decodeRow(columns []*Column, row dataset.Row, stream *Stream, sym *symbolizer.Symbolizer, labelBuilder *labels.ScratchBuilder, reuseLabelsBuffer bool) error {
	if labelBuilder == nil {
		labelBuilder = labelpool.Get()
		defer labelpool.Put(labelBuilder)
	}

	for columnIndex, columnValue := range row.Values {
		if columnValue.IsNil() || columnValue.IsZero() {
			continue
		}

		column := columns[columnIndex]
		switch column.Type {
		case ColumnTypeStreamID:
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			stream.ID = columnValue.Int64()

		case ColumnTypeMinTimestamp:
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			stream.MinTimestamp = time.Unix(0, columnValue.Int64())

		case ColumnTypeMaxTimestamp:
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			stream.MaxTimestamp = time.Unix(0, columnValue.Int64())

		case ColumnTypeRows:
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			stream.Rows = int(columnValue.Int64())

		case ColumnTypeUncompressedSize:
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			stream.UncompressedSize = columnValue.Int64()

		case ColumnTypeLabel:
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_BINARY {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}

			if sym != nil {
				labelBuilder.Add(column.Name, sym.Get(unsafeString(columnValue.Binary())))
			} else {
				labelBuilder.Add(column.Name, string(columnValue.Binary()))
			}

		default:
			// TODO(rfratto): We probably don't want to return an error on unexpected
			// columns because it breaks forward compatibility. Should we log
			// something here?
		}
	}

	// Commit the final set of labels to the stream.
	labelBuilder.Sort()
	if reuseLabelsBuffer {
		labelBuilder.Overwrite(&stream.Labels)
	} else {
		stream.Labels = labelBuilder.Labels()
	}
	return nil
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
