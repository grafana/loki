package dataset

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/dataset/encoding"
	"github.com/grafana/loki/v3/pkg/dataset/encoding/wirecodec"
	"github.com/grafana/loki/v3/pkg/memory"
)

// Open reads a serialized dataset from r and returns the reconstructed
// [Dataset] along with a [buffer.Source] for reading its buffer data.
func Open(ctx context.Context, r encoding.RangeReader) (Dataset, buffer.Source, error) {
	header, bodyOffset, bodyLength, err := readFileHeader(ctx, r)
	if err != nil {
		return Dataset{}, nil, fmt.Errorf("dataset.Open: reading file header: %w", err)
	}

	var manifest wirecodec.Manifest
	if err := manifest.UnmarshalBinary(header.metadata); err != nil {
		return Dataset{}, nil, fmt.Errorf("dataset.Open: unmarshaling metadata: %w", err)
	}

	var alloc memory.Allocator
	index, err := encoding.OpenBufferIndex(ctx, &alloc, header.index)
	if err != nil {
		return Dataset{}, nil, fmt.Errorf("dataset.Open: opening buffer index: %w", err)
	}

	source := &fileSource{
		reader:     r,
		index:      index,
		bodyOffset: bodyOffset,
		bodyLength: bodyLength,
	}
	root, err := manifest.Load(ctx, source)
	if err != nil {
		return Dataset{}, nil, fmt.Errorf("dataset.Open: loading layout: %w", err)
	}

	return Dataset{
		Type:   root.DataType(),
		Layout: root,
	}, source, nil
}

// fileSource is a [buffer.Source] backed by a RangeReader and buffer index.
type fileSource struct {
	reader     encoding.RangeReader
	index      *encoding.BufferIndex
	bodyOffset int64
	bodyLength int64
}

func (s *fileSource) ReadBuffers(ctx context.Context, _ *memory.Allocator, buffers []buffer.ID) ([]buffer.Data, error) {
	var (
		result = make([]buffer.Data, len(buffers))
		errs   []error
	)

	for i, id := range buffers {
		posting, err := s.lookup(id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if posting.Length > int64(math.MaxInt) {
			errs = append(errs, fmt.Errorf("buffer %d is too large to read: %d bytes", id, posting.Length))
			continue
		}

		data := make([]byte, int(posting.Length))
		if err := readFullRange(ctx, s.reader, data, s.bodyOffset+posting.Offset); err != nil {
			errs = append(errs, fmt.Errorf("reading buffer %d: %w", id, err))
			continue
		}
		result[i] = data
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return result, nil
}

func (s *fileSource) BufferSizes(_ context.Context, _ *memory.Allocator, buffers []buffer.ID) ([]int64, error) {
	var (
		sizes = make([]int64, len(buffers))
		errs  []error
	)

	for i, id := range buffers {
		posting, err := s.lookup(id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		sizes[i] = posting.Length
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return sizes, nil
}

func (s *fileSource) lookup(id buffer.ID) (encoding.BufferPosting, error) {
	posting, ok := s.index.Lookup(id)
	if !ok {
		return encoding.BufferPosting{}, fmt.Errorf("buffer %d not found in index", id)
	}
	if posting.Offset < 0 || posting.Length < 0 || posting.Offset > s.bodyLength || posting.Length > s.bodyLength-posting.Offset {
		return encoding.BufferPosting{}, fmt.Errorf(
			"buffer %d range [%d, %d) exceeds body size %d",
			id,
			posting.Offset,
			posting.Offset+posting.Length,
			s.bodyLength,
		)
	}
	return posting, nil
}

var _ buffer.Source = (*fileSource)(nil)
