package dataset

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/memory"
)

var testStrings = []string{
	"hello",
	"world",
	"foo",
	"bar",
	"baz",
}

var batchSize = 64

// malformedPlainData holds the value "ab", which is valid, followed by a
// malformed value.
var malformedPlainData = []struct {
	name string
	data []byte
	err  string
}{
	{
		name: "the data ends inside a length prefix",
		data: []byte{2, 'a', 'b', 0x80},
		err:  "plain: truncated value length at offset 3",
	},
	{
		name: "a length prefix is longer than the longest uvarint",
		data: append([]byte{2, 'a', 'b'}, bytes.Repeat([]byte{0xff}, binary.MaxVarintLen64+2)...),
		err:  "plain: malformed value length at offset 3",
	},
	{
		name: "a value runs past the end of the data",
		data: []byte{2, 'a', 'b', 2, 'c'},
		err:  "plain: value of 2 bytes at offset 3 runs past the end of the data",
	},
	{
		name: "a length does not fit in an int",
		data: append(binary.AppendUvarint([]byte{2, 'a', 'b'}, 1<<63), 'c'),
		err:  "plain: value of 9223372036854775808 bytes at offset 3 runs past the end of the data",
	},
}

func Test_plainBytesEncoder(t *testing.T) {
	var buf bytes.Buffer

	enc := newPlainBytesEncoder(&buf)
	for _, v := range testStrings {
		require.NoError(t, enc.Encode(BinaryValue([]byte(v))))
	}

	dec := newPlainBytesDecoder(buf.Bytes())

	var alloc memory.Allocator
	var out []string
	for {
		v, err := dec.Decode(&alloc, batchSize)

		// Handle potential value before checking errors.
		if v != nil {
			strArr := v.(*columnar.UTF8)
			for i := range strArr.Len() {
				out = append(out, string(strArr.Get(i)))
			}
		}

		if err != nil && errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
	}

	require.Equal(t, testStrings, out)
}

func Test_plainBytesDecoder_Decode(t *testing.T) {
	for _, tc := range malformedPlainData {
		t.Run("returns an error and no values when "+tc.name, func(t *testing.T) {
			var alloc memory.Allocator

			arr, err := newPlainBytesDecoder(tc.data).Decode(&alloc, 5)
			require.EqualError(t, err, tc.err)
			require.Nil(t, arr)
		})
	}

	t.Run("returns an error and no values when the first value runs past the end of the data", func(t *testing.T) {
		var alloc memory.Allocator

		arr, err := newPlainBytesDecoder([]byte{5, 'c'}).Decode(&alloc, 1)
		require.EqualError(t, err, "plain: value of 5 bytes at offset 0 runs past the end of the data")
		require.Nil(t, arr)
	})
}

func Test_plainBytesDecoder_Skip(t *testing.T) {
	encodePlainStrings := func(t *testing.T, values ...string) []byte {
		t.Helper()

		var buf bytes.Buffer
		enc := newPlainBytesEncoder(&buf)
		for _, v := range values {
			require.NoError(t, enc.Encode(BinaryValue([]byte(v))))
		}
		return buf.Bytes()
	}

	decodePlainStrings := func(t *testing.T, dec *plainBytesDecoder, count int) []string {
		t.Helper()

		var alloc memory.Allocator
		arr, err := dec.Decode(&alloc, count)
		require.NoError(t, err)

		values := arr.(*columnar.UTF8)
		out := make([]string, values.Len())
		for i := range out {
			out[i] = string(values.Get(i))
		}
		return out
	}

	t.Run("decodes the values after the skipped ones", func(t *testing.T) {
		dec := newPlainBytesDecoder(encodePlainStrings(t, "a", "", "ccc", "dddd", "e"))

		skipped, err := dec.Skip(3)
		require.NoError(t, err)
		require.Equal(t, 3, skipped)

		require.Equal(t, []string{"dddd", "e"}, decodePlainStrings(t, dec, 2))
	})

	t.Run("skips no value when count is zero or negative", func(t *testing.T) {
		dec := newPlainBytesDecoder(encodePlainStrings(t, "a", "b"))

		for _, count := range []int{0, -1} {
			skipped, err := dec.Skip(count)
			require.NoError(t, err)
			require.Equal(t, 0, skipped)
		}

		require.Equal(t, []string{"a", "b"}, decodePlainStrings(t, dec, 2))
	})

	t.Run("skips fewer values and returns no error when the data ends first", func(t *testing.T) {
		dec := newPlainBytesDecoder(encodePlainStrings(t, "a", ""))

		skipped, err := dec.Skip(5)
		require.NoError(t, err)
		require.Equal(t, 2, skipped)

		var alloc memory.Allocator
		arr, err := dec.Decode(&alloc, 1)
		require.ErrorIs(t, err, io.EOF)
		require.Nil(t, arr)
	})

	t.Run("continues from the previous position when Skip follows Decode and Skip", func(t *testing.T) {
		dec := newPlainBytesDecoder(encodePlainStrings(t, "a", "bb", "ccc", "dddd", "eeeee", "f"))

		require.Equal(t, []string{"a", "bb"}, decodePlainStrings(t, dec, 2))

		skipped, err := dec.Skip(1)
		require.NoError(t, err)
		require.Equal(t, 1, skipped)

		skipped, err = dec.Skip(2)
		require.NoError(t, err)
		require.Equal(t, 2, skipped)

		require.Equal(t, []string{"f"}, decodePlainStrings(t, dec, 1))
	})

	t.Run("skips values at the boundaries of one-, two- and three-byte length prefixes", func(t *testing.T) {
		lengths := []int{0, 1, 127, 128, 16383, 16384, 70000}
		values := make([]string, 0, len(lengths)+1)
		for _, length := range lengths {
			values = append(values, string(bytes.Repeat([]byte("x"), length)))
		}
		values = append(values, "last")
		dec := newPlainBytesDecoder(encodePlainStrings(t, values...))

		skipped, err := dec.Skip(len(lengths))
		require.NoError(t, err)
		require.Equal(t, len(lengths), skipped)

		require.Equal(t, []string{"last"}, decodePlainStrings(t, dec, 1))
	})

	for _, tc := range malformedPlainData {
		t.Run("returns an error and the number of values before it when "+tc.name, func(t *testing.T) {
			skipped, err := newPlainBytesDecoder(tc.data).Skip(5)
			require.EqualError(t, err, tc.err)
			require.Equal(t, 1, skipped)
		})
	}

	t.Run("decodes the written values for random skip and read sizes", func(t *testing.T) {
		const valueCount = 2000

		rnd := rand.New(rand.NewSource(1))
		want := make([]string, valueCount)
		for i := range want {
			value := make([]byte, rnd.Intn(300))
			_, _ = rnd.Read(value)
			want[i] = string(value)
		}
		dec := newPlainBytesDecoder(encodePlainStrings(t, want...))

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
			if arr == nil {
				require.ErrorIs(t, err, io.EOF)
				break
			}
			values := arr.(*columnar.UTF8)
			decoded := make([]string, values.Len())
			for i := range decoded {
				decoded[i] = string(values.Get(i))
			}
			require.Equal(t, want[row:row+len(decoded)], decoded)
			row += len(decoded)
		}
		require.Equal(t, valueCount, row)
	})
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
	for _, scenario := range plainDecoderScenarios() {
		benchmarkName := fmt.Sprintf("scenario=%s", scenario.name)
		b.Run(benchmarkName, func(b *testing.B) {
			data, totalSize, totalCount := encodePlainScenario(b, scenario)
			dec := newPlainBytesDecoder(data)

			var alloc memory.Allocator

			var totalRows int
			for b.Loop() {
				alloc.Reset()
				dec.Reset(data)

				for {
					arr, err := dec.Decode(&alloc, totalCount)
					if arr != nil {
						totalRows += arr.Len()
					}
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

// Benchmark_plainBytesDecoder_DecodeWindow decodes the values of a page in
// windows. Like the row reader, it reclaims the allocator before each read.
func Benchmark_plainBytesDecoder_DecodeWindow(b *testing.B) {
	for _, scenario := range plainDecoderScenarios() {
		for _, window := range []int{100, 1000} {
			benchmarkName := fmt.Sprintf("scenario=%s/window=%d", scenario.name, window)
			b.Run(benchmarkName, func(b *testing.B) {
				data, totalSize, _ := encodePlainScenario(b, scenario)
				dec := newPlainBytesDecoder(data)

				var alloc memory.Allocator

				b.ReportAllocs()

				var totalRows int
				for b.Loop() {
					dec.Reset(data)

					for {
						alloc.Reclaim()

						arr, err := dec.Decode(&alloc, window)
						if arr != nil {
							totalRows += arr.Len()
						}
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

// Benchmark_plainBytesDecoder_Skip alternates between skipping a gap of values
// and decoding a few. It reclaims the allocator before each Skip and Decode
// pair, as the row reader does between reads.
func Benchmark_plainBytesDecoder_Skip(b *testing.B) {
	const readRows = 10

	for _, scenario := range plainDecoderScenarios() {
		for _, gap := range []int{10, 100, 1000} {
			benchmarkName := fmt.Sprintf("scenario=%s/gap=%d", scenario.name, gap)
			b.Run(benchmarkName, func(b *testing.B) {
				data, totalSize, totalCount := encodePlainScenario(b, scenario)
				dec := newPlainBytesDecoder(data)

				var alloc memory.Allocator

				b.ReportAllocs()

				var totalRows int
				for b.Loop() {
					dec.Reset(data)

					for row := 0; row < totalCount; row += readRows + gap {
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

				b.SetBytes(int64(totalSize))
				b.ReportMetric(float64(totalRows)/float64(b.N), "rows/op")
			})
		}
	}
}

type plainDecoderScenario struct {
	name       string
	makeValues func() []Value
}

// plainDecoderScenarios returns the scenarios that the decoder benchmarks run.
// Each scenario generates about 2 MB of values.
func plainDecoderScenarios() []plainDecoderScenario {
	const pageSize = 2_000_000

	return []plainDecoderScenario{
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
}

// encodePlainScenario encodes the scenario values with the plain encoding. The
// returned totalSize sums the value lengths and excludes the length prefixes.
func encodePlainScenario(tb testing.TB, scenario plainDecoderScenario) (data []byte, totalSize, totalCount int) {
	tb.Helper()

	var buf bytes.Buffer
	enc := newPlainBytesEncoder(&buf)

	for _, value := range scenario.makeValues() {
		totalSize += len(value.Binary())
		totalCount++
		require.NoError(tb, enc.Encode(value))
	}
	return buf.Bytes(), totalSize, totalCount
}
