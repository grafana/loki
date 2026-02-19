package dataset

import (
	"context"
	"errors"
	"io"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/sliceclear"
	"github.com/grafana/loki/v3/pkg/memory"
)

// ReaderOptions configures how a [Reader] will read data.
type ReaderOptions struct {
	// Columns to read from the Dataset. It is invalid to provide a Column that
	// is not in Dataset.
	//
	// The set of Columns can include columns not used in Predicate; such columns
	// are considered non-predicate columns.
	Columns []Column

	// Predicates filter the data returned by a RowReader. Predicates are
	// optional; if nil, all rows from Columns are returned.
	//
	// Expressions in Predicate may only reference columns in Columns.
	// Holds a list of predicates that can be sequentially applied to the dataset.
	Predicates []Predicate

	// BatchSize is the number of rows to buffer in memory for future calls to Read.
	// A higher batch size will improve performance by reducing the number of calls to the decoders, but will also increase the memory usage.
	BatchSize int

	// Allocator is used to allocate memory for the Reader, retained between Read calls.
	// A reader must not be used after it's allocator is freed or reclaimed until Reset is called.
	Allocator *memory.Allocator
}

type Reader struct {
	opts  ReaderOptions
	ready bool

	pending          *columnar.RecordBatch
	pendingRow       int64
	pendingAllocator *memory.Allocator

	row int64

	columnReaders []*columnReader
	schema        *columnar.Schema
}

// NewReader creates a new dataset Reader with the given options.
func NewReader(opts ReaderOptions) *Reader {
	r := Reader{opts: opts}
	r.Reset(opts)
	return &r
}

// Reset resets the Reader with the given options so it is ready to be re-used.
func (r *Reader) Reset(opts ReaderOptions) {
	r.opts = opts
	for _, columnReader := range r.columnReaders {
		_ = columnReader.Close()
	}
	r.columnReaders = sliceclear.Clear(r.columnReaders)
	r.pending = nil
	r.pendingAllocator = memory.NewAllocator(r.opts.Allocator)
	r.row = 0

	r.ready = false
}

// Open initializes the Reader so it is ready to be used. Open must be called before Read.
func (r *Reader) Open(ctx context.Context) error {
	if r.ready {
		return nil
	}

	err := validateOpts(r.opts)
	if err != nil {
		return err
	}

	for _, column := range r.opts.Columns {
		r.columnReaders = append(r.columnReaders, newColumnReader(column))
	}

	// TODO: Split this into an output & predicate schema.
	columns := make([]columnar.Column, len(r.opts.Columns))
	for i, column := range r.opts.Columns {
		columns[i] = columnar.Column{
			Name: column.ColumnDesc().Type.Logical + "/" + column.ColumnDesc().Tag,
		}
	}
	r.schema = columnar.NewSchema(columns)

	r.ready = true
	return nil
}

func validateOpts(opts ReaderOptions) error {
	if opts.Allocator == nil {
		return errors.New("allocator is required")
	}
	if opts.BatchSize <= 0 {
		return errors.New("batch size must be greater than 0")
	}
	if len(opts.Columns) == 0 {
		return errors.New("at least one column is required")
	}

	return nil
}

// Read reads up to count rows from the dataset.
// Read may buffer up to opts.BatchSize rows in memory for future calls to Read.
func (r *Reader) Read(ctx context.Context, alloc *memory.Allocator, count int) (*columnar.RecordBatch, error) {
	if !r.ready {
		return nil, errors.New("reader not initialized")
	}

	if r.pending != nil && r.pendingRow+r.pending.NumRows() >= r.row+int64(count) {
		readCount := min(count, int(r.pending.NumRows()))
		// Drain the pending buffer to the output until it's empty.
		// We read pending until it is completely empty, which may result in a small read at the end of the batch.
		output := r.pending.Slice(int(r.row-r.pendingRow), int(r.row-r.pendingRow+int64(readCount)))
		r.row += int64(output.NumRows())
		return output, nil
	}

	// Pending buffer is now empty, refill it up to the batch size.
	// We can reclaim any previously used memory now that we've finished with our previous pending buffer.
	r.pendingAllocator.Reclaim()

	// We also create a temporary allocator for this Read which we can use to allocate any intermediate arrays.
	tempAlloc := memory.NewAllocator(r.pendingAllocator)
	defer tempAlloc.Free()

	// Check the first column to see how many rows are remaining. They should all be the same so this is OK.
	rowsRemaining := int64(r.columnReaders[0].column.ColumnDesc().RowsCount) - r.columnReaders[0].nextRow
	if rowsRemaining <= 0 {
		return nil, io.EOF
	}

	// Init selection vector to true (all rows selected)
	readSize := min(int64(r.opts.BatchSize), rowsRemaining)
	selectionVector := memory.NewBitmap(tempAlloc, int(readSize))
	selectionVector.AppendCount(true, int(readSize))

	// TODO: Implement selection vectors
	_ = applySelectionVector(tempAlloc, selectionVector)

	// The pending allocator is used here because the pending buffer is retained beyond the lifetime of the read.
	// Instead, it is tied to the lifetime of the Reader.
	arrs := memory.NewBuffer[columnar.Array](r.pendingAllocator, len(r.columnReaders))
	r.pendingRow = r.row
	for _, column := range r.columnReaders {
		arr, err := column.Read(ctx, r.pendingAllocator, count)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		arrs.Append(arr)
	}

	if arrs.Len() == 0 {
		return nil, io.EOF
	}

	if arrs.Get(0).Len() == 0 {
		return nil, io.EOF
	}

	r.pending = columnar.NewRecordBatch(r.schema, int64(arrs.Get(0).Len()), arrs.Data())

	readCount := min(arrs.Get(0).Len(), count)
	output := r.pending.Slice(0, int(readCount))

	r.row += int64(output.NumRows())
	return output, nil
}

func applySelectionVector(_ *memory.Allocator, selection memory.Bitmap) memory.Bitmap {
	return selection
}
