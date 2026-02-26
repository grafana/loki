package columnar_test

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestConcat_Null(t *testing.T) {
	var alloc memory.Allocator

	var in []columnar.Array
	for _, l := range []int{10, 5, 32} {
		values := make([]any, l)
		in = append(in, columnartest.Array(t, columnar.KindNull, &alloc, values...))
	}

	expect := columnartest.Array(t, columnar.KindNull, &alloc, make([]any, 10+5+32)...)
	actual, err := columnar.Concat(&alloc, in)
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t, expect, actual)
}

func TestConcat_Bool(t *testing.T) {
	var alloc memory.Allocator

	in := []columnar.Array{
		columnartest.Array(t, columnar.KindBool, &alloc, true, false, false, true),
		columnartest.Array(t, columnar.KindBool, &alloc),
		columnartest.Array(t, columnar.KindBool, &alloc, false, nil),
	}

	expect := columnartest.Array(t, columnar.KindBool, &alloc, true, false, false, true, false, nil)
	actual, err := columnar.Concat(&alloc, in)
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t, expect, actual)
}

func TestConcat_Int64(t *testing.T) {
	var alloc memory.Allocator

	in := []columnar.Array{
		columnartest.Array(t, columnar.KindInt64, &alloc, 1, 2, 3, 4),
		columnartest.Array(t, columnar.KindInt64, &alloc),
		columnartest.Array(t, columnar.KindInt64, &alloc, 5, nil),
	}

	expect := columnartest.Array(t, columnar.KindInt64, &alloc, 1, 2, 3, 4, 5, nil)
	actual, err := columnar.Concat(&alloc, in)
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t, expect, actual)
}

func TestConcat_UTF8(t *testing.T) {
	var alloc memory.Allocator

	in := []columnar.Array{
		columnartest.Array(t, columnar.KindUTF8, &alloc, "hello", "world", "foo", "bar"),
		columnartest.Array(t, columnar.KindUTF8, &alloc),
		columnartest.Array(t, columnar.KindUTF8, &alloc, "baz", nil),
	}

	expect := columnartest.Array(
		t, columnar.KindUTF8, &alloc,
		"hello", "world", "foo", "bar", "baz", nil,
	)

	actual, err := columnar.Concat(&alloc, in)
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t, expect, actual)
}

func TestConcat_UTF8_Slices(t *testing.T) {
	// Variable-sized types like UTF8 don't slice as "naturally" as fixed-size
	// types: it slices the offsets array but not the data array. Because of
	// this, we need to add a special test for concatenating UTF8 to ensure that
	// it handles it properly.

	var alloc memory.Allocator

	in := []columnar.Array{
		columnartest.Array(t, columnar.KindUTF8, &alloc, "hello", "world", "foo", "bar").Slice(1, 3),
		columnartest.Array(t, columnar.KindUTF8, &alloc),
		columnartest.Array(t, columnar.KindUTF8, &alloc, "baz", nil),
	}

	expect := columnartest.Array(
		t, columnar.KindUTF8, &alloc,
		"world", "foo", "baz", nil,
	)

	actual, err := columnar.Concat(&alloc, in)
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t, expect, actual)
}

func BenchmarkConcat(b *testing.B) {
	b.Run("kind=Null", func(b *testing.B) {
		var alloc memory.Allocator

		var in []columnar.Array
		for range 128 {
			validity := memory.NewBitmap(&alloc, 128)
			validity.AppendCount(false, 128)
			in = append(in, columnar.NewNull(validity))
		}

		var loopAlloc memory.Allocator

		for b.Loop() {
			loopAlloc.Reset()

			_, err := columnar.Concat(&loopAlloc, in)
			if err != nil {
				b.Fatal(err)
			}
		}

		b.SetBytes(128 * 128 / 8) // 128 arrays × 128 elements = 16,384 bits = 2,048 bytes
		b.ReportMetric(float64(b.N*128*128)/b.Elapsed().Seconds(), "values/s")
	})

	b.Run("kind=Bool", func(b *testing.B) {
		var alloc memory.Allocator

		var in []columnar.Array
		for i := range 128 {
			values := memory.NewBitmap(&alloc, 128)
			validity := memory.NewBitmap(&alloc, 128)

			// Alternate values and validity for variety
			for j := range 128 {
				values.Append((i+j)%2 == 0)
				validity.Append(j%10 != 0) // Every 10th element is null
			}

			in = append(in, columnar.NewBool(values, validity))
		}

		var loopAlloc memory.Allocator

		for b.Loop() {
			loopAlloc.Reset()

			_, err := columnar.Concat(&loopAlloc, in)
			if err != nil {
				b.Fatal(err)
			}
		}

		b.SetBytes(128 * 128 * 2 / 8) // 128 arrays × 128 elements × 2 bitmaps = 32,768 bits = 4,096 bytes
		b.ReportMetric(float64(b.N*128*128)/b.Elapsed().Seconds(), "values/s")
	})

	b.Run("kind=Int64", func(b *testing.B) {
		var alloc memory.Allocator

		var in []columnar.Array
		for i := range 128 {
			values := make([]int64, 128)
			validity := memory.NewBitmap(&alloc, 128)

			// Alternate values and validity for variety
			for j := range 128 {
				values[j] = int64(i*128 + j)
				validity.Append(j%10 != 0) // Every 10th element is null
			}

			in = append(in, columnar.NewNumber[int64](values, validity))
		}

		var loopAlloc memory.Allocator

		for b.Loop() {
			loopAlloc.Reset()

			_, err := columnar.Concat(&loopAlloc, in)
			if err != nil {
				b.Fatal(err)
			}
		}

		b.SetBytes(128*128*8 + 128*128/8) // Add the 64-bit value arrays with the bitpacked validity bitmap.
		b.ReportMetric(float64(b.N*128*128)/b.Elapsed().Seconds(), "values/s")
	})

	b.Run("kind=UTF8", func(b *testing.B) {
		var alloc memory.Allocator

		rng := rand.New(rand.NewSource(42))

		var builders []*columnar.UTF8Builder
		var totalDataBytes int64

		for range 128 {
			builder := columnar.NewUTF8Builder(&alloc)

			// Generate 128 strings that total ~26KB
			for j := range 128 {
				// Every 10th element is null.
				if j%10 == 0 {
					builder.AppendNull()
					continue
				}

				// Random string length between 100-300 bytes (avg ~200 bytes)
				length := 100 + rng.Intn(201)
				str := make([]byte, length)
				for i := range str {
					// Generate printable ASCII characters (space to ~)
					str[i] = byte(32 + rng.Intn(95))
				}

				builder.AppendValue(str)
				totalDataBytes += int64(length)
			}

			builders = append(builders, builder)
		}

		var in []columnar.Array
		for _, builder := range builders {
			in = append(in, builder.Build())
		}

		var loopAlloc memory.Allocator

		for b.Loop() {
			loopAlloc.Reset()

			_, err := columnar.Concat(&loopAlloc, in)
			if err != nil {
				b.Fatal(err)
			}
		}

		// UTF8 arrays have string data + offsets + validity
		// Data: totalDataBytes
		// Offsets: 128 arrays × 129 offsets × 4 bytes = 66,048 bytes
		// Validity: 16,384 bits / 8 = 2,048 bytes
		b.SetBytes(totalDataBytes + int64(128*129*4) + int64(128*128/8))
		b.ReportMetric(float64(b.N*128*128)/b.Elapsed().Seconds(), "values/s")
	})
}
