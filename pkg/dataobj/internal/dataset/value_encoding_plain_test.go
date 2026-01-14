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
		dec    = newPlainBytesDecoder(&buf)
		decBuf = make([]Value, batchSize)
	)

	for _, v := range testStrings {
		require.NoError(t, enc.Encode(BinaryValue([]byte(v))))
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

func Test_plainBytesEncoder_partialRead(t *testing.T) {
	var buf bytes.Buffer

	var (
		enc    = newPlainBytesEncoder(&buf)
		dec    = newPlainBytesDecoder(&oneByteReader{&buf})
		decBuf = make([]Value, batchSize)
	)

	for _, v := range testStrings {
		require.NoError(t, enc.Encode(BinaryValue([]byte(v))))
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

func Test_plainBytesEncoder_reusingValues(t *testing.T) {
	var buf bytes.Buffer

	var (
		enc    = newPlainBytesEncoder(&buf)
		dec    = newPlainBytesDecoder(&buf)
		decBuf = make([]Value, batchSize)
	)

	for _, v := range testStrings {
		require.NoError(t, enc.Encode(BinaryValue([]byte(v))))
	}

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

	batchSizes := []int{1024, 2048, 4096, 8192}

	for _, scenario := range scenarios {
		for _, batchSize := range batchSizes {
			benchmarkName := fmt.Sprintf("scenario=%s/batch-size=%d", scenario.name, batchSize)
			b.Run(benchmarkName, func(b *testing.B) {
				var buf bytes.Buffer

				var (
					enc = newPlainBytesEncoder(&buf)
				)

				var totalSize int
				for _, value := range scenario.makeValues() {
					totalSize += len(value.Binary())
					require.NoError(b, enc.Encode(value))
				}

				decBuf := make([]Value, batchSize)

				r := bytes.NewReader(buf.Bytes())
				dec := newPlainBytesDecoder(r)

				var totalRows int
				for b.Loop() {
					r.Reset(buf.Bytes())
					dec.Reset(r) // Not necessary to reset both, but we do it anyway to guarantee a fresh state.

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
}

// oneByteReader is like iotest.OneByteReader but it supports ReadByte.
type oneByteReader struct {
	r streamio.Reader
}

func (r *oneByteReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return r.r.Read(p[0:1])
}

func (r *oneByteReader) ReadByte() (byte, error) {
	return r.r.ReadByte()
}
