package columnar_test

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestConcat_Null(t *testing.T) {
	var alloc memory.Allocator

	var in []columnar.Array
	for _, l := range []int{10, 5, 32} {
		validity := memory.MakeBitmap(&alloc, l)
		validity.AppendCount(false, l)
		in = append(in, columnar.MakeNull(validity))
	}

	out, err := columnar.Concat(&alloc, in)
	require.NoError(t, err)
	require.Equal(t, columnar.KindNull, out.Kind())
	require.Equal(t, 10+5+32, out.Len())
	require.Equal(t, 10+5+32, out.Nulls())
}

func TestConcat_Bool(t *testing.T) {
	var alloc memory.Allocator

	inputs := []boolBuilder{
		{values: []bool{true, false, false, true}, validity: nil},
		{values: nil, validity: nil},
		{values: []bool{false, true}, validity: []bool{true, false}},
	}

	var (
		expectValues   = []bool{true, false, false, true, false, true}
		expectValidity = []bool{true, true, true, true, true, false}
	)

	var in []columnar.Array
	for _, inputArray := range inputs {
		in = append(in, inputArray.Build(&alloc))
	}

	out, err := columnar.Concat(&alloc, in)
	require.NoError(t, err)
	require.Equal(t, columnar.KindBool, out.Kind())
	require.Equal(t, len(expectValues), out.Len())
	require.Equal(t, 1, out.Nulls())

	for i := range len(expectValues) {
		if !expectValidity[i] {
			require.True(t, out.IsNull(i), "expected null at index %d", i)
		} else {
			require.False(t, out.IsNull(i), "expected non-null at index %d", i)
			require.Equal(t, expectValues[i], out.(*columnar.Bool).Get(i), "expected %t at index %d", expectValues[i], i)
		}
	}
}

func TestConcat_Int64(t *testing.T) {
	var alloc memory.Allocator

	inputs := []int64Builder{
		{values: []int64{1, 2, 3, 4}, validity: nil},
		{values: nil, validity: nil},
		{values: []int64{5, 6}, validity: []bool{true, false}},
	}

	var (
		expectValues   = []int64{1, 2, 3, 4, 5, 6}
		expectValidity = []bool{true, true, true, true, true, false}
	)

	var in []columnar.Array
	for _, inputArray := range inputs {
		in = append(in, inputArray.Build(&alloc))
	}

	out, err := columnar.Concat(&alloc, in)
	require.NoError(t, err)
	require.Equal(t, columnar.KindInt64, out.Kind())
	require.Equal(t, len(expectValues), out.Len())
	require.Equal(t, 1, out.Nulls())

	for i := range len(expectValues) {
		if !expectValidity[i] {
			require.True(t, out.IsNull(i), "expected null at index %d", i)
		} else {
			require.False(t, out.IsNull(i), "expected non-null at index %d", i)
			require.Equal(t, expectValues[i], out.(*columnar.Int64).Get(i), "expected %d at index %d", expectValues[i], i)
		}
	}
}

func TestConcat_UTF8(t *testing.T) {
	var alloc memory.Allocator

	inputs := []utf8Builder{
		{values: []string{"hello", "world", "foo", "bar"}, validity: nil},
		{values: nil, validity: nil},
		{values: []string{"baz", "qux"}, validity: []bool{true, false}},
	}

	var (
		expectValues   = []string{"hello", "world", "foo", "bar", "baz", "qux"}
		expectValidity = []bool{true, true, true, true, true, false}
	)

	var in []columnar.Array
	for _, inputArray := range inputs {
		in = append(in, inputArray.Build(&alloc))
	}

	out, err := columnar.Concat(&alloc, in)
	require.NoError(t, err)
	require.Equal(t, columnar.KindUTF8, out.Kind())
	require.Equal(t, len(expectValues), out.Len())
	require.Equal(t, 1, out.Nulls())

	for i := range len(expectValues) {
		if !expectValidity[i] {
			require.True(t, out.IsNull(i), "expected null at index %d", i)
		} else {
			require.False(t, out.IsNull(i), "expected non-null at index %d", i)
			require.Equal(t, expectValues[i], string(out.(*columnar.UTF8).Get(i)), "expected %s at index %d", expectValues[i], i)
		}
	}
}

func BenchmarkConcat(b *testing.B) {
	b.Run("kind=Null", func(b *testing.B) {
		var alloc memory.Allocator

		var in []columnar.Array
		for range 128 {
			validity := memory.MakeBitmap(&alloc, 128)
			validity.AppendCount(false, 128)
			in = append(in, columnar.MakeNull(validity))
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
			values := memory.MakeBitmap(&alloc, 128)
			validity := memory.MakeBitmap(&alloc, 128)

			// Alternate values and validity for variety
			for j := range 128 {
				values.Append((i+j)%2 == 0)
				validity.Append(j%10 != 0) // Every 10th element is null
			}

			in = append(in, columnar.MakeBool(values, validity))
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
			validity := memory.MakeBitmap(&alloc, 128)

			// Alternate values and validity for variety
			for j := range 128 {
				values[j] = int64(i*128 + j)
				validity.Append(j%10 != 0) // Every 10th element is null
			}

			in = append(in, columnar.MakeInt64(values, validity))
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

		var builders []utf8Builder
		var totalDataBytes int64

		for range 128 {
			var builder utf8Builder

			// Generate 128 strings that total ~26KB
			for j := range 128 {
				// Random string length between 100-300 bytes (avg ~200 bytes)
				length := 100 + rng.Intn(201)
				str := make([]byte, length)
				for i := range str {
					// Generate printable ASCII characters (space to ~)
					str[i] = byte(32 + rng.Intn(95))
				}
				builder.values = append(builder.values, string(str))
				builder.validity = append(builder.validity, j%10 != 0) // Every 10th element is null
				totalDataBytes += int64(length)
			}

			builders = append(builders, builder)
		}

		var in []columnar.Array
		for _, builder := range builders {
			in = append(in, builder.Build(&alloc))
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

type boolBuilder struct {
	values   []bool
	validity []bool
}

func (b *boolBuilder) Build(alloc *memory.Allocator) columnar.Array {
	values := memory.MakeBitmap(alloc, len(b.values))
	values.AppendValues(b.values...)

	var validity memory.Bitmap
	if len(b.validity) > 0 {
		validity = memory.MakeBitmap(alloc, len(b.validity))
		validity.AppendValues(b.validity...)
	}

	return columnar.MakeBool(values, validity)
}

type int64Builder struct {
	values   []int64
	validity []bool
}

func (b *int64Builder) Build(alloc *memory.Allocator) columnar.Array {
	var validity memory.Bitmap
	if len(b.validity) > 0 {
		validity = memory.MakeBitmap(alloc, len(b.validity))
		validity.AppendValues(b.validity...)
	}

	return columnar.MakeInt64(b.values, validity)
}

type utf8Builder struct {
	values   []string
	validity []bool
}

func (b *utf8Builder) Build(alloc *memory.Allocator) columnar.Array {
	// Build data and offsets for UTF8 array
	var totalBytes int
	for _, s := range b.values {
		totalBytes += len(s)
	}

	data := memory.MakeBuffer[byte](alloc, totalBytes)
	offsets := memory.MakeBuffer[int32](alloc, len(b.values)+1)

	offsets.Append(0)
	for _, s := range b.values {
		data.Append([]byte(s)...)
		offsets.Append(int32(data.Len()))
	}

	var validity memory.Bitmap
	if len(b.validity) > 0 {
		validity = memory.MakeBitmap(alloc, len(b.validity))
		validity.AppendValues(b.validity...)
	}

	return columnar.MakeUTF8(data.Data(), offsets.Data(), validity)
}
