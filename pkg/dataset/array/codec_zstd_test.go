package array_test

import (
	"errors"
	"fmt"
	"io"
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

func TestZstdCodec_Validation(t *testing.T) {
	tt := []struct {
		name string
		spec array.Spec
		typ  types.Type

		expectError bool
	}{
		{
			name:        "accepts valid non-nullable utf8",
			spec:        &array.SpecZstd{Offsets: &array.SpecPlain{}},
			typ:         &types.UTF8{Nullable: false},
			expectError: false,
		},
		{
			name:        "rejects missing offsets spec",
			spec:        &array.SpecZstd{Offsets: nil},
			typ:         &types.UTF8{Nullable: false},
			expectError: true,
		},
		{
			name:        "rejects non-nullable utf8 with validity spec",
			spec:        &array.SpecZstd{Offsets: &array.SpecPlain{}, Validity: &array.SpecBool{}},
			typ:         &types.UTF8{Nullable: false},
			expectError: true,
		},
		{
			name:        "rejects nullable utf8 with no validity spec",
			spec:        &array.SpecZstd{Offsets: &array.SpecPlain{}},
			typ:         &types.UTF8{Nullable: true},
			expectError: true,
		},
		{
			name:        "accepts nullable utf8 with validity spec",
			spec:        &array.SpecZstd{Offsets: &array.SpecPlain{}, Validity: &array.SpecBool{}},
			typ:         &types.UTF8{Nullable: true},
			expectError: false,
		},
		{
			name:        "rejects unsupported type bool",
			spec:        &array.SpecZstd{Offsets: &array.SpecPlain{}},
			typ:         &types.Bool{Nullable: false},
			expectError: true,
		},
		{
			name:        "rejects unsupported type int64",
			spec:        &array.SpecZstd{Offsets: &array.SpecPlain{}},
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

func TestZstdCodec_NonNullable(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	var (
		spec = &array.SpecZstd{Offsets: &array.SpecPlain{}}
		typ  = &types.UTF8{Nullable: false}
	)

	w, err := array.NewWriter(&alloc, spec, typ)
	require.NoError(t, err)

	input := []columnar.Array{
		columnartest.Array(t, types.KindUTF8, &alloc, "hello", "world", "", "foo", "bar"),
		columnartest.Array(t, types.KindUTF8, &alloc, "baz", "qux", "quux", "corge", "grault"),
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
		t, types.KindUTF8, &alloc,
		"hello", "world", "", "foo", "bar",
		"baz", "qux", "quux", "corge", "grault",
	)

	actual := readBatches(t, &alloc, r, 2) // Read in a small batch size to test reading multiple times
	columnartest.RequireArraysEqual(t, expect, actual, memory.Bitmap{})

	// Reading again should produce a EOF.
	_, err = r.Read(t.Context(), &alloc, 1)
	require.ErrorIs(t, err, io.EOF)
}

func TestZstdCodec_Nullable(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	var (
		spec = &array.SpecZstd{Offsets: &array.SpecPlain{}, Validity: &array.SpecBool{}}
		typ  = &types.UTF8{Nullable: true}
	)

	w, err := array.NewWriter(&alloc, spec, typ)
	require.NoError(t, err)

	input := []columnar.Array{
		columnartest.Array(t, types.KindUTF8, &alloc, "hello", nil, "world", nil, "foo"),
		columnartest.Array(t, types.KindUTF8, &alloc, "bar", "baz", "qux", "quux", "corge"),
		columnartest.Array(t, types.KindUTF8, &alloc, nil),
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
		t, types.KindUTF8, &alloc,
		"hello", nil, "world", nil, "foo",
		"bar", "baz", "qux", "quux", "corge",
		nil,
	)

	actual := readBatches(t, &alloc, r, 2) // Read in a small batch size to test reading multiple times
	columnartest.RequireArraysEqual(t, expect, actual, memory.Bitmap{})

	// Reading again should produce a EOF.
	_, err = r.Read(t.Context(), &alloc, 1)
	require.ErrorIs(t, err, io.EOF)
}

func TestZstdCodec_SlicedInput(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	var (
		spec = &array.SpecZstd{Offsets: &array.SpecPlain{}}
		typ  = &types.UTF8{Nullable: false}
	)

	// Build a full array then slice out a middle section, so the input has
	// non-zero-based offsets and a shared data buffer.
	full := columnartest.Array(t, types.KindUTF8, &alloc,
		"alpha", "bravo", "charlie", "delta", "echo",
	)
	sliced := full.Slice(1, 4) // ["bravo", "charlie", "delta"]

	w, err := array.NewWriter(&alloc, spec, typ)
	require.NoError(t, err)
	require.NoError(t, w.Append(sliced))

	result, err := w.Flush(t.Context(), &store)
	require.NoError(t, err)
	require.Equal(t, 3, result.RowCount)

	r, err := array.NewReader(&alloc, result, &store)
	require.NoError(t, err)

	expect := columnartest.Array(t, types.KindUTF8, &alloc,
		"bravo", "charlie", "delta",
	)

	actual := readBatches(t, &alloc, r, 2) // Read in a small batch size to test reading multiple times
	columnartest.RequireArraysEqual(t, expect, actual, memory.Bitmap{})
}

func BenchmarkZstdCodec(b *testing.B) {
	var (
		store buffer.MemoryStore

		spec = &array.SpecZstd{Offsets: &array.SpecPlain{}}
		typ  = &types.UTF8{Nullable: false}
	)

	const valuesPerPage = 1 << 16

	type scenario struct {
		name       string
		valueCount int
		encoded    array.Array
	}

	build := func(name string, valueCount int, valueAt func(i int) []byte) scenario {
		var alloc memory.Allocator
		w, err := array.NewWriter(&alloc, spec, typ)
		require.NoError(b, err)

		builder := columnar.NewUTF8Builder(&alloc)
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
		build("variance=constant", valuesPerPage, func(int) []byte { return []byte("hello") }),
		func() scenario {
			rnd := rand.New(rand.NewSource(0))
			return build("variance=random", valuesPerPage, func(int) []byte {
				buf := make([]byte, 5+rnd.Intn(20))
				rnd.Read(buf)
				return buf
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
						// Approximate bytes: data + offsets
						decodedBytesPerOp := int64(sc.encoded.RowCount) * 15 // ~15 bytes avg string
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
