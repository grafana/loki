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

func TestPlainCodec_Validation(t *testing.T) {
	tt := []struct {
		name string
		spec array.Spec
		typ  types.Type

		expectError bool
	}{
		{
			name:        "accepts valid non-nullable int32",
			spec:        &array.SpecPlain{Validity: nil},
			typ:         &types.Int32{Nullable: false},
			expectError: false,
		},
		{
			name:        "accepts valid non-nullable int64",
			spec:        &array.SpecPlain{Validity: nil},
			typ:         &types.Int64{Nullable: false},
			expectError: false,
		},
		{
			name:        "accepts valid non-nullable uint32",
			spec:        &array.SpecPlain{Validity: nil},
			typ:         &types.Uint32{Nullable: false},
			expectError: false,
		},
		{
			name:        "accepts valid non-nullable uint64",
			spec:        &array.SpecPlain{Validity: nil},
			typ:         &types.Uint64{Nullable: false},
			expectError: false,
		},
		{
			name:        "rejects non-nullable int64 with validity spec",
			spec:        &array.SpecPlain{Validity: &array.SpecBool{}},
			typ:         &types.Int64{Nullable: false},
			expectError: true,
		},
		{
			name:        "rejects nullable int64 with no validity spec",
			spec:        &array.SpecPlain{Validity: nil},
			typ:         &types.Int64{Nullable: true},
			expectError: true,
		},
		{
			name:        "accepts nullable int64 with validity spec",
			spec:        &array.SpecPlain{Validity: &array.SpecBool{}},
			typ:         &types.Int64{Nullable: true},
			expectError: false,
		},
		{
			name:        "rejects unsupported type bool",
			spec:        &array.SpecPlain{Validity: nil},
			typ:         &types.Bool{Nullable: false},
			expectError: true,
		},
		{
			name:        "rejects unsupported type utf8",
			spec:        &array.SpecPlain{Validity: nil},
			typ:         &types.UTF8{Nullable: false},
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

func TestPlainCodec_NonNullable(t *testing.T) {
	t.Run("int32", func(t *testing.T) {
		var alloc memory.Allocator
		var store inMemoryStore

		var (
			spec = &array.SpecPlain{Validity: nil}
			typ  = &types.Int32{Nullable: false}
		)

		w, err := array.NewWriter(&alloc, spec, typ)
		require.NoError(t, err)

		input := []columnar.Array{
			columnartest.Array(t, columnar.KindInt32, &alloc, int32(10), int32(-20), int32(30), int32(40), int32(-50)),
			columnartest.Array(t, columnar.KindInt32, &alloc, int32(60), int32(70), int32(80), int32(90), int32(100)),
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
			t, columnar.KindInt32, &alloc,
			int32(10), int32(-20), int32(30), int32(40), int32(-50),
			int32(60), int32(70), int32(80), int32(90), int32(100),
		)
		actual, err := r.Read(t.Context(), &alloc, result.Stats.RowCount)
		require.NoError(t, err)
		columnartest.RequireArraysEqual(t, expect, actual)

		// Reading again should produce a EOF.
		_, err = r.Read(t.Context(), &alloc, 1)
		require.ErrorIs(t, err, io.EOF)
	})

	t.Run("int64", func(t *testing.T) {
		var alloc memory.Allocator
		var store inMemoryStore

		var (
			spec = &array.SpecPlain{Validity: nil}
			typ  = &types.Int64{Nullable: false}
		)

		w, err := array.NewWriter(&alloc, spec, typ)
		require.NoError(t, err)

		input := []columnar.Array{
			columnartest.Array(t, columnar.KindInt64, &alloc, int64(10), int64(-20), int64(30), int64(40), int64(-50)),
			columnartest.Array(t, columnar.KindInt64, &alloc, int64(60), int64(70), int64(80), int64(90), int64(100)),
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
			t, columnar.KindInt64, &alloc,
			int64(10), int64(-20), int64(30), int64(40), int64(-50),
			int64(60), int64(70), int64(80), int64(90), int64(100),
		)
		actual, err := r.Read(t.Context(), &alloc, result.Stats.RowCount)
		require.NoError(t, err)
		columnartest.RequireArraysEqual(t, expect, actual)

		// Reading again should produce a EOF.
		_, err = r.Read(t.Context(), &alloc, 1)
		require.ErrorIs(t, err, io.EOF)
	})

	t.Run("uint32", func(t *testing.T) {
		var alloc memory.Allocator
		var store inMemoryStore

		var (
			spec = &array.SpecPlain{Validity: nil}
			typ  = &types.Uint32{Nullable: false}
		)

		w, err := array.NewWriter(&alloc, spec, typ)
		require.NoError(t, err)

		input := []columnar.Array{
			columnartest.Array(t, columnar.KindUint32, &alloc, uint32(10), uint32(20), uint32(30), uint32(40), uint32(50)),
			columnartest.Array(t, columnar.KindUint32, &alloc, uint32(60), uint32(70), uint32(80), uint32(90), uint32(100)),
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
			t, columnar.KindUint32, &alloc,
			uint32(10), uint32(20), uint32(30), uint32(40), uint32(50),
			uint32(60), uint32(70), uint32(80), uint32(90), uint32(100),
		)
		actual, err := r.Read(t.Context(), &alloc, result.Stats.RowCount)
		require.NoError(t, err)
		columnartest.RequireArraysEqual(t, expect, actual)

		// Reading again should produce a EOF.
		_, err = r.Read(t.Context(), &alloc, 1)
		require.ErrorIs(t, err, io.EOF)
	})

	t.Run("uint64", func(t *testing.T) {
		var alloc memory.Allocator
		var store inMemoryStore

		var (
			spec = &array.SpecPlain{Validity: nil}
			typ  = &types.Uint64{Nullable: false}
		)

		w, err := array.NewWriter(&alloc, spec, typ)
		require.NoError(t, err)

		input := []columnar.Array{
			columnartest.Array(t, columnar.KindUint64, &alloc, uint64(10), uint64(20), uint64(30), uint64(40), uint64(50)),
			columnartest.Array(t, columnar.KindUint64, &alloc, uint64(60), uint64(70), uint64(80), uint64(90), uint64(100)),
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
			t, columnar.KindUint64, &alloc,
			uint64(10), uint64(20), uint64(30), uint64(40), uint64(50),
			uint64(60), uint64(70), uint64(80), uint64(90), uint64(100),
		)
		actual, err := r.Read(t.Context(), &alloc, result.Stats.RowCount)
		require.NoError(t, err)
		columnartest.RequireArraysEqual(t, expect, actual)

		// Reading again should produce a EOF.
		_, err = r.Read(t.Context(), &alloc, 1)
		require.ErrorIs(t, err, io.EOF)
	})
}

func TestPlainCodec_Nullable(t *testing.T) {
	var alloc memory.Allocator
	var store inMemoryStore

	var (
		spec = &array.SpecPlain{Validity: &array.SpecBool{}}
		typ  = &types.Int64{Nullable: true}
	)

	w, err := array.NewWriter(&alloc, spec, typ)
	require.NoError(t, err)

	input := []columnar.Array{
		columnartest.Array(t, columnar.KindInt64, &alloc, int64(10), nil, int64(30), nil, int64(50)),
		columnartest.Array(t, columnar.KindInt64, &alloc, int64(60), int64(70), int64(80), int64(90), int64(100)),
		columnartest.Array(t, columnar.KindInt64, &alloc, nil),
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
		t, columnar.KindInt64, &alloc,
		int64(10), nil, int64(30), nil, int64(50),
		int64(60), int64(70), int64(80), int64(90), int64(100),
		nil,
	)
	actual, err := r.Read(t.Context(), &alloc, result.Stats.RowCount)
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t, expect, actual)

	// Reading again should produce a EOF.
	_, err = r.Read(t.Context(), &alloc, 1)
	require.ErrorIs(t, err, io.EOF)
}

func BenchmarkPlainCodec(b *testing.B) {
	var (
		store inMemoryStore

		spec = &array.SpecPlain{Validity: nil}
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
		build("variance=constant", valuesPerPage, func(int) int64 { return 42 }),
		func() scenario {
			rnd := rand.New(rand.NewSource(0))
			return build("variance=random", valuesPerPage, func(int) int64 {
				return rnd.Int63()
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
						decodedBytesPerOp := int64(sc.valueCount) * 8 // Each int64 is 8 bytes
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
