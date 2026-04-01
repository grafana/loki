package columnar

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
	"github.com/grafana/loki/v3/pkg/memory"
)

// ReaderAdapter is a temporary translation layer that allows the caller to read
// [columnar.RecordBatch] values from a reader that only supports reads through
// a slice of [dataset.Row].
type ReaderAdapter struct {
	inner    *dataset.RowReader
	colTypes []datasetmd.PhysicalType

	buf []dataset.Row
}

// NewReaderAdapter creates a ReaderAdapter with the provided dataset reader options.
func NewReaderAdapter(innerOpts dataset.RowReaderOptions) *ReaderAdapter {
	r := &ReaderAdapter{inner: dataset.NewRowReader(innerOpts)}
	r.Reset(innerOpts)
	return r
}

// Reset reinitializes the adapter with new reader options.
func (r *ReaderAdapter) Reset(opts dataset.RowReaderOptions) {
	r.inner.Reset(opts)

	slicegrow.GrowToCap(r.colTypes, len(opts.Columns))
	r.colTypes = r.colTypes[:0]
	for _, col := range opts.Columns {
		r.colTypes = append(r.colTypes, col.ColumnDesc().Type.Physical)
	}
}

// Open initializes the underlying dataset row reader.
func (r *ReaderAdapter) Open(ctx context.Context) error {
	return r.inner.Open(ctx)
}

// Close closes the underlying reader.
func (r *ReaderAdapter) Close() error {
	return r.inner.Close()
}

// Read reads up to batchSize rows from the underlying dataset reader and
// returns them as a [columnar.RecordBatch].
func (r *ReaderAdapter) Read(ctx context.Context, alloc *memory.Allocator, batchSize int) (*columnar.RecordBatch, error) {
	r.buf = slicegrow.GrowToCap(r.buf, batchSize)
	r.buf = r.buf[:batchSize]

	var arrBuilders []columnar.Builder
	n, readErr := r.inner.Read(ctx, r.buf)

	for _, colType := range r.colTypes {
		switch colType {
		case datasetmd.PHYSICAL_TYPE_UNSPECIFIED:
			return nil, fmt.Errorf("undefined physical type: %v", colType)

		case datasetmd.PHYSICAL_TYPE_INT64:
			builder := columnar.NewNumberBuilder[int64](alloc)
			builder.Grow(n)
			arrBuilders = append(arrBuilders, builder)

		case datasetmd.PHYSICAL_TYPE_UINT64:
			builder := columnar.NewNumberBuilder[uint64](alloc)
			builder.Grow(n)
			arrBuilders = append(arrBuilders, builder)

		case datasetmd.PHYSICAL_TYPE_BINARY:
			builder := columnar.NewUTF8Builder(alloc)
			builder.Grow(n)
			arrBuilders = append(arrBuilders, builder)
		}
	}

	for rowIndex := range n {
		row := r.buf[rowIndex]

		for colIdx, val := range row.Values {
			colType := r.colTypes[colIdx]

			builder := arrBuilders[colIdx]
			if val.IsNil() {
				builder.AppendNull()
				continue
			}

			switch colType {
			case datasetmd.PHYSICAL_TYPE_UNSPECIFIED:
				return nil, fmt.Errorf("unsupported column type: %s", colType)
			case datasetmd.PHYSICAL_TYPE_INT64:
				builder.(*columnar.NumberBuilder[int64]).AppendValue(val.Int64())
			case datasetmd.PHYSICAL_TYPE_UINT64:
				builder.(*columnar.NumberBuilder[uint64]).AppendValue(val.Uint64())
			case datasetmd.PHYSICAL_TYPE_BINARY:
				builder.(*columnar.UTF8Builder).AppendValue(val.Binary())
			}
		}
	}

	arrs := make([]columnar.Array, len(arrBuilders))
	for i, builder := range arrBuilders {
		arrs[i] = builder.BuildArray()
	}

	// We only return readErr after processing n so that we properly handle n>0
	// while also getting an error such as io.EOF.
	return columnar.NewRecordBatch(nil, int64(n), arrs), readErr
}
