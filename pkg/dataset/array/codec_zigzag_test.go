package array_test

import (
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/dataset/array"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestZigZagCodec_Validation(t *testing.T) {
	data := &array.SpecPlain{}

	tt := []struct {
		name string
		spec array.Spec
		typ  types.Type

		expectError bool
	}{
		{
			name:        "accepts non-nullable int32",
			spec:        &array.SpecZigZag{Data: data},
			typ:         &types.Int32{Nullable: false},
			expectError: false,
		},
		{
			name:        "accepts non-nullable int64",
			spec:        &array.SpecZigZag{Data: data},
			typ:         &types.Int64{Nullable: false},
			expectError: false,
		},
		{
			name:        "accepts nullable int64 with validity in data spec",
			spec:        &array.SpecZigZag{Data: &array.SpecPlain{Validity: &array.SpecBool{}}},
			typ:         &types.Int64{Nullable: true},
			expectError: false,
		},
		{
			name:        "rejects uint32",
			spec:        &array.SpecZigZag{Data: data},
			typ:         &types.Uint32{Nullable: false},
			expectError: true,
		},
		{
			name:        "rejects uint64",
			spec:        &array.SpecZigZag{Data: data},
			typ:         &types.Uint64{Nullable: false},
			expectError: true,
		},
		{
			name:        "rejects bool",
			spec:        &array.SpecZigZag{Data: data},
			typ:         &types.Bool{Nullable: false},
			expectError: true,
		},
		{
			name:        "rejects utf8",
			spec:        &array.SpecZigZag{Data: data},
			typ:         &types.UTF8{Nullable: false},
			expectError: true,
		},
		{
			name:        "rejects nil data spec",
			spec:        &array.SpecZigZag{Data: nil},
			typ:         &types.Int64{Nullable: false},
			expectError: true,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			var alloc memory.Allocator
			_, err := array.NewWriter(&alloc, tc.spec, tc.typ)

			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestZigZagCodec_NonNullable(t *testing.T) {
	t.Run("int32", func(t *testing.T) {
		var alloc memory.Allocator
		var store buffer.MemoryStore

		var (
			spec = &array.SpecZigZag{Data: &array.SpecPlain{}}
			typ  = &types.Int32{Nullable: false}
		)

		w, err := array.NewWriter(&alloc, spec, typ)
		require.NoError(t, err)

		input := []columnar.Array{
			columnartest.Array(t, types.KindInt32, &alloc, int32(-3), int32(-2), int32(-1), int32(0), int32(1)),
			columnartest.Array(t, types.KindInt32, &alloc, int32(2), int32(3), int32(100), int32(-100)),
		}
		for _, arr := range input {
			require.NoError(t, w.Append(arr))
		}

		result, err := w.Flush(t.Context(), &store)
		require.NoError(t, err)
		require.Equal(t, 9, result.RowCount)
		require.Equal(t, 0, result.Stats.NullCount)

		r, err := array.NewReader(&alloc, result, &store)
		require.NoError(t, err)

		expect := columnartest.Array(
			t, types.KindInt32, &alloc,
			int32(-3), int32(-2), int32(-1), int32(0), int32(1),
			int32(2), int32(3), int32(100), int32(-100),
		)

		actual := readBatches(t, &alloc, r, 2) // Read in a small batch size to test reading multiple times
		columnartest.RequireArraysEqual(t, expect, actual, memory.Bitmap{})

		_, err = r.Read(t.Context(), &alloc, 1)
		require.ErrorIs(t, err, io.EOF)
	})

	t.Run("int64", func(t *testing.T) {
		var alloc memory.Allocator
		var store buffer.MemoryStore

		var (
			spec = &array.SpecZigZag{Data: &array.SpecPlain{}}
			typ  = &types.Int64{Nullable: false}
		)

		w, err := array.NewWriter(&alloc, spec, typ)
		require.NoError(t, err)

		input := []columnar.Array{
			columnartest.Array(t, types.KindInt64, &alloc, int64(-3), int64(-2), int64(-1), int64(0), int64(1)),
			columnartest.Array(t, types.KindInt64, &alloc, int64(2), int64(3), int64(100), int64(-100)),
		}
		for _, arr := range input {
			require.NoError(t, w.Append(arr))
		}

		result, err := w.Flush(t.Context(), &store)
		require.NoError(t, err)
		require.Equal(t, 9, result.RowCount)
		require.Equal(t, 0, result.Stats.NullCount)

		r, err := array.NewReader(&alloc, result, &store)
		require.NoError(t, err)

		expect := columnartest.Array(
			t, types.KindInt64, &alloc,
			int64(-3), int64(-2), int64(-1), int64(0), int64(1),
			int64(2), int64(3), int64(100), int64(-100),
		)

		actual := readBatches(t, &alloc, r, 2) // Read in a small batch size to test reading multiple times
		columnartest.RequireArraysEqual(t, expect, actual, memory.Bitmap{})

		_, err = r.Read(t.Context(), &alloc, 1)
		require.ErrorIs(t, err, io.EOF)
	})
}

func TestZigZagCodec_Nullable(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	var (
		spec = &array.SpecZigZag{Data: &array.SpecPlain{Validity: &array.SpecBool{}}}
		typ  = &types.Int64{Nullable: true}
	)

	w, err := array.NewWriter(&alloc, spec, typ)
	require.NoError(t, err)

	input := []columnar.Array{
		columnartest.Array(t, types.KindInt64, &alloc, int64(-1), nil, int64(1), nil, int64(0)),
		columnartest.Array(t, types.KindInt64, &alloc, int64(42), int64(-42)),
		columnartest.Array(t, types.KindInt64, &alloc, nil),
	}
	for _, arr := range input {
		require.NoError(t, w.Append(arr))
	}

	result, err := w.Flush(t.Context(), &store)
	require.NoError(t, err)
	require.Equal(t, 8, result.RowCount)
	require.Equal(t, 3, result.Stats.NullCount)

	r, err := array.NewReader(&alloc, result, &store)
	require.NoError(t, err)

	expect := columnartest.Array(
		t, types.KindInt64, &alloc,
		int64(-1), nil, int64(1), nil, int64(0),
		int64(42), int64(-42),
		nil,
	)

	actual := readBatches(t, &alloc, r, 2) // Read in a small batch size to test reading multiple times
	columnartest.RequireArraysEqual(t, expect, actual, memory.Bitmap{})

	_, err = r.Read(t.Context(), &alloc, 1)
	require.ErrorIs(t, err, io.EOF)
}

func TestZigZagCodec_PartialRead(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	var (
		spec = &array.SpecZigZag{Data: &array.SpecPlain{}}
		typ  = &types.Int64{Nullable: false}
	)

	w, err := array.NewWriter(&alloc, spec, typ)
	require.NoError(t, err)

	input := columnartest.Array(t, types.KindInt64, &alloc,
		int64(-5), int64(-4), int64(-3), int64(-2), int64(-1),
		int64(0), int64(1), int64(2), int64(3), int64(4),
	)
	require.NoError(t, w.Append(input))

	result, err := w.Flush(t.Context(), &store)
	require.NoError(t, err)

	r, err := array.NewReader(&alloc, result, &store)
	require.NoError(t, err)

	batch1, err := r.Read(t.Context(), &alloc, 3)
	require.NoError(t, err)
	require.Equal(t, 3, batch1.Len())
	columnartest.RequireArraysEqual(t,
		columnartest.Array(t, types.KindInt64, &alloc, int64(-5), int64(-4), int64(-3)),
		batch1, memory.Bitmap{},
	)

	batch2, err := r.Read(t.Context(), &alloc, 3)
	require.NoError(t, err)
	require.Equal(t, 3, batch2.Len())
	columnartest.RequireArraysEqual(t,
		columnartest.Array(t, types.KindInt64, &alloc, int64(-2), int64(-1), int64(0)),
		batch2, memory.Bitmap{},
	)

	batch3, err := r.Read(t.Context(), &alloc, 3)
	require.NoError(t, err)
	require.Equal(t, 3, batch3.Len())
	columnartest.RequireArraysEqual(t,
		columnartest.Array(t, types.KindInt64, &alloc, int64(1), int64(2), int64(3)),
		batch3, memory.Bitmap{},
	)

	// Last batch: only 1 remaining.
	batch4, err := r.Read(t.Context(), &alloc, 3)
	require.NoError(t, err)
	require.Equal(t, 1, batch4.Len())
	columnartest.RequireArraysEqual(t,
		columnartest.Array(t, types.KindInt64, &alloc, int64(4)),
		batch4, memory.Bitmap{},
	)

	_, err = r.Read(t.Context(), &alloc, 1)
	require.ErrorIs(t, err, io.EOF)
}

func TestZigZagCodec_Extremes(t *testing.T) {
	t.Run("int32", func(t *testing.T) {
		var alloc memory.Allocator
		var store buffer.MemoryStore

		var (
			spec = &array.SpecZigZag{Data: &array.SpecPlain{}}
			typ  = &types.Int32{Nullable: false}
		)

		w, err := array.NewWriter(&alloc, spec, typ)
		require.NoError(t, err)

		input := columnartest.Array(t, types.KindInt32, &alloc,
			int32(0), int32(math.MinInt32), int32(math.MaxInt32), int32(-1), int32(1),
		)
		require.NoError(t, w.Append(input))

		result, err := w.Flush(t.Context(), &store)
		require.NoError(t, err)

		r, err := array.NewReader(&alloc, result, &store)
		require.NoError(t, err)

		actual := readBatches(t, &alloc, r, 2) // Read in a small batch size to test reading multiple times
		columnartest.RequireArraysEqual(t, input, actual, memory.Bitmap{})
	})

	t.Run("int64", func(t *testing.T) {
		var alloc memory.Allocator
		var store buffer.MemoryStore

		var (
			spec = &array.SpecZigZag{Data: &array.SpecPlain{}}
			typ  = &types.Int64{Nullable: false}
		)

		w, err := array.NewWriter(&alloc, spec, typ)
		require.NoError(t, err)

		input := columnartest.Array(t, types.KindInt64, &alloc,
			int64(0), int64(math.MinInt64), int64(math.MaxInt64), int64(-1), int64(1),
		)
		require.NoError(t, w.Append(input))

		result, err := w.Flush(t.Context(), &store)
		require.NoError(t, err)

		r, err := array.NewReader(&alloc, result, &store)
		require.NoError(t, err)

		actual := readBatches(t, &alloc, r, 2) // Read in a small batch size to test reading multiple times
		columnartest.RequireArraysEqual(t, input, actual, memory.Bitmap{})
	})
}

func TestZigZagCodec_WithBitpacked(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	var (
		spec = &array.SpecZigZag{
			Data: &array.SpecBitpacked{BlockSize: 8, Widths: &array.SpecPlain{}},
		}
		typ = &types.Int64{Nullable: false}
	)

	w, err := array.NewWriter(&alloc, spec, typ)
	require.NoError(t, err)

	input := columnartest.Array(t, types.KindInt64, &alloc,
		int64(0), int64(-1), int64(1), int64(-2), int64(2),
		int64(-3), int64(3), int64(-4), int64(4), int64(-5),
	)
	require.NoError(t, w.Append(input))

	result, err := w.Flush(t.Context(), &store)
	require.NoError(t, err)
	require.Equal(t, 10, result.RowCount)

	r, err := array.NewReader(&alloc, result, &store)
	require.NoError(t, err)

	actual := readBatches(t, &alloc, r, 2) // Read in a small batch size to test reading multiple times
	columnartest.RequireArraysEqual(t, input, actual, memory.Bitmap{})
}

func BenchmarkZigZagCodec(b *testing.B) {
	var (
		store buffer.MemoryStore

		spec = &array.SpecZigZag{Data: &array.SpecPlain{}}
		typ  = &types.Int64{Nullable: false}
	)

	const valuesPerPage = 1 << 16

	type scenario struct {
		name       string
		valueCount int
		encoded    array.Array
	}

	build := func(name string, valueCount int, valueAt func(i int) int64) scenario {
		var alloc memory.Allocator
		w, err := array.NewWriter(&alloc, spec, typ)
		require.NoError(b, err)

		builder := columnar.NewNumberBuilder[int64](&alloc)
		builder.Grow(valueCount)

		for i := range valueCount {
			builder.AppendValue(valueAt(i))
		}
		require.NoError(b, w.Append(builder.Build()))

		arr, err := w.Flush(b.Context(), &store)
		require.NoError(b, err)
		return scenario{name: name, valueCount: valueCount, encoded: arr}
	}

	scenarios := []scenario{
		build("variance=constant", valuesPerPage, func(int) int64 { return -42 }),
		build("variance=small_range", valuesPerPage, func(i int) int64 { return int64(i%16) - 8 }),
		func() scenario {
			rnd := rand.New(rand.NewSource(0))
			return build("variance=random", valuesPerPage, func(int) int64 {
				return rnd.Int63() - rnd.Int63()
			})
		}(),
	}

	batchSizes := []int{256, 1024, 4096}

	for _, sc := range scenarios {
		b.Run(sc.name, func(b *testing.B) {
			b.Run(fmt.Sprintf("values_per_page=%d", sc.valueCount), func(b *testing.B) {
				for _, batchSize := range batchSizes {
					b.Run(fmt.Sprintf("batch_size=%d", batchSize), func(b *testing.B) {
						var alloc memory.Allocator

						b.ReportAllocs()
						decodedBytesPerOp := int64(sc.valueCount) * 8
						b.SetBytes(decodedBytesPerOp)

						for b.Loop() {
							alloc.Reset()

							r, _ := array.NewReader(&alloc, sc.encoded, &store)

							var decoded int
							for {
								arr, err := r.Read(b.Context(), &alloc, batchSize)
								if arr != nil {
									decoded += arr.Len()
								}

								if errors.Is(err, io.EOF) {
									break
								} else if err != nil {
									b.Fatal(err)
								}
							}

							if decoded != sc.valueCount {
								b.Fatalf("decoded %d values, expected %d", decoded, sc.valueCount)
							}
						}

						elapsed := b.Elapsed()
						if elapsed > 0 {
							totalDecoded := int64(sc.valueCount) * int64(b.N)
							b.ReportMetric(float64(totalDecoded)/elapsed.Seconds(), "rows/s")
						}
					})
				}
			})
		})
	}
}
