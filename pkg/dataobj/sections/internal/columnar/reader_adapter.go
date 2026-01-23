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
	inner    *dataset.Reader
	colTypes []datasetmd.PhysicalType

	buf []dataset.Row
}

// NewReaderAdapter creates a ReaderAdapter with the provided dataset reader options.
func NewReaderAdapter(innerOpts dataset.ReaderOptions) *ReaderAdapter {
	r := &ReaderAdapter{inner: dataset.NewReader(innerOpts)}
	r.Reset(innerOpts)
	return r
}

// Reset reinitializes the adapter with new reader options.
func (r *ReaderAdapter) Reset(opts dataset.ReaderOptions) {
	r.inner.Reset(opts)

	slicegrow.GrowToCap(r.colTypes, len(opts.Columns))
	r.colTypes = r.colTypes[:0]
	for _, col := range opts.Columns {
		r.colTypes = append(r.colTypes, col.ColumnDesc().Type.Physical)
	}
}

// Close closes the underlying reader.
func (r *ReaderAdapter) Close() error {
	return r.inner.Close()
}

// Read reads up to batchSize rows from the underlying dataset reader and
// returns them as a [columnar.RecordBatch].
func (r *ReaderAdapter) Read(ctx context.Context, alloc *memory.Allocator, batchSize int) (columnar.RecordBatch, error) {
	r.buf = slicegrow.GrowToCap(r.buf, batchSize)
	r.buf = r.buf[:batchSize]

	var arrBuilders []arrayBuilder
	n, readErr := r.inner.Read(ctx, r.buf)

	for _, colType := range r.colTypes {
		switch colType {
		case datasetmd.PHYSICAL_TYPE_UNSPECIFIED:
			return columnar.RecordBatch{}, fmt.Errorf("undefined physical type: %v", colType)
		case datasetmd.PHYSICAL_TYPE_INT64:
			arrBuilders = append(arrBuilders, newInt64ArrayBuilder(alloc, n))
		case datasetmd.PHYSICAL_TYPE_UINT64:
			arrBuilders = append(arrBuilders, newUint64ArrayBuilder(alloc, n))
		case datasetmd.PHYSICAL_TYPE_BINARY:
			arrBuilders = append(arrBuilders, newUTF8ArrayBuilder(alloc, n))
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
				return columnar.RecordBatch{}, fmt.Errorf("unsupported column type: %s", colType)
			case datasetmd.PHYSICAL_TYPE_INT64:
				builder.(*int64ArrayBuilder).Append(val.Int64())
			case datasetmd.PHYSICAL_TYPE_UINT64:
				builder.(*uint64ArrayBuilder).Append(val.Uint64())
			case datasetmd.PHYSICAL_TYPE_BINARY:
				builder.(*utf8ArrayBuilder).Append(val.Binary())
			}
		}
	}

	arrs := make([]columnar.Array, len(arrBuilders))
	for i, builder := range arrBuilders {
		arrs[i] = builder.Build()
	}

	// We only return readErr after processing n so that we properly handle n>0
	// while also getting an error such as io.EOF.
	return columnar.NewRecordBatch(int64(n), arrs), readErr
}

type arrayBuilder interface {
	Build() columnar.Array
	AppendNull()
}

type int64ArrayBuilder struct {
	buf      memory.Buffer[int64]
	validity memory.Bitmap
	alloc    *memory.Allocator
}

func newInt64ArrayBuilder(alloc *memory.Allocator, size int) *int64ArrayBuilder {
	return &int64ArrayBuilder{
		alloc:    alloc,
		buf:      memory.MakeBuffer[int64](alloc, size),
		validity: memory.MakeBitmap(alloc, size),
	}
}

func (b *int64ArrayBuilder) AppendNull() {
	// the value does not matter in this case
	b.buf.Append(0)
	b.validity.Append(false)
}

func (b *int64ArrayBuilder) Append(v int64) {
	b.buf.Append(v)
	b.validity.Append(true)
}

func (b *int64ArrayBuilder) Build() columnar.Array {
	return columnar.MakeInt64(b.buf.Data(), b.validity)
}

type uint64ArrayBuilder struct {
	buf      memory.Buffer[uint64]
	validity memory.Bitmap
	alloc    *memory.Allocator
}

func newUint64ArrayBuilder(alloc *memory.Allocator, size int) *uint64ArrayBuilder {
	return &uint64ArrayBuilder{
		alloc:    alloc,
		buf:      memory.MakeBuffer[uint64](alloc, size),
		validity: memory.MakeBitmap(alloc, size),
	}
}

func (b *uint64ArrayBuilder) AppendNull() {
	// the value does not matter in this case
	b.buf.Append(0)
	b.validity.Append(false)
}

func (b *uint64ArrayBuilder) Append(v uint64) {
	b.buf.Append(v)
	b.validity.Append(true)
}

func (b *uint64ArrayBuilder) Build() columnar.Array {
	return columnar.MakeUint64(b.buf.Data(), b.validity)
}

type utf8ArrayBuilder struct {
	alloc      *memory.Allocator
	offsetsBuf memory.Buffer[int32]
	valuesBuf  memory.Buffer[byte]
	validity   memory.Bitmap
	totalBytes int32
	count      int32
}

func newUTF8ArrayBuilder(alloc *memory.Allocator, size int) *utf8ArrayBuilder {
	b := &utf8ArrayBuilder{
		alloc:      alloc,
		offsetsBuf: memory.MakeBuffer[int32](alloc, size+1),
		// we don't know the size of the values buffer in advance
		// setting initial capacity to "size"  as an approximation
		valuesBuf:  memory.MakeBuffer[byte](alloc, size),
		validity:   memory.MakeBitmap(alloc, size),
		totalBytes: 0,
		count:      0,
	}
	// 0 offset is always 0
	b.offsetsBuf.Append(0)
	return b
}

func (b *utf8ArrayBuilder) Build() columnar.Array {
	return columnar.MakeUTF8(
		b.valuesBuf.Data()[:b.totalBytes],
		b.offsetsBuf.Data()[:b.count+1],
		b.validity,
	)
}

func (b *utf8ArrayBuilder) AppendNull() {
	b.validity.Append(false)
	b.offsetsBuf.Append(b.totalBytes)
	b.count++
}

func (b *utf8ArrayBuilder) Append(v []byte) {
	b.validity.Append(true)
	b.valuesBuf.Grow(len(v))
	b.valuesBuf.Append(v...)
	b.totalBytes += int32(len(v))
	b.offsetsBuf.Append(b.totalBytes)
	b.count++
}
