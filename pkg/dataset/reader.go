package dataset

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/dataset/layout"
	"github.com/grafana/loki/v3/pkg/expr"
	"github.com/grafana/loki/v3/pkg/memory"
)

// A Reader reads rows from a [Dataset], applying filtering and projection.
//
// Callers must call [Reader.Open] before calling [Reader.Read].
type Reader struct {
	alloc  *memory.Allocator
	source buffer.Source
	opts   ReaderOptions

	opened    bool
	pruneMask memory.Bitmap
	inner     layout.Reader
	offset    int // Offset into the dataset.
}

// ReaderOptions configures a [Reader].
type ReaderOptions struct {
	// Dataset to read.
	Dataset Dataset

	// Projection is the expression describing which columns to materialize.
	Projection expr.Expression

	// Filter is the expression to filter rows by.
	Filter expr.Expression

	// Mask is an optional bitmap to start with for filtering. If defined, it
	// must be the same length as the dataset.
	Mask memory.Bitmap
}

// NewReader creates a [Reader] for reading rows from a [Dataset].
//
// NewReader returns an error if the provided Mask length does not match the
// dataset length.
func NewReader(alloc *memory.Allocator, source buffer.Source, options ReaderOptions) (*Reader, error) {
	if maskLen, datasetLen := options.Mask.Len(), options.Dataset.Layout.Len(); maskLen > 0 && maskLen != datasetLen {
		return nil, fmt.Errorf("mask length %d does not match dataset length %d", maskLen, datasetLen)
	}

	return &Reader{
		alloc:  alloc,
		source: source,
		opts:   options,

		pruneMask: options.Mask,
	}, nil
}

// Open prepares the reader for reading. It must be called before Read.
func (r *Reader) Open(ctx context.Context) error {
	fullMask, err := r.prune(ctx)
	if err != nil {
		return err
	}
	r.pruneMask = fullMask

	// TODO(rfratto): This is where we would do prefetching after pruning, then
	// we would give some kind of cached buffer.Source to the reader below.

	inner, err := layout.NewReader(r.alloc, r.opts.Dataset.Layout, r.source)
	if err != nil {
		return err
	}
	r.inner = inner
	r.opened = true
	return nil
}

func (r *Reader) prune(ctx context.Context) (memory.Bitmap, error) {
	pruneAlloc := memory.NewAllocator(r.alloc)
	defer pruneAlloc.Free()

	inner, err := layout.NewReader(pruneAlloc, r.opts.Dataset.Layout, r.source)
	if err != nil {
		return memory.Bitmap{}, err
	}
	defer inner.Close()

	rows, err := inner.Next(ctx, math.MaxInt32)
	if errors.Is(err, io.EOF) {
		return r.pruneMask, nil
	} else if err != nil {
		return memory.Bitmap{}, err
	} else if rows == 0 {
		return r.pruneMask, nil
	}

	// NOTE(rfratto): The prune mask survives for the lifetime of the reader so
	// we allocate it with r.alloc.
	return inner.Prune(ctx, r.alloc, r.opts.Filter, r.pruneMask)
}

// Read returns up to count rows of selected fields that match the reader's
// filter. At the end of the data, Read returns nil, io.EOF.
//
// If there was an error reading the Dataset, Read returns the error with no
// array.
//
// The returned Array is allocated with alloc and is bound to its lifetime.
func (r *Reader) Read(ctx context.Context, alloc *memory.Allocator, count int) (columnar.Array, error) {
	if !r.opened {
		return nil, fmt.Errorf("Reader must be opened before reading")
	}

	wantRows := count

	chunkAlloc := memory.NewAllocator(alloc)
	defer chunkAlloc.Free()

	var chunks []columnar.Array
	for wantRows > 0 {
		chunk, err := r.readNext(ctx, chunkAlloc, wantRows)
		if err != nil && errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}

		if chunk != nil {
			chunks = append(chunks, chunk)
			wantRows -= chunk.Len()
		}
	}

	if len(chunks) == 0 {
		return nil, io.EOF
	}
	return columnar.Concat(alloc, chunks)
}

func (r *Reader) readNext(ctx context.Context, alloc *memory.Allocator, count int) (columnar.Array, error) {
	rows, err := r.inner.Next(ctx, count)
	if err != nil {
		return nil, err
	}
	defer func() {
		r.offset += rows
	}()

	var mask memory.Bitmap
	if r.pruneMask.Len() > 0 {
		mask = *r.pruneMask.Slice(r.offset, r.offset+rows)
	}

	filterMask := mask
	if r.opts.Filter != nil {
		// The returned filterMask doesn't survive the lifetime of readNext, so we
		// use a temporary allocator.
		maskAlloc := memory.NewAllocator(alloc)
		defer maskAlloc.Free()

		filterMask, err = r.inner.Filter(ctx, maskAlloc, r.opts.Filter, mask)
		if err != nil {
			return nil, err
		}
	}
	if filterMask.Len() > 0 && filterMask.SetCount() == 0 {
		return nil, nil
	}

	projected, err := r.inner.Project(ctx, alloc, r.opts.Projection, filterMask)
	if err != nil {
		return nil, err
	}
	filtered, err := compute.Filter(alloc, projected, filterMask)
	if err != nil {
		return nil, fmt.Errorf("filtering projected data: %w", err)
	}
	return filtered.(columnar.Array), nil
}

// Close releases resources held by the Reader.
func (r *Reader) Close() error {
	if !r.opened {
		return nil
	}

	r.opened = false
	return r.inner.Close()
}
