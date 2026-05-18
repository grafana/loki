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

func TestBitpackedCodec_Validation(t *testing.T) {
	widths := &array.SpecPlain{Validity: nil}

	tt := []struct {
		name string
		spec array.Spec
		typ  types.Type

		expectError bool
	}{
		{
			name:        "accepts valid non-nullable uint32",
			spec:        &array.SpecBitpacked{BlockSize: 128, Widths: widths},
			typ:         &types.Uint32{Nullable: false},
			expectError: false,
		},
		{
			name:        "accepts valid non-nullable uint64",
			spec:        &array.SpecBitpacked{BlockSize: 128, Widths: widths},
			typ:         &types.Uint64{Nullable: false},
			expectError: false,
		},
		{
			name:        "accepts nullable uint64 with validity spec",
			spec:        &array.SpecBitpacked{BlockSize: 128, Widths: widths, Validity: &array.SpecBool{}},
			typ:         &types.Uint64{Nullable: true},
			expectError: false,
		},
		{
			name:        "rejects nullable uint64 with no validity spec",
			spec:        &array.SpecBitpacked{BlockSize: 128, Widths: widths},
			typ:         &types.Uint64{Nullable: true},
			expectError: true,
		},
		{
			name:        "rejects non-nullable uint64 with validity spec",
			spec:        &array.SpecBitpacked{BlockSize: 128, Widths: widths, Validity: &array.SpecBool{}},
			typ:         &types.Uint64{Nullable: false},
			expectError: true,
		},
		{
			name:        "rejects int32",
			spec:        &array.SpecBitpacked{BlockSize: 128, Widths: widths},
			typ:         &types.Int32{Nullable: false},
			expectError: true,
		},
		{
			name:        "rejects int64",
			spec:        &array.SpecBitpacked{BlockSize: 128, Widths: widths},
			typ:         &types.Int64{Nullable: false},
			expectError: true,
		},
		{
			name:        "rejects bool",
			spec:        &array.SpecBitpacked{BlockSize: 128, Widths: widths},
			typ:         &types.Bool{Nullable: false},
			expectError: true,
		},
		{
			name:        "rejects utf8",
			spec:        &array.SpecBitpacked{BlockSize: 128, Widths: widths},
			typ:         &types.UTF8{Nullable: false},
			expectError: true,
		},
		{
			name:        "rejects zero block size",
			spec:        &array.SpecBitpacked{BlockSize: 0, Widths: widths},
			typ:         &types.Uint64{Nullable: false},
			expectError: true,
		},
		{
			name:        "rejects negative block size",
			spec:        &array.SpecBitpacked{BlockSize: -1, Widths: widths},
			typ:         &types.Uint64{Nullable: false},
			expectError: true,
		},
		{
			name:        "rejects non-multiple-of-8 block size",
			spec:        &array.SpecBitpacked{BlockSize: 7, Widths: widths},
			typ:         &types.Uint64{Nullable: false},
			expectError: true,
		},
		{
			name:        "rejects nil widths spec",
			spec:        &array.SpecBitpacked{BlockSize: 128, Widths: nil},
			typ:         &types.Uint64{Nullable: false},
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

func TestBitpackedCodec_NonNullable(t *testing.T) {
	t.Run("uint32", func(t *testing.T) {
		var alloc memory.Allocator
		var store buffer.MemoryStore

		var (
			spec = &array.SpecBitpacked{BlockSize: 8, Widths: &array.SpecPlain{}}
			typ  = &types.Uint32{Nullable: false}
		)

		w, err := array.NewWriter(&alloc, spec, typ)
		require.NoError(t, err)

		input := []columnar.Array{
			columnartest.Array(t, types.KindUint32, &alloc, uint32(10), uint32(20), uint32(30), uint32(40), uint32(50)),
			columnartest.Array(t, types.KindUint32, &alloc, uint32(60), uint32(70), uint32(80), uint32(90), uint32(100)),
		}
		for _, arr := range input {
			require.NoError(t, w.Append(arr))
		}

		result, err := w.Flush(t.Context(), &store)
		require.NoError(t, err)
		require.Equal(t, 10, result.RowCount)
		require.Equal(t, 0, result.Stats.NullCount)

		r, err := array.NewReader(&alloc, result, &store)
		require.NoError(t, err)

		expect := columnartest.Array(
			t, types.KindUint32, &alloc,
			uint32(10), uint32(20), uint32(30), uint32(40), uint32(50),
			uint32(60), uint32(70), uint32(80), uint32(90), uint32(100),
		)

		actual := readBatches(t, &alloc, r, 2) // Read in a small batch size to test reading multiple times
		columnartest.RequireArraysEqual(t, expect, actual, memory.Bitmap{})

		_, err = r.Read(t.Context(), &alloc, 1)
		require.ErrorIs(t, err, io.EOF)
	})

	t.Run("uint64", func(t *testing.T) {
		var alloc memory.Allocator
		var store buffer.MemoryStore

		var (
			spec = &array.SpecBitpacked{BlockSize: 8, Widths: &array.SpecPlain{}}
			typ  = &types.Uint64{Nullable: false}
		)

		w, err := array.NewWriter(&alloc, spec, typ)
		require.NoError(t, err)

		input := []columnar.Array{
			columnartest.Array(t, types.KindUint64, &alloc, uint64(10), uint64(20), uint64(30), uint64(40), uint64(50)),
			columnartest.Array(t, types.KindUint64, &alloc, uint64(60), uint64(70), uint64(80), uint64(90), uint64(100)),
		}
		for _, arr := range input {
			require.NoError(t, w.Append(arr))
		}

		result, err := w.Flush(t.Context(), &store)
		require.NoError(t, err)
		require.Equal(t, 10, result.RowCount)
		require.Equal(t, 0, result.Stats.NullCount)

		r, err := array.NewReader(&alloc, result, &store)
		require.NoError(t, err)

		expect := columnartest.Array(
			t, types.KindUint64, &alloc,
			uint64(10), uint64(20), uint64(30), uint64(40), uint64(50),
			uint64(60), uint64(70), uint64(80), uint64(90), uint64(100),
		)

		actual := readBatches(t, &alloc, r, 2) // Read in a small batch size to test reading multiple times
		columnartest.RequireArraysEqual(t, expect, actual, memory.Bitmap{})

		_, err = r.Read(t.Context(), &alloc, 1)
		require.ErrorIs(t, err, io.EOF)
	})
}

func TestBitpackedCodec_Nullable(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	var (
		spec = &array.SpecBitpacked{BlockSize: 8, Widths: &array.SpecPlain{}, Validity: &array.SpecBool{}}
		typ  = &types.Uint64{Nullable: true}
	)

	w, err := array.NewWriter(&alloc, spec, typ)
	require.NoError(t, err)

	input := []columnar.Array{
		columnartest.Array(t, types.KindUint64, &alloc, uint64(10), nil, uint64(30), nil, uint64(50)),
		columnartest.Array(t, types.KindUint64, &alloc, uint64(60), uint64(70), uint64(80), uint64(90), uint64(100)),
		columnartest.Array(t, types.KindUint64, &alloc, nil),
	}
	for _, arr := range input {
		require.NoError(t, w.Append(arr))
	}

	result, err := w.Flush(t.Context(), &store)
	require.NoError(t, err)
	require.Equal(t, 11, result.RowCount)
	require.Equal(t, 3, result.Stats.NullCount)

	r, err := array.NewReader(&alloc, result, &store)
	require.NoError(t, err)

	expect := columnartest.Array(
		t, types.KindUint64, &alloc,
		uint64(10), nil, uint64(30), nil, uint64(50),
		uint64(60), uint64(70), uint64(80), uint64(90), uint64(100),
		nil,
	)

	actual := readBatches(t, &alloc, r, 2) // Read in a small batch size to test reading multiple times
	columnartest.RequireArraysEqual(t, expect, actual, memory.Bitmap{})

	_, err = r.Read(t.Context(), &alloc, 1)
	require.ErrorIs(t, err, io.EOF)
}

func TestBitpackedCodec_AllZeros(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	var (
		spec = &array.SpecBitpacked{BlockSize: 8, Widths: &array.SpecPlain{}}
		typ  = &types.Uint64{Nullable: false}
	)

	w, err := array.NewWriter(&alloc, spec, typ)
	require.NoError(t, err)

	input := columnartest.Array(t, types.KindUint64, &alloc,
		uint64(0), uint64(0), uint64(0), uint64(0), uint64(0),
	)
	require.NoError(t, w.Append(input))

	result, err := w.Flush(t.Context(), &store)
	require.NoError(t, err)
	require.Equal(t, 5, result.RowCount)

	r, err := array.NewReader(&alloc, result, &store)
	require.NoError(t, err)

	actual := readBatches(t, &alloc, r, 2) // Read in a small batch size to test reading multiple times
	columnartest.RequireArraysEqual(t, input, actual, memory.Bitmap{})
}

func TestBitpackedCodec_MaxWidth(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	var (
		spec = &array.SpecBitpacked{BlockSize: 128, Widths: &array.SpecPlain{}}
		typ  = &types.Uint64{Nullable: false}
	)

	w, err := array.NewWriter(&alloc, spec, typ)
	require.NoError(t, err)

	input := columnartest.Array(t, types.KindUint64, &alloc,
		uint64(0), uint64(math.MaxUint64), uint64(42),
	)
	require.NoError(t, w.Append(input))

	result, err := w.Flush(t.Context(), &store)
	require.NoError(t, err)

	r, err := array.NewReader(&alloc, result, &store)
	require.NoError(t, err)

	actual := readBatches(t, &alloc, r, 2) // Read in a small batch size to test reading multiple times
	columnartest.RequireArraysEqual(t, input, actual, memory.Bitmap{})
}

func TestBitpackedCodec_SingleBitValues(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	var (
		spec = &array.SpecBitpacked{BlockSize: 128, Widths: &array.SpecPlain{}}
		typ  = &types.Uint32{Nullable: false}
	)

	w, err := array.NewWriter(&alloc, spec, typ)
	require.NoError(t, err)

	input := columnartest.Array(t, types.KindUint32, &alloc,
		uint32(0), uint32(1), uint32(1), uint32(0), uint32(1),
		uint32(0), uint32(0), uint32(1), uint32(1), uint32(1),
	)
	require.NoError(t, w.Append(input))

	result, err := w.Flush(t.Context(), &store)
	require.NoError(t, err)

	r, err := array.NewReader(&alloc, result, &store)
	require.NoError(t, err)

	actual := readBatches(t, &alloc, r, 2) // Read in a small batch size to test reading multiple times
	columnartest.RequireArraysEqual(t, input, actual, memory.Bitmap{})
}

func TestBitpackedCodec_MixedWidthBlocks(t *testing.T) {
	// Block 1 (small values, ~3 bits) and Block 2 (large values, ~7 bits)
	// demonstrate per-block width adaptation.
	var alloc memory.Allocator
	var store buffer.MemoryStore

	var (
		spec = &array.SpecBitpacked{BlockSize: 8, Widths: &array.SpecPlain{}}
		typ  = &types.Uint64{Nullable: false}
	)

	w, err := array.NewWriter(&alloc, spec, typ)
	require.NoError(t, err)

	input := columnartest.Array(t, types.KindUint64, &alloc,
		// Block 1: max=7 → width=3
		uint64(1), uint64(3), uint64(7), uint64(5), uint64(2), uint64(0), uint64(6), uint64(1),
		// Block 2: max=100 → width=7
		uint64(100), uint64(50), uint64(99), uint64(1),
	)
	require.NoError(t, w.Append(input))

	result, err := w.Flush(t.Context(), &store)
	require.NoError(t, err)

	r, err := array.NewReader(&alloc, result, &store)
	require.NoError(t, err)

	actual := readBatches(t, &alloc, r, 2) // Read in a small batch size to test reading multiple times
	columnartest.RequireArraysEqual(t, input, actual, memory.Bitmap{})
}

func BenchmarkBitpackedCodec(b *testing.B) {
	var (
		store buffer.MemoryStore

		spec = &array.SpecBitpacked{BlockSize: 128, Widths: &array.SpecPlain{}}
		typ  = &types.Uint64{Nullable: false}
	)

	const valuesPerPage = 1 << 16

	type scenario struct {
		name       string
		valueCount int
		encoded    array.Array
	}

	build := func(name string, valueCount int, valueAt func(i int) uint64) scenario {
		var alloc memory.Allocator
		w, err := array.NewWriter(&alloc, spec, typ)
		require.NoError(b, err)

		builder := columnar.NewNumberBuilder[uint64](&alloc)
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
		build("variance=constant", valuesPerPage, func(int) uint64 { return 42 }),
		build("variance=small_range", valuesPerPage, func(i int) uint64 { return uint64(i % 16) }),
		func() scenario {
			rnd := rand.New(rand.NewSource(0))
			return build("variance=random", valuesPerPage, func(int) uint64 {
				return rnd.Uint64()
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
