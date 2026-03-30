package array_test

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	"testing"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/dataset/array"
	"github.com/grafana/loki/v3/pkg/dataset/types"
	"github.com/grafana/loki/v3/pkg/memory"
	"github.com/stretchr/testify/require"
)

func TestBoolCodec_Validation(t *testing.T) {
	tt := []struct {
		name string
		spec array.Spec
		typ  types.Type

		expectError bool
	}{
		{
			name:        "accepts valid non-nullable bool",
			spec:        &array.SpecBool{Validity: nil},
			typ:         &types.Bool{Nullable: false},
			expectError: false,
		},
		{
			name:        "rejects non-nullable bool with validity spec",
			spec:        &array.SpecBool{Validity: &array.SpecBool{}},
			typ:         &types.Bool{Nullable: false},
			expectError: true,
		},
		{
			name:        "rejects nullable bool with no validity spec",
			spec:        &array.SpecBool{Validity: nil},
			typ:         &types.Bool{Nullable: true},
			expectError: true,
		},
		{
			name:        "accepts nullable bool with validity spec",
			spec:        &array.SpecBool{Validity: &array.SpecBool{}},
			typ:         &types.Bool{Nullable: true},
			expectError: false,
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

func TestBoolCodec_NonNullable(t *testing.T) {
	var alloc memory.Allocator
	var store inMemoryStore

	var (
		spec = &array.SpecBool{Validity: nil}
		typ  = &types.Bool{Nullable: false}
	)

	w, err := array.NewWriter(&alloc, spec, typ)
	require.NoError(t, err)

	input := []columnar.Array{
		columnartest.Array(t, columnar.KindBool, &alloc, true, false, false, true, false),
		columnartest.Array(t, columnar.KindBool, &alloc, false, true, false, false, true),
	}
	for _, arr := range input {
		require.NoError(t, w.Append(arr))
	}

	result, err := w.Flush(t.Context(), &store)
	require.NoError(t, err)
	require.Equal(t, 10, result.Stats.RowCount)
	require.Equal(t, 0, result.Stats.NullCount)

	r, err := array.NewReader(&alloc, result, &store)
	require.NoError(t, err)

	expect := columnartest.Array(
		t, columnar.KindBool, &alloc,
		true, false, false, true, false,
		false, true, false, false, true,
	)
	actual, err := r.Read(t.Context(), &alloc, result.Stats.RowCount)
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t, expect, actual)

	// Reading again should produce a EOF.
	_, err = r.Read(t.Context(), &alloc, 1)
	require.ErrorIs(t, err, io.EOF)
}

func TestBoolCodec_Nullable(t *testing.T) {
	var alloc memory.Allocator
	var store inMemoryStore

	var (
		spec = &array.SpecBool{Validity: &array.SpecBool{}}
		typ  = &types.Bool{Nullable: true}
	)

	w, err := array.NewWriter(&alloc, spec, typ)
	require.NoError(t, err)

	input := []columnar.Array{
		columnartest.Array(t, columnar.KindBool, &alloc, true, nil, false, nil, false),
		columnartest.Array(t, columnar.KindBool, &alloc, false, true, false, false, true),
		columnartest.Array(t, columnar.KindBool, &alloc, nil),
	}
	for _, arr := range input {
		require.NoError(t, w.Append(arr))
	}

	result, err := w.Flush(t.Context(), &store)
	require.NoError(t, err)
	require.Equal(t, 11, result.Stats.RowCount)
	require.Equal(t, 3, result.Stats.NullCount)

	r, err := array.NewReader(&alloc, result, &store)
	require.NoError(t, err)

	expect := columnartest.Array(
		t, columnar.KindBool, &alloc,
		true, nil, false, nil, false,
		false, true, false, false, true,
		nil,
	)
	actual, err := r.Read(t.Context(), &alloc, result.Stats.RowCount)
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t, expect, actual)

	// Reading again should produce a EOF.
	_, err = r.Read(t.Context(), &alloc, 1)
	require.ErrorIs(t, err, io.EOF)
}

func BenchmarkBoolCodec(b *testing.B) {
	var (
		store inMemoryStore

		spec = &array.SpecBool{Validity: nil}
		typ  = &types.Bool{Nullable: false}
	)

	const valuesPerPage = 1 << 16

	type scenario struct {
		name       string
		valueCount int
		encoded    array.Array
	}

	build := func(name string, valueCount int, valueAt func(i int) bool) scenario {
		var alloc memory.Allocator
		w, err := array.NewWriter(&alloc, spec, typ)
		require.NoError(b, err)

		builder := columnar.NewBoolBuilder(&alloc)
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
		build("variance=rle", valuesPerPage, func(int) bool { return true }),
		func() scenario {
			rnd64 := rand.New(rand.NewSource(0))
			return build("variance=mixed_rle_and_bitpack", valuesPerPage, func(i int) bool {
				// Alternates between long RLE runs and bitpacked-ish data.
				if (i/1024)%2 == 0 {
					return true
				}
				return rnd64.Intn(2) == 0
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
						decodedBytesPerOp := int64(sc.valueCount) / 8 // Each value is one bit
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
