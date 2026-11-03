package columnar

import (
	"context"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/memory"
)

// ReaderAdapter reads [columnar.RecordBatch] values from a dataset. It applies
// predicates to whole columns with selection vectors and returns the arrays
// that the dataset decoders produce, without building rows.
type ReaderAdapter struct {
	inner *dataset.BatchReader
}

// NewReaderAdapter creates a ReaderAdapter with the provided dataset reader options.
func NewReaderAdapter(innerOpts dataset.RowReaderOptions) *ReaderAdapter {
	return &ReaderAdapter{inner: dataset.NewBatchReader(innerOpts)}
}

// Reset reinitializes the adapter with new reader options.
func (r *ReaderAdapter) Reset(opts dataset.RowReaderOptions) {
	r.inner.Reset(opts)
}

// Open initializes the underlying dataset reader.
func (r *ReaderAdapter) Open(ctx context.Context) error {
	return r.inner.Open(ctx)
}

// Close closes the underlying reader.
func (r *ReaderAdapter) Close() error {
	return r.inner.Close()
}

// Read reads up to batchSize rows from the underlying dataset reader and
// returns them as a [columnar.RecordBatch]. At the end of the dataset, Read
// returns an empty batch and [io.EOF].
func (r *ReaderAdapter) Read(ctx context.Context, alloc *memory.Allocator, batchSize int) (*columnar.RecordBatch, error) {
	return r.inner.Read(ctx, alloc, batchSize)
}
