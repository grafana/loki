package dataset

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

func Test_bitmap(t *testing.T) {
	tt := []string{
		"1",
		"0",
		"111",
		"101",
		"11111111",
		"111111111111111111111111111111111111111111111111111111111111111111", // 66
		"01010",
	}

	for _, tc := range tt {
		expected := tc
		t.Run(expected, func(t *testing.T) {
			var buf bytes.Buffer
			var (
				enc = newBitmapEncoder(&buf)
				dec = newBitmapDecoder(nil)
			)
			parts := strings.Split(expected, "")
			for _, b := range parts {
				val := uint64(0)
				if b == "1" {
					val = 1
				}
				require.NoError(t, enc.Encode(Uint64Value(val)))
			}
			require.NoError(t, enc.Flush())
			dec.Reset(buf.Bytes())
			a := memory.Allocator{}
			actual := decodeBitmapValues(t, dec, &a, 64)

			sb := strings.Builder{}
			for _, b := range actual {
				if b {
					sb.WriteString("1")
				} else {
					sb.WriteString("0")
				}
			}
			actualStr := sb.String()
			require.Equal(t, expected, actualStr)
		})
	}
}

func Test_bitmapDecoder_TruncatedData(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "rle_missing_value",
			data: []byte{0x02}, // rle run length 1, missing value
		},
		{
			name: "bitpack_missing_set",
			data: []byte{0x81}, // bitpack header for 1 set, missing data byte
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dec := newBitmapDecoder(tc.data)
			var alloc memory.Allocator

			var (
				res any
				err error
			)
			require.NotPanics(t, func() {
				res, err = dec.Decode(&alloc, 8)
			})

			bm, ok := res.(*columnar.Bool)
			require.True(t, ok)
			require.ErrorIs(t, err, io.EOF)
			require.Zero(t, bm.Len())
		})
	}
}

func decodeBitmapValues(t *testing.T, dec *bitmapDecoder, alloc *memory.Allocator, batchSize int) []bool {
	t.Helper()

	var actual []bool
	for {
		res, err := dec.Decode(alloc, batchSize)
		bm, ok := res.(*columnar.Bool)
		require.True(t, ok)
		for i := range bm.Len() {
			actual = append(actual, bm.Get(i))
		}
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}

	return actual
}

func Fuzz_bitmap(f *testing.F) {
	f.Add(int64(775972800), 10)
	f.Add(int64(758350800), 25)
	f.Add(int64(1718425412), 50)
	f.Add(int64(1734130411), 75)

	f.Fuzz(func(t *testing.T, seed int64, count int) {
		if count <= 0 {
			t.Skip()
		}

		rnd := rand.New(rand.NewSource(seed))

		var buf bytes.Buffer

		var (
			enc = newBitmapEncoder(&buf)
			dec = newBitmapDecoder(nil)
		)

		expected := make([]bool, 0, count)
		for range count {
			v := rnd.Intn(2) == 0
			expected = append(expected, v)
			uv := uint64(0)
			if v {
				uv = 1
			}
			require.NoError(t, enc.Encode(Uint64Value(uv)))
		}
		require.NoError(t, enc.Flush())
		dec.Reset(buf.Bytes())

		var alloc memory.Allocator
		actual := decodeBitmapValues(t, dec, &alloc, 64)
		require.Len(t, actual, count)
		require.Equal(t, expected, actual)
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
			// at most 10% of the values can be true
			t.Skip()
		}

		var (
			buf bytes.Buffer

			rnd = rand.New(rand.NewSource(seed))
			enc = newBitmapEncoder(&buf)
			dec = newBitmapDecoder(nil)
		)

		expected := make([]bool, 0, count)

		var runLength int
		for range count {
			if rnd.Intn(count) < distinct {
				if runLength > 0 {
					require.NoError(t, enc.EncodeN(Uint64Value(0), uint64(runLength)))
					runLength = 0
				}

				require.NoError(t, enc.Encode(Uint64Value(1)))
				expected = append(expected, true)
			} else {
				expected = append(expected, false)
				runLength++
			}
		}

		if runLength > 0 {
			require.NoError(t, enc.EncodeN(Uint64Value(0), uint64(runLength)))
		}

		require.NoError(t, enc.Flush())
		dec.Reset(buf.Bytes())

		var alloc memory.Allocator
		actual := decodeBitmapValues(t, dec, &alloc, 64)
		require.Len(t, actual, count)
		require.Equal(t, expected, actual)
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

func Benchmark_bitmapDecoder_DecodeBatches(b *testing.B) {
	const valuesPerPage = 1 << 16

	type scenario struct {
		name       string
		valueCount int
		encoded    []byte
	}

	build := func(name string, valueCount int, valueAt func(i int) bool) scenario {
		var buf bytes.Buffer
		enc := newBitmapEncoder(&buf)
		for i := 0; i < valueCount; i++ {
			boolVal := valueAt(i)
			val := Uint64Value(0)
			if boolVal {
				val = Uint64Value(1)
			}
			require.NoError(b, enc.Encode(val))
		}
		require.NoError(b, enc.Flush())
		return scenario{name: name, valueCount: valueCount, encoded: buf.Bytes()}
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
						dec := newBitmapDecoder(nil)

						b.ReportAllocs()
						decodedBytesPerOp := int64(sc.valueCount) / 8 // Each value is one bit
						b.SetBytes(decodedBytesPerOp)

						for b.Loop() {
							alloc.Reset()
							dec.Reset(sc.encoded)

							decoded := 0
							for {
								res, err := dec.Decode(&alloc, batchSize)
								bm := res.(*columnar.Bool)
								decoded += bm.Len()

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
