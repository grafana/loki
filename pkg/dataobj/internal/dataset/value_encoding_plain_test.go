package dataset

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
)

var testStrings = []string{
	"hello",
	"world",
	"foo",
	"bar",
	"baz",
}

var batchSize = 64

func Test_plainBytesEncoder(t *testing.T) {
	var buf bytes.Buffer

	var (
		enc    = newPlainBytesEncoder(&buf)
		dec    = newPlainBytesDecoder(nil)
		decBuf = make([]Value, batchSize)
	)

	for _, v := range testStrings {
		require.NoError(t, enc.Encode(BinaryValue([]byte(v))))
	}
	dec.Reset(buf.Bytes())

	var out []string

	for {
		n, err := dec.Decode(decBuf[:batchSize])
		if errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		for _, v := range decBuf[:n] {
			out = append(out, string(v.Binary()))
		}
	}

	require.Equal(t, testStrings, out)
}

func Test_plainBytesEncoder_reusingValues(t *testing.T) {
	var buf bytes.Buffer

	var (
		enc    = newPlainBytesEncoder(&buf)
		dec    = newPlainBytesDecoder(nil)
		decBuf = make([]Value, batchSize)
	)

	for _, v := range testStrings {
		require.NoError(t, enc.Encode(BinaryValue([]byte(v))))
	}
	dec.Reset(buf.Bytes())

	for i := range decBuf {
		decBuf[i] = BinaryValue(make([]byte, 64))
	}

	var out []string

	for {
		n, err := dec.Decode(decBuf[:batchSize])
		if errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		for _, v := range decBuf[:n] {
			out = append(out, string(v.Binary()))
		}
	}

	require.Equal(t, testStrings, out)
}

func Benchmark_plainBytesEncoder_Append(b *testing.B) {
	enc := newPlainBytesEncoder(streamio.Discard)

	for i := 0; i < b.N; i++ {
		for _, v := range testStrings {
			_ = enc.Encode(BinaryValue([]byte(v)))
		}
	}
}

func Benchmark_plainBytesDecoder_Decode(b *testing.B) {
	const pageSize = 2_000_000

	type scenario struct {
		name       string
		makeValues func() []Value
	}

	scenarios := []scenario{
		{
			name: "constant-size",
			makeValues: func() []Value {
				var result []Value

				const valueSize = 100

				rnd := rand.New(rand.NewSource(0))

				var totalSize int
				for totalSize+valueSize < pageSize {
					value := make([]byte, valueSize)
					_, _ = rnd.Read(value)

					result = append(result, BinaryValue(value))
					totalSize += valueSize
				}

				return result
			},
		},
		{
			name: "variable-size",
			makeValues: func() []Value {
				var result []Value

				const (
					minSize = 16
					maxSize = 1024
				)

				rnd := rand.New(rand.NewSource(0))

				var totalSize int
				for totalSize < pageSize {
					valueSize := minSize + rnd.Intn(maxSize-minSize+1)
					if rem := pageSize - totalSize; valueSize > rem {
						valueSize = rem
					}

					value := make([]byte, valueSize)
					_, _ = rnd.Read(value)
					result = append(result, BinaryValue(value))
					totalSize += valueSize
				}

				return result
			},
		},
	}

	for _, scenario := range scenarios {
		benchmarkName := fmt.Sprintf("scenario=%s", scenario.name)
		b.Run(benchmarkName, func(b *testing.B) {
			var buf bytes.Buffer

			var (
				enc = newPlainBytesEncoder(&buf)
			)

			var totalSize, totalCount int
			for _, value := range scenario.makeValues() {
				totalSize += len(value.Binary())
				totalCount++
				require.NoError(b, enc.Encode(value))
			}

			decBuf := make([]Value, totalCount)

			dec := newPlainBytesDecoder(nil)

			var totalRows int
			for b.Loop() {
				dec.Reset(buf.Bytes())

				for {
					n, err := dec.Decode(decBuf)
					totalRows += n
					if err != nil && errors.Is(err, io.EOF) {
						break
					} else if err != nil {
						b.Fatal(err)
					}
				}
			}

			b.SetBytes(int64(totalSize))
			b.ReportMetric(float64(totalRows)/b.Elapsed().Seconds(), "rows/s")
		})
	}
}
