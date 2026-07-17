package encoding

import (
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/dataset/array"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/dataset/encoding/wirecodec"
	"github.com/grafana/loki/v3/pkg/dataset/layout"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestBufferIndex(t *testing.T) {
	t.Run("round trip", func(t *testing.T) {
		var alloc memory.Allocator
		postings := []BufferPosting{
			{BufferID: 1, Offset: 0, Length: 100},
			{BufferID: buffer.ID(math.MaxUint64), Offset: 300, Length: 50},
			{BufferID: 2, Offset: 100, Length: 200},
		}

		data, err := BuildBufferIndex(t.Context(), &alloc, postings)
		require.NoError(t, err)
		require.NotEmpty(t, data)

		idx, err := OpenBufferIndex(t.Context(), &alloc, data)
		require.NoError(t, err)
		for _, expected := range postings {
			actual, ok := idx.Lookup(expected.BufferID)
			require.True(t, ok)
			require.Equal(t, expected, actual)
		}
		_, ok := idx.Lookup(999)
		require.False(t, ok)
	})

	t.Run("empty", func(t *testing.T) {
		var alloc memory.Allocator

		data, err := BuildBufferIndex(t.Context(), &alloc, nil)
		require.NoError(t, err)

		idx, err := OpenBufferIndex(t.Context(), &alloc, data)
		require.NoError(t, err)
		_, ok := idx.Lookup(1)
		require.False(t, ok)
	})
}

func TestBufferIndex_RejectsInvalidPostings(t *testing.T) {
	tests := []struct {
		name     string
		postings []BufferPosting
		err      string
	}{
		{
			name:     "invalid buffer ID",
			postings: []BufferPosting{{BufferID: buffer.Invalid}},
			err:      "buffer ID must be non-zero",
		},
		{
			name:     "negative offset",
			postings: []BufferPosting{{BufferID: 1, Offset: -1}},
			err:      "offset must be non-negative",
		},
		{
			name:     "negative length",
			postings: []BufferPosting{{BufferID: 1, Length: -1}},
			err:      "length must be non-negative",
		},
		{
			name:     "overflowing range",
			postings: []BufferPosting{{BufferID: 1, Offset: math.MaxInt64, Length: 1}},
			err:      "overflows int64",
		},
		{
			name: "duplicate buffer ID",
			postings: []BufferPosting{
				{BufferID: 1},
				{BufferID: 2},
				{BufferID: 1},
			},
			err: "duplicate buffer ID 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("Build", func(t *testing.T) {
				var alloc memory.Allocator

				_, err := BuildBufferIndex(t.Context(), &alloc, tt.postings)
				require.ErrorContains(t, err, tt.err)
			})

			t.Run("Open", func(t *testing.T) {
				var alloc memory.Allocator
				data, err := marshalBufferIndex(t.Context(), &alloc, tt.postings)
				require.NoError(t, err)

				_, err = OpenBufferIndex(t.Context(), &alloc, data)
				require.ErrorContains(t, err, tt.err)
			})
		})
	}
}

func TestOpenBufferIndex_RejectsInvalidSchema(t *testing.T) {
	plain := func() layout.Spec {
		return &layout.SpecArray{Spec: &array.SpecPlain{}}
	}

	tests := []struct {
		name string
		typ  types.Type
		spec layout.Spec
		arr  columnar.Array
		err  string
	}{
		{
			name: "non-struct",
			typ:  &types.Int64{},
			spec: plain(),
			arr:  columnar.NewNumber([]int64{1}, memory.Bitmap{}),
			err:  "expected Struct",
		},
		{
			name: "missing fields",
			typ: &types.Struct{Fields: []types.StructField{
				{Name: bufferIndexIDColumnName, Type: &types.Uint64{}},
			}},
			spec: &layout.SpecStruct{Fields: []layout.Spec{plain()}},
			arr: columnar.NewStruct(
				columnar.NewSchema([]columnar.Column{{Name: bufferIndexIDColumnName}}),
				[]columnar.Array{columnar.NewNumber([]uint64{1}, memory.Bitmap{})},
				1,
				memory.Bitmap{},
			),
			err: "expected 3 fields, got 1",
		},
		{
			name: "wrong buffer ID type",
			typ: &types.Struct{Fields: []types.StructField{
				{Name: bufferIndexIDColumnName, Type: &types.Int64{}},
				{Name: bufferIndexOffsetColumnName, Type: &types.Int64{}},
				{Name: bufferIndexLengthColumnName, Type: &types.Int64{}},
			}},
			spec: &layout.SpecStruct{Fields: []layout.Spec{plain(), plain(), plain()}},
			arr: columnar.NewStruct(
				columnar.NewSchema([]columnar.Column{
					{Name: bufferIndexIDColumnName},
					{Name: bufferIndexOffsetColumnName},
					{Name: bufferIndexLengthColumnName},
				}),
				[]columnar.Array{
					columnar.NewNumber([]int64{1}, memory.Bitmap{}),
					columnar.NewNumber([]int64{0}, memory.Bitmap{}),
					columnar.NewNumber([]int64{1}, memory.Bitmap{}),
				},
				1,
				memory.Bitmap{},
			),
			err: "expected non-nullable Uint64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := marshalInline(t, tt.typ, tt.spec, tt.arr)
			var alloc memory.Allocator

			_, err := OpenBufferIndex(t.Context(), &alloc, data)
			require.ErrorContains(t, err, tt.err)
		})
	}
}

func BenchmarkBufferIndex(b *testing.B) {
	const numPostings = 1_000_000

	postings := makeBufferPostings(numPostings)
	var alloc memory.Allocator
	encoded, err := BuildBufferIndex(b.Context(), &alloc, postings)
	require.NoError(b, err)
	b.Logf("encoded size: %d bytes (%.2f MB) for %d postings", len(encoded), float64(len(encoded))/(1024*1024), numPostings)
	b.Logf("%.1f bytes/posting", float64(len(encoded))/numPostings)

	b.Run(fmt.Sprintf("Build/%d", numPostings), func(b *testing.B) {
		var alloc memory.Allocator
		b.ReportAllocs()

		for b.Loop() {
			alloc.Reset()
			if _, err := BuildBufferIndex(b.Context(), &alloc, postings); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run(fmt.Sprintf("Open/%d", numPostings), func(b *testing.B) {
		var alloc memory.Allocator
		b.ReportAllocs()

		for b.Loop() {
			alloc.Reset()
			if _, err := OpenBufferIndex(b.Context(), &alloc, encoded); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func makeBufferPostings(count int) []BufferPosting {
	rng := rand.New(rand.NewSource(42))
	postings := make([]BufferPosting, count)
	var offset int64
	for i := range count {
		var length int64
		if rng.Float64() < 0.7 {
			length = rng.Int63n(50*1024) + 100
		} else {
			length = rng.Int63n(768*1024) + 256*1024
		}
		postings[i] = BufferPosting{
			BufferID: buffer.ID(i + 1),
			Offset:   offset,
			Length:   length,
		}
		offset += length
	}
	return postings
}

func marshalInline(t *testing.T, typ types.Type, spec layout.Spec, arr columnar.Array) []byte {
	t.Helper()

	var alloc memory.Allocator
	store := &buffer.MemoryStore{}
	w, err := layout.NewWriter(&alloc, store, spec, typ)
	require.NoError(t, err)
	require.NoError(t, w.Append(t.Context(), arr))
	root, err := w.Flush(t.Context())
	require.NoError(t, err)
	data, err := wirecodec.MarshalInline(t.Context(), root, store)
	require.NoError(t, err)
	return data
}
