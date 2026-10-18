package dataset

import (
	"bytes"
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
	require.NoError(t, enc.Flush())
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
		require.NoError(t, enc.Flush())
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

// benchmarkDeltaEncode measures the amortized cost of appending a value and
// periodically flushing a page's worth of values (deltaEncoder buffers
// appended values and only does its real work -- the DELTA_BINARY_PACKED
// encode -- inside Flush, so a benchmark that only calls Encode and never
// Flush would never exercise that cost).
func benchmarkDeltaEncode(b *testing.B, valueAt func(i int) int64) {
	const pageRows = 4096

	enc := newDeltaEncoder(streamio.Discard)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = enc.Encode(Int64Value(valueAt(i)))
		if (i+1)%pageRows == 0 {
			_ = enc.Flush()
			enc.Reset(streamio.Discard)
		}
	}
	_ = enc.Flush()
}

func Benchmark_deltaEncoder_Encode(b *testing.B) {
	b.Run("Sequential", func(b *testing.B) {
		benchmarkDeltaEncode(b, func(i int) int64 { return int64(i) })
	})

	b.Run("Largest delta", func(b *testing.B) {
		benchmarkDeltaEncode(b, func(i int) int64 {
			if i%2 == 0 {
				return 0
			}
			return math.MaxInt64
		})
	})

	b.Run("Random", func(b *testing.B) {
		rnd := rand.New(rand.NewSource(0))
		benchmarkDeltaEncode(b, func(int) int64 { return rnd.Int63() })
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
			require.NoError(b, enc.Flush())
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
			require.NoError(b, enc.Flush())
			return &buf
		},
		"random": func() *bytes.Buffer {
			var buf bytes.Buffer
			enc := newDeltaEncoder(&buf)

			rnd := rand.New(rand.NewSource(0))

			for i := 0; i < pageSize; i++ {
				_ = enc.Encode(Int64Value(rnd.Int63()))
			}
			require.NoError(b, enc.Flush())
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

// Benchmark_deltaDecoder_PartialRead measures the cost of reading only a
// small prefix of a large page -- the pattern behind the Reader/batch=100
// regression this encoding switch introduced: a batch-oriented decoder that
// decodes the whole page up front pays for every value in the page on the
// very first read, no matter how few are actually requested. A decoder that
// decodes incrementally (one DELTA_BINARY_PACKED block, i.e. up to 128
// values, at a time) should instead cost roughly the same regardless of how
// large the page is, since it only ever decodes as many blocks as needed to
// satisfy the requested count.
func Benchmark_deltaDecoder_PartialRead(b *testing.B) {
	readCounts := []int{64, 256}
	pageSizes := []int{1 << 10, 1 << 14, 1 << 18}

	for _, readCount := range readCounts {
		for _, pageSize := range pageSizes {
			b.Run(fmt.Sprintf("read=%d/pageSize=%d", readCount, pageSize), func(b *testing.B) {
				var buf bytes.Buffer
				enc := newDeltaEncoder(&buf)
				for i := 0; i < pageSize; i++ {
					require.NoError(b, enc.Encode(Int64Value(int64(i))))
				}
				require.NoError(b, enc.Flush())
				data := buf.Bytes()

				dec := newDeltaDecoder(nil)
				var alloc memory.Allocator

				for b.Loop() {
					alloc.Reset()
					dec.Reset(data)

					values, err := dec.Decode(&alloc, readCount)
					if err != nil && !errors.Is(err, io.EOF) {
						b.Fatalf("error decoding: %v", err)
					}
					if values.Len() != readCount {
						b.Fatalf("got %d values, want %d", values.Len(), readCount)
					}
				}
			})
		}
	}
}
