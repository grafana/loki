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

// plainNumericTypes lists every numeric type supported by the plain encoding,
// used to drive the shared round-trip tests.
var plainNumericTypes = []struct {
	name  string
	build func(nullable bool) types.Type
}{
	{"int32", func(n bool) types.Type { return &types.Int32{Nullable: n} }},
	{"int64", func(n bool) types.Type { return &types.Int64{Nullable: n} }},
	{"uint32", func(n bool) types.Type { return &types.Uint32{Nullable: n} }},
	{"uint64", func(n bool) types.Type { return &types.Uint64{Nullable: n} }},
}

func TestPlainCodec_NonNullable(t *testing.T) {
	batches := [][]any{
		{10, 20, 30, 40, 50},
		{60, 70, 80, 90, 100},
	}
	expected := []any{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}

	for _, nt := range plainNumericTypes {
		t.Run(nt.name, func(t *testing.T) {
			runPlainRoundtrip(t, &array.SpecPlain{Validity: nil}, nt.build(false), batches, expected, 0)
		})
	}
}

func TestPlainCodec_Nullable(t *testing.T) {
	batches := [][]any{
		{10, nil, 30, nil, 50},
		{60, 70, 80, 90, 100},
		{nil},
	}
	expected := []any{10, nil, 30, nil, 50, 60, 70, 80, 90, 100, nil}

	for _, nt := range plainNumericTypes {
		t.Run(nt.name, func(t *testing.T) {
			runPlainRoundtrip(t, &array.SpecPlain{Validity: &array.SpecBool{}}, nt.build(true), batches, expected, 3)
		})
	}
}

// runPlainRoundtrip writes each batch to a plain [array.Writer] constructed
// from spec and typ, then reads the result back and asserts the decoded values
// match expected.
func runPlainRoundtrip(t *testing.T, spec array.Spec, typ types.Type, batches [][]any, expected []any, expectedNulls int) {
	t.Helper()

	var alloc memory.Allocator
	var store buffer.MemoryStore

	w, err := array.NewWriter(&alloc, spec, typ)
	require.NoError(t, err)

	kind := typ.Kind()

	var totalRows int
	for _, batch := range batches {
		require.NoError(t, w.Append(columnartest.Array(t, kind, &alloc, batch...)))
		totalRows += len(batch)
	}

	result, err := w.Flush(t.Context(), &store)
	require.NoError(t, err)
	require.Equal(t, totalRows, result.RowCount)
	require.Equal(t, expectedNulls, result.Stats.NullCount)

	r, err := array.NewReader(&alloc, result, &store)
	require.NoError(t, err)

	expect := columnartest.Array(t, kind, &alloc, expected...)

	actual := readBatches(t, &alloc, r, 2) // Read in a small batch size to test reading multiple times
	columnartest.RequireArraysEqual(t, expect, actual, memory.Bitmap{})

	// Reading again should produce an EOF.
	_, err = r.Read(t.Context(), &alloc, 1)
	require.ErrorIs(t, err, io.EOF)
}

func TestPlainCodec_Reader(t *testing.T) {
	t.Run("Reject undersized buffer", func(t *testing.T) {
		var alloc memory.Allocator
		var store buffer.MemoryStore

		var (
			spec = &array.SpecPlain{Validity: nil}
			typ  = &types.Int64{Nullable: false}
		)

		w, err := array.NewWriter(&alloc, spec, typ)
		require.NoError(t, err)

		input := columnartest.Array(t, types.KindInt64, &alloc, int64(10), int64(20), int64(30))
		require.NoError(t, w.Append(input))

		result, err := w.Flush(t.Context(), &store)
		require.NoError(t, err)

		// Corrupt RowCount so that it no longer fits in the underlying buffer.
		// Reading must fail cleanly rather than slicing past the end of the data.
		result.RowCount = 1_000_000

		r, err := array.NewReader(&alloc, result, &store)
		require.NoError(t, err)

		_, err = r.Read(t.Context(), &alloc, 1)
		require.Error(t, err)
	})

	t.Run("reject mismatching row count", func(t *testing.T) {
		var alloc memory.Allocator
		var store buffer.MemoryStore

		var (
			spec = &array.SpecPlain{Validity: &array.SpecBool{}}
			typ  = &types.Int64{Nullable: true}
		)

		w, err := array.NewWriter(&alloc, spec, typ)
		require.NoError(t, err)

		input := columnartest.Array(t, types.KindInt64, &alloc, int64(10), int64(20), int64(30))
		require.NoError(t, w.Append(input))

		result, err := w.Flush(t.Context(), &store)
		require.NoError(t, err)

		// Corrupt the Array so the validity child disagrees with the parent's row
		// count. NewReader must reject this upfront rather than letting it propagate
		// into Read (where it would panic inside columnar.NewNumber on length
		// mismatch).
		result.Children[0].RowCount = result.RowCount + 1

		_, err = array.NewReader(&alloc, result, &store)
		require.Error(t, err)
	})
}

func BenchmarkPlainCodec(b *testing.B) {
	var (
		store buffer.MemoryStore

		spec = &array.SpecPlain{Validity: nil}
		typ  = &types.Int64{Nullable: false}
	)

	const values = 1 << 16

	var arr array.Array
	{
		var alloc memory.Allocator

		builder := columnar.NewNumberBuilder[int64](&alloc)
		builder.Grow(values)

		for i := range values {
			builder.AppendValue(int64(i))
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
