package dataset

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_bitmap(t *testing.T) {
	var buf bytes.Buffer

	var (
		enc = newBitmapEncoder(&buf)
		dec = newBitmapDecoder(nil)
	)

	count := 1500
	for range count {
		require.NoError(t, enc.Encode(Uint64Value(uint64(1))))
	}
	require.NoError(t, enc.Flush())
	dec.Reset(buf.Bytes())

	t.Logf("Buffer size: %d", buf.Len())

	actual, err := decodeValues(dec)
	require.NoError(t, err)
	require.Len(t, actual, count)
	for i := range count {
		require.Equal(t, uint64(1), actual[i].Uint64())
	}
}

func Test_bitmap_encodeN(t *testing.T) {
	var buf bytes.Buffer

	var (
		enc = newBitmapEncoder(&buf)
		dec = newBitmapDecoder(nil)
	)

	count := 1500
	require.NoError(t, enc.EncodeN(Uint64Value(1), uint64(count)))
	require.NoError(t, enc.Flush())
	dec.Reset(buf.Bytes())

	t.Logf("Buffer size: %d", buf.Len())

	actual, err := decodeValues(dec)
	require.NoError(t, err)
	require.Len(t, actual, count)
	for i := range count {
		require.Equal(t, uint64(1), actual[i].Uint64())
	}

	buf.Reset()
	enc.Reset(&buf)

	require.NoError(t, enc.Encode(Uint64Value(2)))      // start a new RLE run
	require.NoError(t, enc.EncodeN(Uint64Value(2), 99)) // append to the run

	require.Equal(t, enc.runLength, uint64(100))
	require.Equal(t, enc.runValue, uint64(2))

	require.NoError(t, enc.EncodeN(Uint64Value(3), 5)) // flush and start a new RLE run
	require.Equal(t, enc.runLength, uint64(5))
	require.Equal(t, enc.runValue, uint64(3))

	require.NoError(t, enc.EncodeN(Uint64Value(4), 2)) // switch to bitpacking
	require.Equal(t, enc.setSize, byte(7))

	require.NoError(t, enc.Flush())
	dec.Reset(buf.Bytes())

	t.Logf("Buffer size: %d", buf.Len())

	actual, err = decodeValues(dec)
	require.NoError(t, err)
	require.Len(t, actual, 100+5+2)

	for i := range 100 {
		require.Equal(t, uint64(2), actual[i].Uint64())
	}
	actual = actual[100:]

	for i := range 5 {
		require.Equal(t, uint64(3), actual[i].Uint64())
	}
	actual = actual[5:]

	for i := range 2 {
		require.Equal(t, uint64(4), actual[i].Uint64())
	}
}

func Test_bitmap_bitpacking(t *testing.T) {
	var buf bytes.Buffer

	var (
		enc    = newBitmapEncoder(&buf)
		dec    = newBitmapDecoder(nil)
		decBuf = make([]Value, batchSize)
	)

	expect := []uint64{0, 1, 2, 3, 4, 5, 6, 7}
	for _, v := range expect {
		require.NoError(t, enc.Encode(Uint64Value(v)))
	}
	require.NoError(t, enc.Flush())
	dec.Reset(buf.Bytes())

	var actual []uint64
	for {
		n, err := dec.Decode(decBuf[:batchSize])
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		for _, v := range decBuf[:n] {
			actual = append(actual, v.Uint64())
		}
	}
	require.NoError(t, enc.Flush())

	require.Equal(t, expect, actual)
}

func Test_bitmap_bitpacking_partial(t *testing.T) {
	var buf bytes.Buffer

	var (
		enc    = newBitmapEncoder(&buf)
		dec    = newBitmapDecoder(nil)
		decBuf = make([]Value, batchSize)
	)

	expect := []uint64{0, 1, 2, 3, 4}
	for _, v := range expect {
		require.NoError(t, enc.Encode(Uint64Value(v)))
	}
	require.NoError(t, enc.Flush())
	dec.Reset(buf.Bytes())

	var actual []uint64
	for {
		n, err := dec.Decode(decBuf[:batchSize])
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		for _, v := range decBuf[:n] {
			actual = append(actual, v.Uint64())
		}
	}

	require.Equal(t, expect, actual)
}

func Fuzz_bitmap(f *testing.F) {
	f.Add(int64(775972800), 1, 10)
	f.Add(int64(758350800), 8, 25)
	f.Add(int64(1718425412), 32, 50)
	f.Add(int64(1734130411), 64, 75)

	f.Fuzz(func(t *testing.T, seed int64, width int, count int) {
		if width < 1 || width > 64 {
			t.Skip()
		} else if count <= 0 {
			t.Skip()
		}

		rnd := rand.New(rand.NewSource(seed))

		var buf bytes.Buffer

		var (
			enc = newBitmapEncoder(&buf)
			dec = newBitmapDecoder(nil)
		)

		var numbers []uint64
		for range count {
			var mask uint64 = math.MaxUint64
			if width < 64 {
				mask = (1 << width) - 1
			}

			v := uint64(rnd.Int63()) & mask
			numbers = append(numbers, v)
			require.NoError(t, enc.Encode(Uint64Value(v)))
		}
		require.NoError(t, enc.Flush())
		dec.Reset(buf.Bytes())

		actual, err := decodeValues(dec)
		require.NoError(t, err)
		require.Len(t, actual, count)
		for i := range count {
			require.Equal(t, numbers[i], actual[i].Uint64())
		}
	})
}

func Fuzz_bitmap_EncodeN(f *testing.F) {
	f.Add(int64(775972800), 1000, 10)
	f.Add(int64(758350800), 500, 25)
	f.Add(int64(1734130411), 10000, 1000)

	f.Fuzz(func(t *testing.T, seed int64, count int, distinct int) {
		if count < 1 {
			t.Skip()
		} else if distinct < 1 || distinct*10 > count {
			// atmost 10% of the values can be distinct
			t.Skip()
		}

		var (
			buf     bytes.Buffer
			numbers []uint64

			rnd = rand.New(rand.NewSource(seed))
			enc = newBitmapEncoder(&buf)
			dec = newBitmapDecoder(nil)
		)

		var runLength int
		for range count {
			var v uint64
			// Decide if this position should have a distinct value or null (0)
			if rnd.Intn(count) < distinct {
				v = uint64(rnd.Int63()) + 1 // Use a non-zero value for distinct elements

				if runLength > 0 {
					require.NoError(t, enc.EncodeN(Uint64Value(0), uint64(runLength)))
					runLength = 0
				}

				// Encode the distinct value
				require.NoError(t, enc.Encode(Uint64Value(v)))
			} else {
				v = 0
				runLength++
			}

			numbers = append(numbers, v)
		}

		// Encode any remaining nulls
		if runLength > 0 {
			require.NoError(t, enc.EncodeN(Uint64Value(0), uint64(runLength)))
		}

		require.NoError(t, enc.Flush())
		dec.Reset(buf.Bytes())

		actual, err := decodeValues(dec)
		require.NoError(t, err)
		require.Len(t, actual, count)
		for i := range count {
			require.Equal(t, numbers[i], actual[i].Uint64())
		}
	})
}

func Benchmark_bitmapEncoder(b *testing.B) {
	b.Run("width=1", func(b *testing.B) { benchmarkBitmapEncoder(b, 1) })
	b.Run("width=3", func(b *testing.B) { benchmarkBitmapEncoder(b, 3) })
	b.Run("width=5", func(b *testing.B) { benchmarkBitmapEncoder(b, 5) })
	b.Run("width=8", func(b *testing.B) { benchmarkBitmapEncoder(b, 8) })
	b.Run("width=32", func(b *testing.B) { benchmarkBitmapEncoder(b, 32) })
	b.Run("width=64", func(b *testing.B) { benchmarkBitmapEncoder(b, 64) })
}

func benchmarkBitmapEncoder(b *testing.B, width int) {
	b.Run("variance=none", func(b *testing.B) {
		var cw countingWriter
		enc := newBitmapEncoder(&cw)

		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			_ = enc.Encode(Uint64Value(1))
		}
		_ = enc.Flush()

		b.ReportMetric(float64(cw.n), "encoded_bytes")
	})

	b.Run("variance=alternating", func(b *testing.B) {
		var cw countingWriter
		enc := newBitmapEncoder(&cw)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = enc.Encode(Uint64Value(uint64(i % width)))
		}
		_ = enc.Flush()

		b.ReportMetric(float64(cw.n), "encoded_bytes")
	})

	b.Run("variance=random", func(b *testing.B) {
		rnd := rand.New(rand.NewSource(0))

		var cw countingWriter
		enc := newBitmapEncoder(&cw)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = enc.Encode(Uint64Value(uint64(rnd.Int63()) % uint64(width)))
		}
		_ = enc.Flush()

		b.ReportMetric(float64(cw.n), "encoded_bytes")
	})
}

var bitmapDecoderSink Value

func Benchmark_bitmapDecoder_DecodeBatches(b *testing.B) {
	const valuesPerPage = 1 << 16

	type scenario struct {
		name       string
		valueCount int
		encoded    []byte
	}

	build := func(name string, valueCount int, valueAt func(i int) uint64) scenario {
		var buf bytes.Buffer
		enc := newBitmapEncoder(&buf)
		for i := 0; i < valueCount; i++ {
			require.NoError(b, enc.Encode(Uint64Value(valueAt(i))))
		}
		require.NoError(b, enc.Flush())
		return scenario{name: name, valueCount: valueCount, encoded: buf.Bytes()}
	}

	scenarios := []scenario{
		build("variance=rle", valuesPerPage, func(int) uint64 { return 1 }),
		build("variance=bitpack_width=3", valuesPerPage, func(i int) uint64 { return uint64(i % 8) }),
		func() scenario {
			rnd64 := rand.New(rand.NewSource(0))
			return build("variance=bitpack_width=64", valuesPerPage, func(int) uint64 { return rnd64.Uint64() | (1 << 63) })
		}(),
		func() scenario {
			rnd64 := rand.New(rand.NewSource(0))
			return build("variance=mixed_rle_and_bitpack", valuesPerPage, func(i int) uint64 {
				// Alternates between long RLE runs and bitpacked-ish data.
				if (i/1024)%2 == 0 {
					return 1
				}
				return rnd64.Uint64() | (1 << 63)
			})
		}(),
	}

	batchSizes := []int{256, 1024, 4096}

	for _, sc := range scenarios {
		b.Run(sc.name, func(b *testing.B) {
			b.Run(fmt.Sprintf("values_per_page=%d", sc.valueCount), func(b *testing.B) {
				for _, batchSize := range batchSizes {
					b.Run(fmt.Sprintf("batch_size=%d", batchSize), func(b *testing.B) {
						page := make([]Value, batchSize)
						dec := newBitmapDecoder(nil)

						b.ReportAllocs()
						decodedBytesPerOp := int64(sc.valueCount) * 8 // uint64 output
						b.SetBytes(decodedBytesPerOp)

						for b.Loop() {
							dec.Reset(sc.encoded)

							decoded := 0
							for {
								n, err := dec.Decode(page)
								if n > 0 {
									bitmapDecoderSink = page[n-1]
									decoded += n
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
							b.ReportMetric(float64(totalDecoded)/elapsed.Seconds()/1000/1000, "million_values/s")
						}
					})
				}
			})
		})
	}
}

func Benchmark_bitmap_EncodeN(b *testing.B) {
	// Test different run lengths
	runLengths := []int{1000, 10000, 100000, 1000000}

	for _, runLength := range runLengths {
		// Test Encode (repeated calls)
		b.Run(fmt.Sprintf("Encode_%d_times", runLength), func(b *testing.B) {
			var cw countingWriter
			enc := newBitmapEncoder(&cw)

			for i := 0; i < b.N; i++ {
				// Encode the same value multiple times
				for j := 0; j < runLength; j++ {
					_ = enc.Encode(Uint64Value(42))
				}
				_ = enc.Flush()
			}
		})

		// Test EncodeN (single call)
		b.Run(fmt.Sprintf("EncodeN_%d_times", runLength), func(b *testing.B) {
			var cw countingWriter
			enc := newBitmapEncoder(&cw)

			for i := 0; i < b.N; i++ {
				// Encode the same value once with count
				_ = enc.EncodeN(Uint64Value(42), uint64(runLength))
				_ = enc.Flush()
			}
		})
	}
}

type countingWriter struct {
	n int64
}

func (w *countingWriter) Write(p []byte) (n int, err error) {
	n = len(p)
	w.n += int64(n)
	return
}

func (w *countingWriter) WriteByte(_ byte) error {
	w.n++
	return nil
}

func decodeValues(dec *bitmapDecoder) ([]Value, error) {
	var (
		all    []Value
		decBuf = make([]Value, batchSize)
	)

	for {
		n, err := dec.Decode(decBuf[:batchSize])
		if n > 0 {
			all = append(all, decBuf[:n]...)
		}

		if errors.Is(err, io.EOF) {
			return all, nil
		} else if err != nil {
			return all, err
		}
	}
}
