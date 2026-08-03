package wirecodec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"

	"github.com/gogo/protobuf/proto"

	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/dataset/encoding/wirecodec/internal/inlinemd"
	"github.com/grafana/loki/v3/pkg/dataset/layout"
	"github.com/grafana/loki/v3/pkg/expr"
	"github.com/grafana/loki/v3/pkg/memory"
)

// MarshalInline marshals a layout and all of its data into a single byte slice.
// MarshalInline should only be used for very small data sets, as unmarshaling
// it requires reading the entire slice back into memory.
func MarshalInline(ctx context.Context, root layout.Layout, source buffer.Source) ([]byte, error) {
	var alloc memory.Allocator

	r, err := layout.NewReader(&alloc, root, source)
	if err != nil {
		return nil, fmt.Errorf("creating reader: %w", err)
	}
	defer r.Close()

	if _, err := r.Next(ctx, math.MaxInt); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("reading buffers: %w", err)
	}
	bufs, err := r.AppendBuffers(nil, nil, &expr.Identity{}, memory.Bitmap{})
	if err != nil {
		return nil, fmt.Errorf("appending buffers: %w", err)
	}

	s := inlineSink{
		// We start at maxBufferID + 1 so we don't overwrite any existing
		// buffers.
		nextID: maxBufferID(bufs) + 1,
	}

	md, err := marshalMetadata(ctx, root, &s)
	if err != nil {
		return nil, fmt.Errorf("marshaling layout: %w", err)
	}

	inlineBufs := make([]*inlinemd.Buffer, 0, len(bufs)+len(s.bufs))

	bufData, err := source.ReadBuffers(ctx, &alloc, bufs)
	if err != nil {
		return nil, fmt.Errorf("reading buffers: %w", err)
	}
	for i := range len(bufs) {
		inlineBufs = append(inlineBufs, &inlinemd.Buffer{
			Id:   uint64(bufs[i]),
			Data: []byte(bufData[i]),
		})
	}
	inlineBufs = append(inlineBufs, s.bufs...)

	return proto.Marshal(&inlinemd.Inline{Metadata: md, Buffers: inlineBufs})
}

type inlineSink struct {
	nextID uint64
	bufs   []*inlinemd.Buffer
}

func (s *inlineSink) WriteBuffers(_ context.Context, data []buffer.Data) ([]buffer.ID, error) {
	res := make([]buffer.ID, len(data))
	for i, bd := range data {
		id := s.nextID
		if id == 0 {
			id = 1
		}

		s.bufs = append(s.bufs, &inlinemd.Buffer{Id: id, Data: slices.Clone(bd)})
		res[i] = buffer.ID(id)

		s.nextID = id + 1
	}
	return res, nil
}

func maxBufferID(bufs []buffer.ID) uint64 {
	var maxID uint64
	for _, buf := range bufs {
		if uint64(buf) > maxID {
			maxID = uint64(buf)
		}
	}
	return maxID
}

// UnmarshalInline unmarshals an inlined layout. The returned [buffer.Source]
// refers to data within the input data slice; the caller must not modify the
// data slice after calling this function.
func UnmarshalInline(ctx context.Context, data []byte) (layout.Layout, buffer.Source, error) {
	if len(data) == 0 {
		return nil, nil, errors.New("cannot unmarshal empty inline dataset")
	}

	var inline inlinemd.Inline
	if err := proto.Unmarshal(data, &inline); err != nil {
		return nil, nil, err
	}

	src := inlineSource{
		buffers: make(map[uint64]*inlinemd.Buffer, len(inline.Buffers)),
	}
	for _, buf := range inline.Buffers {
		src.buffers[buf.Id] = buf
	}

	layout, err := unmarshalMetadata(ctx, inline.Metadata, &src)
	if err != nil {
		return nil, nil, fmt.Errorf("reading inline metadata: %w", err)
	}
	return layout, &src, nil
}

type inlineSource struct {
	buffers map[uint64]*inlinemd.Buffer
}

func (s *inlineSource) ReadBuffers(_ context.Context, _ *memory.Allocator, bufs []buffer.ID) ([]buffer.Data, error) {
	res := make([]buffer.Data, len(bufs))

	var errs []error
	for i, buf := range bufs {
		bufData, ok := s.buffers[uint64(buf)]
		if !ok {
			errs = append(errs, fmt.Errorf("buffer not found: %d", buf))
			continue
		}
		res[i] = bufData.Data
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return res, nil
}

func (s *inlineSource) BufferSizes(_ context.Context, _ *memory.Allocator, bufs []buffer.ID) ([]int64, error) {
	sizes := make([]int64, len(bufs))
	var errs []error
	for i, buf := range bufs {
		bufData, ok := s.buffers[uint64(buf)]
		if !ok {
			errs = append(errs, fmt.Errorf("buffer not found: %d", buf))
			continue
		}
		sizes[i] = int64(len(bufData.Data))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return sizes, nil
}
