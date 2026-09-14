package dataset

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/dataset/encoding"
	"github.com/grafana/loki/v3/pkg/dataset/encoding/wirecodec"
	"github.com/grafana/loki/v3/pkg/dataset/layout"
	"github.com/grafana/loki/v3/pkg/expr"
	"github.com/grafana/loki/v3/pkg/memory"
)

// Build serializes a [Dataset] into a [File] using the canonical encoding
// format. Build writes encoding metadata to store, and the returned File reads
// all dataset and metadata buffers lazily from store. Callers must keep store
// and those buffers available and unchanged for the lifetime of the File.
func Build(ctx context.Context, ds Dataset, store buffer.Store) (*File, error) {
	var alloc memory.Allocator

	dataBuffers, err := collectDataBuffers(ctx, &alloc, ds.Layout, store)
	if err != nil {
		return nil, fmt.Errorf("dataset.Build: %w", err)
	}

	tracker := &trackingSink{inner: store}
	manifest, err := wirecodec.Build(ctx, ds.Layout, tracker)
	if err != nil {
		return nil, fmt.Errorf("dataset.Build: building manifest: %w", err)
	}

	buffers := deduplicateBuffers(dataBuffers, tracker.written)
	sizes, err := store.BufferSizes(ctx, &alloc, buffers)
	if err != nil {
		return nil, fmt.Errorf("dataset.Build: getting buffer sizes: %w", err)
	}

	postings := buildBufferPostings(buffers, sizes)
	indexBytes, err := encoding.BuildBufferIndex(ctx, &alloc, postings)
	if err != nil {
		return nil, fmt.Errorf("dataset.Build: building buffer index: %w", err)
	}

	metadataBytes, err := manifest.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("dataset.Build: marshaling metadata: %w", err)
	}

	header, err := marshalFileHeader(fileHeader{metadata: metadataBytes, index: indexBytes})
	if err != nil {
		return nil, fmt.Errorf("dataset.Build: marshaling file header: %w", err)
	}

	file, err := newFile(header, postings, store)
	if err != nil {
		return nil, fmt.Errorf("dataset.Build: constructing file: %w", err)
	}
	return file, nil
}

func collectDataBuffers(ctx context.Context, alloc *memory.Allocator, root layout.Layout, source buffer.Source) ([]buffer.ID, error) {
	r, err := layout.NewReader(alloc, root, source)
	if err != nil {
		return nil, fmt.Errorf("creating reader: %w", err)
	}
	defer r.Close()

	if _, err := r.Next(ctx, math.MaxInt); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("reading layout: %w", err)
	}
	buffers, err := r.AppendBuffers(nil, nil, &expr.Identity{}, memory.Bitmap{})
	if err != nil {
		return nil, fmt.Errorf("collecting buffers: %w", err)
	}
	return buffers, nil
}

func deduplicateBuffers(groups ...[]buffer.ID) []buffer.ID {
	var size int
	for _, group := range groups {
		size += len(group)
	}

	var (
		result = make([]buffer.ID, 0, size)
		seen   = make(map[buffer.ID]struct{}, size)
	)

	for _, group := range groups {
		for _, id := range group {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			result = append(result, id)
		}
	}
	return result
}

func buildBufferPostings(buffers []buffer.ID, sizes []int64) []encoding.BufferPosting {
	var (
		postings = make([]encoding.BufferPosting, len(buffers))
		offset   int64
	)

	for i, id := range buffers {
		postings[i] = encoding.BufferPosting{
			BufferID: id,
			Offset:   offset,
			Length:   sizes[i],
		}
		offset += sizes[i]
	}
	return postings
}

// trackingSink records every buffer ID written to its inner sink. This includes
// intermediate layout metadata buffers that are not exposed by the manifest.
type trackingSink struct {
	inner   buffer.Sink
	written []buffer.ID
}

func (t *trackingSink) WriteBuffers(ctx context.Context, data []buffer.Data) ([]buffer.ID, error) {
	buffers, err := t.inner.WriteBuffers(ctx, data)
	if err != nil {
		return buffers, err
	}
	t.written = append(t.written, buffers...)
	return buffers, nil
}
