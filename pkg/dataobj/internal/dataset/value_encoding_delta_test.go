package dataset

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/memory"
)

// malformedDeltaData holds the value 1234, which is valid, followed by a
// malformed varint.
var malformedDeltaData = []struct {
	name string
	data []byte
	err  string
}{
	{
		name: "the data ends inside a varint",
		data: append(binary.AppendVarint(nil, 1234), 0x80),
		err:  "delta: truncated value at offset 2",
	},
	{
		name: "a varint overflows 64 bits",
		data: append(binary.AppendVarint(nil, 1234), append(bytes.Repeat([]byte{0xff}, 9), 0x02)...),
		err:  "delta: malformed value at offset 2",
	},
}

func Test_delta(t *testing.T) {
	numbers := []int64{
		1234,
		543,
		2345,
		1432,
	}

	var buf bytes.Buffer

	var (
		enc   = newDeltaEncoder(&buf)
		dec   = newDeltaDecoder(nil)
		alloc = memory.Allocator{}
	)

	for _, num := range numbers {
		require.NoError(t, enc.Encode(Int64Value(num)))
	}
	dec.Reset(buf.Bytes())

	var actual []int64
	for {
		values, err := dec.Decode(&alloc, batchSize)
		if !errors.Is(err, io.EOF) {
			require.NoError(t, err)
		}
		actual = append(actual, values.(*columnar.Number[int64]).Values()...)
		if err != nil {
			break
		}
	}

	require.Equal(t, numbers, actual)
}

func Test_deltaDecoder_Decode(t *testing.T) {
	for _, tc := range malformedDeltaData {
		t.Run("returns an error and no values when "+tc.name, func(t *testing.T) {
			var alloc memory.Allocator

			arr, err := newDeltaDecoder(tc.data).Decode(&alloc, 5)
			require.EqualError(t, err, tc.err)
			require.Nil(t, arr)
		})
	}
}

func Test_deltaDecoder_Skip(t *testing.T) {
	numbers := []int64{1234, 543, -2345, 1432, 99, -5}

	decodeDeltaNumbers := func(t *testing.T, dec *deltaDecoder, count int) []int64 {
		t.Helper()

		var alloc memory.Allocator
		arr, err := dec.Decode(&alloc, count)
		require.NoError(t, err)
		return arr.(*columnar.Number[int64]).Values()
	}

	t.Run("decodes the values after the skipped ones", func(t *testing.T) {
		dec := newDeltaDecoder(encodeDeltaNumbers(t, numbers...))

		skipped, err := dec.Skip(3)
		require.NoError(t, err)
		require.Equal(t, 3, skipped)

		require.Equal(t, numbers[3:], decodeDeltaNumbers(t, dec, len(numbers)-3))
	})

	t.Run("skips no value when count is zero or negative", func(t *testing.T) {
		dec := newDeltaDecoder(encodeDeltaNumbers(t, numbers...))

		for _, count := range []int{0, -1} {
			skipped, err := dec.Skip(count)
			require.NoError(t, err)
			require.Equal(t, 0, skipped)
		}

		require.Equal(t, numbers, decodeDeltaNumbers(t, dec, len(numbers)))
	})

	t.Run("skips fewer values and returns no error when the data ends first", func(t *testing.T) {
		dec := newDeltaDecoder(encodeDeltaNumbers(t, numbers...))

		skipped, err := dec.Skip(len(numbers) + 4)
		require.NoError(t, err)
		require.Equal(t, len(numbers), skipped)

		var alloc memory.Allocator
		arr, err := dec.Decode(&alloc, 1)
		require.ErrorIs(t, err, io.EOF)
		require.Zero(t, arr.Len())
	})

	t.Run("continues from the previous position when Skip follows Decode and Skip", func(t *testing.T) {
		dec := newDeltaDecoder(encodeDeltaNumbers(t, numbers...))

		require.Equal(t, numbers[:2], decodeDeltaNumbers(t, dec, 2))

		skipped, err := dec.Skip(1)
		require.NoError(t, err)
		require.Equal(t, 1, skipped)

		skipped, err = dec.Skip(2)
		require.NoError(t, err)
		require.Equal(t, 2, skipped)

		require.Equal(t, numbers[5:], decodeDeltaNumbers(t, dec, 1))
	})

	for _, tc := range malformedDeltaData {
		t.Run("returns an error and the number of values before it when "+tc.name, func(t *testing.T) {
			skipped, err := newDeltaDecoder(tc.data).Skip(5)
			require.EqualError(t, err, tc.err)
			require.Equal(t, 1, skipped)
		})
	}

	t.Run("decodes the written values for random skip and read sizes", func(t *testing.T) {
		const valueCount = 2000

		rnd := rand.New(rand.NewSource(1))
		want := make([]int64, valueCount)
		for i := range want {
			want[i] = rnd.Int63() - rnd.Int63()
		}
		dec := newDeltaDecoder(encodeDeltaNumbers(t, want...))

		var (
			alloc memory.Allocator
			row   int
		)
		for row < valueCount {
			skipCount := rnd.Intn(40)
			skipped, err := dec.Skip(skipCount)
			require.NoError(t, err)
			require.Equal(t, min(skipCount, valueCount-row), skipped)
			row += skipped

			arr, err := dec.Decode(&alloc, 1+rnd.Intn(10))
			if !errors.Is(err, io.EOF) {
				require.NoError(t, err)
			}
			decoded := arr.(*columnar.Number[int64]).Values()
			require.Equal(t, want[row:row+len(decoded)], decoded)
			row += len(decoded)
			if err != nil {
				break
			}
		}
		require.Equal(t, valueCount, row)
	})
}

func Fuzz_delta(f *testing.F) {
	f.Add(int64(775972800), 10)
	f.Add(int64(758350800), 25)

	f.Fuzz(func(t *testing.T, seed int64, count int) {
		if count <= 0 {
			t.Skip()
		}

		rnd := rand.New(rand.NewSource(seed))

		var buf bytes.Buffer

		var (
			enc   = newDeltaEncoder(&buf)
			dec   = newDeltaDecoder(nil)
			alloc = memory.Allocator{}
		)

		var numbers []int64
		for i := 0; i < count; i++ {
			v := rnd.Int63()
			numbers = append(numbers, v)
			require.NoError(t, enc.Encode(Int64Value(v)))
		}
		dec.Reset(buf.Bytes())

		var actual []int64
		for {
			values, err := dec.Decode(&alloc, batchSize)
			if err != nil && !errors.Is(err, io.EOF) {
				t.Fatalf("error decoding: %v", err)
			}
			actual = append(actual, values.(*columnar.Number[int64]).Values()...)
			if errors.Is(err, io.EOF) {
				break
			}
		}

		require.Equal(t, numbers, actual)
	})
}

func Benchmark_deltaEncoder_Encode(b *testing.B) {
	b.Run("Sequential", func(b *testing.B) {
		enc := newDeltaEncoder(streamio.Discard)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = enc.Encode(Int64Value(int64(i)))
		}
	})

	b.Run("Largest delta", func(b *testing.B) {
		enc := newDeltaEncoder(streamio.Discard)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if i%2 == 0 {
				_ = enc.Encode(Int64Value(0))
			} else {
				_ = enc.Encode(Int64Value(math.MaxInt64))
			}
		}
	})

	b.Run("Random", func(b *testing.B) {
		rnd := rand.New(rand.NewSource(0))
		enc := newDeltaEncoder(streamio.Discard)

		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			_ = enc.Encode(Int64Value(rnd.Int63()))
		}
	})
}

func Benchmark_deltaDecoder_Decode(b *testing.B) {
	pageSize := 1 << 16

	scenarios := map[string]func() *bytes.Buffer{
		"sequential": func() *bytes.Buffer {
			var buf bytes.Buffer

			enc := newDeltaEncoder(&buf)

			for i := 0; i < pageSize; i++ {
				err := enc.Encode(Int64Value(int64(i)))
				require.NoError(b, err)
			}
			return &buf
		},
		"largest delta": func() *bytes.Buffer {
			var buf bytes.Buffer
			enc := newDeltaEncoder(&buf)
			for i := 0; i < pageSize; i++ {
				if i%2 == 0 {
					_ = enc.Encode(Int64Value(0))
				} else {
					_ = enc.Encode(Int64Value(math.MaxInt64))
				}
			}
			return &buf
		},
		"random": func() *bytes.Buffer {
			var buf bytes.Buffer
			enc := newDeltaEncoder(&buf)

			rnd := rand.New(rand.NewSource(0))

			for i := 0; i < pageSize; i++ {
				_ = enc.Encode(Int64Value(rnd.Int63()))
			}
			return &buf
		},
	}

	batchSizes := []int{256, 1024, 4096}

	for datasetName, makeDataset := range scenarios {
		for _, batchSize := range batchSizes {
			b.Run(fmt.Sprintf("%s/batchSize=%d", datasetName, batchSize), func(b *testing.B) {
				buf := makeDataset()
				dec := newDeltaDecoder(nil)

				var alloc memory.Allocator

				valuesRead := 0
				for b.Loop() {
					alloc.Reset()
					dec.Reset(buf.Bytes())

					for {
						values, err := dec.Decode(&alloc, batchSize)
						valuesRead += values.Len()
						if err != nil && errors.Is(err, io.EOF) {
							break
						} else if err != nil {
							b.Fatalf("error decoding: %v", err)
						}
					}
				}

				b.SetBytes(int64(pageSize * int(unsafe.Sizeof(int64(0)))))
				b.ReportMetric(float64(valuesRead)/float64(b.Elapsed().Seconds()), "rows/s")
			})
		}
	}
}

// Benchmark_deltaDecoder_Skip alternates between skipping a gap of values and
// decoding a few. It reclaims the allocator before each Skip and Decode pair, as
// the row reader does between reads.
func Benchmark_deltaDecoder_Skip(b *testing.B) {
	const (
		pageSize = 1 << 16
		readRows = 10
	)

	scenarios := map[string]func(rnd *rand.Rand, i int) int64{
		"sequential": func(_ *rand.Rand, i int) int64 { return int64(i) },
		"random":     func(rnd *rand.Rand, _ int) int64 { return rnd.Int63() },
	}

	for scenarioName, value := range scenarios {
		for _, gap := range []int{10, 100, 1000} {
			b.Run(fmt.Sprintf("scenario=%s/gap=%d", scenarioName, gap), func(b *testing.B) {
				rnd := rand.New(rand.NewSource(0))
				numbers := make([]int64, pageSize)
				for i := range numbers {
					numbers[i] = value(rnd, i)
				}
				data := encodeDeltaNumbers(b, numbers...)
				dec := newDeltaDecoder(data)

				var alloc memory.Allocator

				b.ReportAllocs()

				var totalRows int
				for b.Loop() {
					dec.Reset(data)

					for row := 0; row < pageSize; row += readRows + gap {
						alloc.Reclaim()

						if _, err := dec.Skip(gap); err != nil {
							b.Fatal(err)
						}

						arr, err := dec.Decode(&alloc, readRows)
						if arr != nil {
							totalRows += arr.Len()
						}
						if err != nil && !errors.Is(err, io.EOF) {
							b.Fatal(err)
						}
					}
				}

				b.SetBytes(int64(pageSize * int(unsafe.Sizeof(int64(0)))))
				b.ReportMetric(float64(totalRows)/float64(b.N), "rows/op")
			})
		}
	}
}

func encodeDeltaNumbers(t testing.TB, numbers ...int64) []byte {
	t.Helper()

	var buf bytes.Buffer
	enc := newDeltaEncoder(&buf)
	for _, n := range numbers {
		require.NoError(t, enc.Encode(Int64Value(n)))
	}
	return buf.Bytes()
}
