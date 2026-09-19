package encoding

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"sort"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/dataset/array"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/dataset/encoding/wirecodec"
	"github.com/grafana/loki/v3/pkg/dataset/layout"
	"github.com/grafana/loki/v3/pkg/expr"
	"github.com/grafana/loki/v3/pkg/memory"
)

const (
	bufferIndexIDColumnName     = "buffer_id"
	bufferIndexOffsetColumnName = "buffer_offset"
	bufferIndexLengthColumnName = "buffer_length"
)

var bufferIndexType = &types.Struct{
	Fields: []types.StructField{
		{Name: bufferIndexIDColumnName, Type: &types.Uint64{}},
		{Name: bufferIndexOffsetColumnName, Type: &types.Int64{}},
		{Name: bufferIndexLengthColumnName, Type: &types.Int64{}},
	},
}

var (
	bufferIndexBitpackedSpec     = &array.SpecBitpacked{BlockSize: 2048, Widths: &array.SpecPlain{}}
	bufferIndexSignedSpec        = &array.SpecZigZag{Data: bufferIndexBitpackedSpec}
	bufferIndexUnsignedDeltaSpec = &array.SpecDelta{Data: bufferIndexBitpackedSpec}
	bufferIndexSignedDeltaSpec   = &array.SpecDelta{Data: bufferIndexSignedSpec}
)

func bufferIndexColumnSpec(arraySpec array.Spec) layout.Spec {
	return &layout.SpecChunked{
		SizeHint: 512 * 1024,
		Chunk:    &layout.SpecArray{Spec: arraySpec},
	}
}

var bufferIndexSpec = &layout.SpecStruct{
	Fields: []layout.Spec{
		bufferIndexColumnSpec(bufferIndexUnsignedDeltaSpec),
		bufferIndexColumnSpec(bufferIndexSignedDeltaSpec),
		bufferIndexColumnSpec(bufferIndexSignedSpec),
	},
}

// BufferIndex maps buffer IDs to their offset and length within a contiguous
// byte range.
type BufferIndex struct {
	postings []BufferPosting // Sorted by BufferID for binary search in Lookup.
}

// BuildBufferIndex serializes postings into a buffer index. The provided
// allocator is used for temporary encoding allocations. BuildBufferIndex does
// not retain postings after it returns.
func BuildBufferIndex(ctx context.Context, alloc *memory.Allocator, postings []BufferPosting) ([]byte, error) {
	if err := validateBufferPostings(postings); err != nil {
		return nil, fmt.Errorf("validating buffer index: %w", err)
	}
	return marshalBufferIndex(ctx, alloc, postings)
}

func marshalBufferIndex(ctx context.Context, alloc *memory.Allocator, postings []BufferPosting) ([]byte, error) {
	store := &buffer.MemoryStore{}

	w, err := layout.NewWriter(alloc, store, bufferIndexSpec, bufferIndexType)
	if err != nil {
		return nil, fmt.Errorf("creating buffer index writer: %w", err)
	}

	if len(postings) > 0 {
		arr := buildBufferIndexColumnar(alloc, postings)
		if err := w.Append(ctx, arr); err != nil {
			return nil, fmt.Errorf("appending buffer index data: %w", err)
		}
	}

	root, err := w.Flush(ctx)
	if err != nil {
		return nil, fmt.Errorf("flushing buffer index: %w", err)
	}

	return wirecodec.MarshalInline(ctx, root, store)
}

func buildBufferIndexColumnar(alloc *memory.Allocator, postings []BufferPosting) *columnar.Struct {
	ids := columnar.NewNumberBuilder[uint64](alloc)
	offsets := columnar.NewNumberBuilder[int64](alloc)
	lengths := columnar.NewNumberBuilder[int64](alloc)

	ids.Grow(len(postings))
	offsets.Grow(len(postings))
	lengths.Grow(len(postings))

	for _, p := range postings {
		ids.AppendValue(uint64(p.BufferID))
		offsets.AppendValue(p.Offset)
		lengths.AppendValue(p.Length)
	}

	schema := columnar.NewSchema([]columnar.Column{
		{Name: bufferIndexIDColumnName},
		{Name: bufferIndexOffsetColumnName},
		{Name: bufferIndexLengthColumnName},
	})
	return columnar.NewStruct(schema, []columnar.Array{
		ids.Build(),
		offsets.Build(),
		lengths.Build(),
	}, len(postings), memory.Bitmap{})
}

// OpenBufferIndex deserializes a buffer index from bytes. The provided
// allocator is used for internal decoding; callers must ensure it remains
// valid for the duration of the call.
func OpenBufferIndex(ctx context.Context, alloc *memory.Allocator, data []byte) (*BufferIndex, error) {
	root, source, err := wirecodec.UnmarshalInline(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("unmarshaling buffer index: %w", err)
	}

	if err := validateBufferIndexType(root.DataType()); err != nil {
		return nil, fmt.Errorf("validating buffer index schema: %w", err)
	}

	tempAlloc := memory.NewAllocator(alloc)
	defer tempAlloc.Free()

	r, err := layout.NewReader(tempAlloc, root, source)
	if err != nil {
		return nil, fmt.Errorf("creating buffer index reader: %w", err)
	}
	defer r.Close()

	n, err := r.Next(ctx, math.MaxInt)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("reading buffer index: %w", err)
	}

	// Empty index — nothing to decode.
	if n == 0 {
		return &BufferIndex{}, nil
	}

	arr, err := r.Project(ctx, tempAlloc, &expr.Identity{}, memory.Bitmap{})
	if err != nil {
		return nil, fmt.Errorf("projecting buffer index: %w", err)
	}

	s, ok := arr.(*columnar.Struct)
	if !ok {
		return nil, fmt.Errorf("buffer index: expected struct, got %T", arr)
	}

	if s.NumFields() != 3 {
		return nil, fmt.Errorf("buffer index: expected 3 fields, got %d", s.NumFields())
	}
	ids, ok := s.Field(0).(*columnar.Number[uint64])
	if !ok {
		return nil, fmt.Errorf("buffer index: expected Uint64 buffer IDs, got %T", s.Field(0))
	}
	bufOffsets, ok := s.Field(1).(*columnar.Number[int64])
	if !ok {
		return nil, fmt.Errorf("buffer index: expected Int64 buffer offsets, got %T", s.Field(1))
	}
	bufLengths, ok := s.Field(2).(*columnar.Number[int64])
	if !ok {
		return nil, fmt.Errorf("buffer index: expected Int64 buffer lengths, got %T", s.Field(2))
	}

	postings := make([]BufferPosting, ids.Len())
	for i := range postings {
		postings[i] = BufferPosting{
			BufferID: buffer.ID(ids.Get(i)),
			Offset:   bufOffsets.Get(i),
			Length:   bufLengths.Get(i),
		}
	}
	slices.SortFunc(postings, compareBufferPostings)
	if err := validateBufferPostings(postings); err != nil {
		return nil, fmt.Errorf("validating buffer index: %w", err)
	}

	return &BufferIndex{postings: postings}, nil
}

// Lookup returns the posting for the given buffer ID.
func (idx *BufferIndex) Lookup(bufferID buffer.ID) (BufferPosting, bool) {
	i := sort.Search(len(idx.postings), func(i int) bool {
		return idx.postings[i].BufferID >= bufferID
	})
	if i < len(idx.postings) && idx.postings[i].BufferID == bufferID {
		return idx.postings[i], true
	}
	return BufferPosting{}, false
}

func validateBufferIndexType(typ types.Type) error {
	structType, ok := typ.(*types.Struct)
	if !ok {
		return fmt.Errorf("expected Struct, got %T", typ)
	}
	if structType.Nullable {
		return fmt.Errorf("expected non-nullable Struct")
	}
	if len(structType.Fields) != len(bufferIndexType.Fields) {
		return fmt.Errorf("expected %d fields, got %d", len(bufferIndexType.Fields), len(structType.Fields))
	}

	for i, field := range structType.Fields {
		expected := bufferIndexType.Fields[i]
		if field.Name != expected.Name {
			return fmt.Errorf("field %d: expected name %q, got %q", i, expected.Name, field.Name)
		}
		switch expected.Type.(type) {
		case *types.Uint64:
			actual, ok := field.Type.(*types.Uint64)
			if !ok || actual.Nullable {
				return fmt.Errorf("field %q: expected non-nullable Uint64, got %s", field.Name, field.Type)
			}
		case *types.Int64:
			actual, ok := field.Type.(*types.Int64)
			if !ok || actual.Nullable {
				return fmt.Errorf("field %q: expected non-nullable Int64, got %s", field.Name, field.Type)
			}
		}
	}
	return nil
}

func validateBufferPostings(postings []BufferPosting) error {
	for i, posting := range postings {
		if posting.BufferID == buffer.Invalid {
			return fmt.Errorf("posting %d: buffer ID must be non-zero", i)
		}
		if posting.Offset < 0 {
			return fmt.Errorf("posting %d: offset must be non-negative, got %d", i, posting.Offset)
		}
		if posting.Length < 0 {
			return fmt.Errorf("posting %d: length must be non-negative, got %d", i, posting.Length)
		}
		if posting.Offset > math.MaxInt64-posting.Length {
			return fmt.Errorf("posting %d: offset %d plus length %d overflows int64", i, posting.Offset, posting.Length)
		}
	}

	if slices.IsSortedFunc(postings, compareBufferPostings) {
		for i := 1; i < len(postings); i++ {
			if postings[i-1].BufferID == postings[i].BufferID {
				return fmt.Errorf("posting %d: duplicate buffer ID %d", i, postings[i].BufferID)
			}
		}
		return nil
	}

	seen := make(map[buffer.ID]struct{}, len(postings))
	for i, posting := range postings {
		if _, ok := seen[posting.BufferID]; ok {
			return fmt.Errorf("posting %d: duplicate buffer ID %d", i, posting.BufferID)
		}
		seen[posting.BufferID] = struct{}{}
	}
	return nil
}

func compareBufferPostings(a, b BufferPosting) int {
	if a.BufferID < b.BufferID {
		return -1
	}
	if a.BufferID > b.BufferID {
		return 1
	}
	return 0
}
