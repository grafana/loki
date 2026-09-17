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

func TestDeltaCodec_Validation(t *testing.T) {
	data := &array.SpecPlain{}

	tt := []struct {
		name string
		spec array.Spec
		typ  types.Type

		expectError bool
	}{
		{
			name:        "accepts non-nullable int32",
			spec:        &array.SpecDelta{Data: data},
			typ:         &types.Int32{Nullable: false},
			expectError: false,
		},
		{
			name:        "accepts non-nullable int64",
			spec:        &array.SpecDelta{Data: data},
			typ:         &types.Int64{Nullable: false},
			expectError: false,
		},
		{
			name:        "accepts non-nullable uint32",
			spec:        &array.SpecDelta{Data: data},
			typ:         &types.Uint32{Nullable: false},
			expectError: false,
		},
		{
			name:        "accepts non-nullable uint64",
			spec:        &array.SpecDelta{Data: data},
			typ:         &types.Uint64{Nullable: false},
			expectError: false,
		},
		{
			name:        "accepts nullable int64 with validity in data spec",
			spec:        &array.SpecDelta{Data: &array.SpecPlain{Validity: &array.SpecBool{}}},
			typ:         &types.Int64{Nullable: true},
			expectError: false,
		},
		{
			name:        "rejects bool",
			spec:        &array.SpecDelta{Data: data},
			typ:         &types.Bool{Nullable: false},
			expectError: true,
		},
		{
			name:        "rejects utf8",
			spec:        &array.SpecDelta{Data: data},
			typ:         &types.UTF8{Nullable: false},
			expectError: true,
		},
		{
			name:        "rejects nil data spec",
			spec:        &array.SpecDelta{Data: nil},
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

func TestDeltaCodec_NonNullable(t *testing.T) {
	t.Run("int32", func(t *testing.T) {
		var alloc memory.Allocator
		var store buffer.MemoryStore

		var (
			spec = &array.SpecDelta{Data: &array.SpecPlain{}}
			typ  = &types.Int32{Nullable: false}
		)

		w, err := array.NewWriter(&alloc, spec, typ)
		require.NoError(t, err)

		input := []columnar.Array{
			columnartest.Array(t, types.KindInt32, &alloc, int32(10), int32(20), int32(30), int32(25), int32(15)),
			columnartest.Array(t, types.KindInt32, &alloc, int32(15), int32(100), int32(100), int32(0)),
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
			int32(10), int32(20), int32(30), int32(25), int32(15),
			int32(15), int32(100), int32(100), int32(0),
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
			spec = &array.SpecDelta{Data: &array.SpecPlain{}}
			typ  = &types.Int64{Nullable: false}
		)

		w, err := array.NewWriter(&alloc, spec, typ)
		require.NoError(t, err)

		input := []columnar.Array{
			columnartest.Array(t, types.KindInt64, &alloc, int64(100), int64(200), int64(150), int64(300), int64(250)),
			columnartest.Array(t, types.KindInt64, &alloc, int64(250), int64(500), int64(0), int64(-100)),
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
			int64(100), int64(200), int64(150), int64(300), int64(250),
			int64(250), int64(500), int64(0), int64(-100),
		)

		actual := readBatches(t, &alloc, r, 2) // Read in a small batch size to test reading multiple times
		columnartest.RequireArraysEqual(t, expect, actual, memory.Bitmap{})

		_, err = r.Read(t.Context(), &alloc, 1)
		require.ErrorIs(t, err, io.EOF)
	})
}

func TestDeltaCodec_Nullable(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	var (
		spec = &array.SpecDelta{Data: &array.SpecPlain{Validity: &array.SpecBool{}}}
		typ  = &types.Int64{Nullable: true}
	)

	w, err := array.NewWriter(&alloc, spec, typ)
	require.NoError(t, err)

	input := []columnar.Array{
		columnartest.Array(t, types.KindInt64, &alloc, int64(10), nil, int64(30), nil, int64(50)),
		columnartest.Array(t, types.KindInt64, &alloc, int64(60), int64(70)),
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
		int64(10), nil, int64(30), nil, int64(50),
		int64(60), int64(70),
		nil,
	)

	actual := readBatches(t, &alloc, r, 2) // Read in a small batch size to test reading multiple times
	columnartest.RequireArraysEqual(t, expect, actual, memory.Bitmap{})

	_, err = r.Read(t.Context(), &alloc, 1)
	require.ErrorIs(t, err, io.EOF)
}

func TestDeltaCodec_PartialRead(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	var (
		spec = &array.SpecDelta{Data: &array.SpecPlain{}}
		typ  = &types.Int64{Nullable: false}
	)

	w, err := array.NewWriter(&alloc, spec, typ)
	require.NoError(t, err)

	input := columnartest.Array(t, types.KindInt64, &alloc,
		int64(10), int64(20), int64(30), int64(40), int64(50),
		int64(60), int64(70), int64(80), int64(90), int64(100),
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
		columnartest.Array(t, types.KindInt64, &alloc, int64(10), int64(20), int64(30)),
		batch1, memory.Bitmap{},
	)

	batch2, err := r.Read(t.Context(), &alloc, 3)
	require.NoError(t, err)
	require.Equal(t, 3, batch2.Len())
	columnartest.RequireArraysEqual(t,
		columnartest.Array(t, types.KindInt64, &alloc, int64(40), int64(50), int64(60)),
		batch2, memory.Bitmap{},
	)

	batch3, err := r.Read(t.Context(), &alloc, 3)
	require.NoError(t, err)
	require.Equal(t, 3, batch3.Len())
	columnartest.RequireArraysEqual(t,
		columnartest.Array(t, types.KindInt64, &alloc, int64(70), int64(80), int64(90)),
		batch3, memory.Bitmap{},
	)

	// Last batch: only 1 remaining.
	batch4, err := r.Read(t.Context(), &alloc, 3)
	require.NoError(t, err)
	require.Equal(t, 1, batch4.Len())
	columnartest.RequireArraysEqual(t,
		columnartest.Array(t, types.KindInt64, &alloc, int64(100)),
		batch4, memory.Bitmap{},
	)

	_, err = r.Read(t.Context(), &alloc, 1)
	require.ErrorIs(t, err, io.EOF)
}

func TestDeltaCodec_ReuseAfterFlush(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	var (
		spec = &array.SpecDelta{Data: &array.SpecPlain{}}
		typ  = &types.Int64{Nullable: false}
	)

	w, err := array.NewWriter(&alloc, spec, typ)
	require.NoError(t, err)

	// First session: append, flush, read back.
	first := columnartest.Array(t, types.KindInt64, &alloc,
		int64(100), int64(200), int64(300),
	)
	require.NoError(t, w.Append(first))

	firstResult, err := w.Flush(t.Context(), &store)
	require.NoError(t, err)

	firstReader, err := array.NewReader(&alloc, firstResult, &store)
	require.NoError(t, err)
	firstActual := readBatches(t, &alloc, firstReader, 8)
	columnartest.RequireArraysEqual(t, first, firstActual, memory.Bitmap{})

	// Second session: reuse the writer. The writer must have reset its delta
	// state so the next array's first value isn't encoded as a delta from the
	// previous session's last value.
	second := columnartest.Array(t, types.KindInt64, &alloc,
		int64(10), int64(20), int64(30),
	)
	require.NoError(t, w.Append(second))

	secondResult, err := w.Flush(t.Context(), &store)
	require.NoError(t, err)

	secondReader, err := array.NewReader(&alloc, secondResult, &store)
	require.NoError(t, err)
	secondActual := readBatches(t, &alloc, secondReader, 8)
	columnartest.RequireArraysEqual(t, second, secondActual, memory.Bitmap{})
}

func TestDeltaCodec_WithZigZagBitpacked(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	var (
		spec = &array.SpecDelta{
			Data: &array.SpecZigZag{
				Data: &array.SpecBitpacked{BlockSize: 8, Widths: &array.SpecPlain{}},
			},
		}
		typ = &types.Int64{Nullable: false}
	)

	w, err := array.NewWriter(&alloc, spec, typ)
	require.NoError(t, err)

	input := columnartest.Array(t, types.KindInt64, &alloc,
		int64(100), int64(110), int64(105), int64(120), int64(115),
		int64(130), int64(125), int64(140), int64(135), int64(150),
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

func BenchmarkDeltaCodec(b *testing.B) {
	const valueCount = 1_000_000

	// Generate 1M realistic logging timestamps: unix nanosecond precision,
	// batched close together with inter-batch gaps.
	rnd := rand.New(rand.NewSource(42))
	timestamps := make([]int64, valueCount)

	ts := int64(1_700_000_000_000_000_000) // ~2023-11-14 in nanoseconds
	i := 0
	for i < valueCount {
		// Batch of 10-100 logs with small intra-batch spacing.
		batchSize := 10 + rnd.Intn(91)
		if i+batchSize > valueCount {
			batchSize = valueCount - i
		}
		for j := range batchSize {
			timestamps[i+j] = ts
			ts += int64(100 + rnd.Intn(901)) // 100ns-1µs between entries
		}
		i += batchSize
		ts += int64(1_000_000 + rnd.Intn(49_000_001)) // 1ms-50ms between batches
	}

	// Encode with Delta -> ZigZag -> Bitpacked.
	var (
		store buffer.MemoryStore

		spec = &array.SpecDelta{
			Data: &array.SpecZigZag{
				Data: &array.SpecBitpacked{BlockSize: 128, Widths: &array.SpecPlain{}},
			},
		}
		typ = &types.Int64{Nullable: false}
	)

	var alloc memory.Allocator
	w, err := array.NewWriter(&alloc, spec, typ)
	require.NoError(b, err)

	builder := columnar.NewNumberBuilder[int64](&alloc)
	builder.Grow(valueCount)
	for _, ts := range timestamps {
		builder.AppendValue(ts)
	}
	require.NoError(b, w.Append(builder.Build()))

	encoded, err := w.Flush(b.Context(), &store)
	require.NoError(b, err)

	batchSizes := []int{256, 1024, 4096}

	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("batch_size=%d", batchSize), func(b *testing.B) {
			var alloc memory.Allocator

			b.ReportAllocs()
			b.SetBytes(int64(valueCount) * 8)

			for b.Loop() {
				alloc.Reset()

				r, _ := array.NewReader(&alloc, encoded, &store)

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

				if decoded != valueCount {
					b.Fatalf("decoded %d values, expected %d", decoded, valueCount)
				}
			}

			elapsed := b.Elapsed()
			if elapsed > 0 {
				totalDecoded := int64(valueCount) * int64(b.N)
				b.ReportMetric(float64(totalDecoded)/elapsed.Seconds(), "rows/s")
			}
		})
	}
}
