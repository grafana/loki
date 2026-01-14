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

	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
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
		enc    = newDeltaEncoder(&buf)
		dec    = newDeltaDecoder(&buf)
		decBuf = make([]Value, batchSize)
	)

	for _, num := range numbers {
		require.NoError(t, enc.Encode(Int64Value(num)))
	}

	var actual []int64
	for {
		n, err := dec.Decode(decBuf[:batchSize])
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		for _, v := range decBuf[:n] {
			actual = append(actual, v.Int64())
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
			enc    = newDeltaEncoder(&buf)
			dec    = newDeltaDecoder(&buf)
			decBuf = make([]Value, batchSize)
		)

		var numbers []int64
		for i := 0; i < count; i++ {
			v := rnd.Int63()
			numbers = append(numbers, v)
			require.NoError(t, enc.Encode(Int64Value(v)))
		}

		var actual []int64
		for {
			n, err := dec.Decode(decBuf[:batchSize])
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)
			for _, v := range decBuf[:n] {
				actual = append(actual, v.Int64())
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
				decBuf := make([]Value, batchSize)
				reader := bytes.NewReader(buf.Bytes())
				dec := newDeltaDecoder(reader)

				valuesRead := 0
				for b.Loop() {
					reader.Reset(buf.Bytes())
					dec.Reset(reader)
					for {
						n, err := dec.Decode(decBuf)
						valuesRead += n
						if err != nil && errors.Is(err, io.EOF) {
							break
						} else if err != nil {
							b.Fatal(err)
						}
					}
				}

				b.SetBytes(int64(pageSize * int(unsafe.Sizeof(int64(0)))))
				b.ReportMetric(float64(valuesRead)/float64(b.Elapsed().Seconds()), "rows/s")
			})
		}
	}
}
