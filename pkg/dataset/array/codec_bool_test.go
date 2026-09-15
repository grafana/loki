package array_test

import (
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/dataset/array"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/memory"
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
	var store buffer.MemoryStore

	var (
		spec = &array.SpecBool{Validity: nil}
		typ  = &types.Bool{Nullable: false}
	)

	w, err := array.NewWriter(&alloc, spec, typ)
	require.NoError(t, err)

	input := []columnar.Array{
		columnartest.Array(t, types.KindBool, &alloc, true, false, false, true, false),
		columnartest.Array(t, types.KindBool, &alloc, false, true, false, false, true),
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
		t, types.KindBool, &alloc,
		true, false, false, true, false,
		false, true, false, false, true,
	)

	actual := readBatches(t, &alloc, r, 2) // Read in a small batch size to test reading multiple times
	columnartest.RequireArraysEqual(t, expect, actual, memory.Bitmap{})

	// Reading again should produce a EOF.
	_, err = r.Read(t.Context(), &alloc, 1)
	require.ErrorIs(t, err, io.EOF)
}

func TestBoolCodec_Nullable(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	var (
		spec = &array.SpecBool{Validity: &array.SpecBool{}}
		typ  = &types.Bool{Nullable: true}
	)

	w, err := array.NewWriter(&alloc, spec, typ)
	require.NoError(t, err)

	input := []columnar.Array{
		columnartest.Array(t, types.KindBool, &alloc, true, nil, false, nil, false),
		columnartest.Array(t, types.KindBool, &alloc, false, true, false, false, true),
		columnartest.Array(t, types.KindBool, &alloc, nil),
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
		t, types.KindBool, &alloc,
		true, nil, false, nil, false,
		false, true, false, false, true,
		nil,
	)

	actual := readBatches(t, &alloc, r, 2) // Read in a small batch size to test reading multiple times
	columnartest.RequireArraysEqual(t, expect, actual, memory.Bitmap{})

	// Reading again should produce a EOF.
	_, err = r.Read(t.Context(), &alloc, 1)
	require.ErrorIs(t, err, io.EOF)
}

func BenchmarkBoolCodec(b *testing.B) {
	var (
		store buffer.MemoryStore

		spec = &array.SpecBool{Validity: nil}
		typ  = &types.Bool{Nullable: false}
	)

	const values = 1 << 16

	var arr array.Array
	{
		var alloc memory.Allocator

		builder := columnar.NewBoolBuilder(&alloc)
		builder.Grow(1_000_000)

		for i := range values {
			// Alternating bits of false/true
			builder.AppendValue(i%2 == 0)
		}

		writer, err := array.NewWriter(&alloc, spec, typ)
		require.NoError(b, err)

		require.NoError(b, writer.Append(builder.Build()))
		arr, err = writer.Flush(b.Context(), &store)
		require.NoError(b, err)
	}

	batchSizes := []int{2048, 4096, 8192}

	b.Run(fmt.Sprintf("rows=%d", values), func(b *testing.B) {
		for _, batchSize := range batchSizes {
			b.Run(fmt.Sprintf("batch_size=%d", batchSize), func(b *testing.B) {
				var alloc memory.Allocator

				var totalBytes int64
				var totalRows int64

				for b.Loop() {
					alloc.Reset()

					r, _ := array.NewReader(&alloc, arr, &store)
					for {
						arr, err := r.Read(b.Context(), &alloc, batchSize)
						if arr != nil {
							totalBytes += int64(arr.Size())
							totalRows += int64(arr.Len())
						}

						if err != nil && errors.Is(err, io.EOF) {
							break
						} else if err != nil {
							b.Fatal(err)
						}
					}
				}

				b.SetBytes(totalBytes / int64(b.N))

				elapsed := b.Elapsed()
				if elapsed > 0 {
					b.ReportMetric(float64(totalRows)/elapsed.Seconds(), "rows/s")
				}
			})
		}
	})
}

func readBatches(t *testing.T, alloc *memory.Allocator, r array.Reader, batchSize int) columnar.Array {
	t.Helper()

	var all []columnar.Array
	for {
		arr, err := r.Read(t.Context(), alloc, batchSize)
		if arr != nil {
			all = append(all, arr)
		}

		if err != nil && errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}

	res, err := columnar.Concat(alloc, all)
	require.NoError(t, err)
	return res
}
