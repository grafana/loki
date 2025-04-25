package dataset

import (
	"bytes"
	"errors"
	"io"
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_bitmap(t *testing.T) {
	var buf bytes.Buffer

	var (
		enc    = newBitmapEncoder(&buf)
		dec    = newBitmapDecoder(&buf)
		decBuf = make([]Value, batchSize)
	)

	count := 1500
	for i := 0; i < count; i++ {
		require.NoError(t, enc.Encode(Uint64Value(uint64(1))))
	}
	require.NoError(t, enc.Flush())

	t.Logf("Buffer size: %d", buf.Len())

	for {
		n, err := dec.Decode(decBuf[:batchSize])
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		for _, v := range decBuf[:n] {
			require.Equal(t, uint64(1), v.Uint64())
		}
	}
}

func Test_bitmap_bitpacking(t *testing.T) {
	var buf bytes.Buffer

	var (
		enc    = newBitmapEncoder(&buf)
		dec    = newBitmapDecoder(&buf)
		decBuf = make([]Value, batchSize)
	)

	expect := []uint64{0, 1, 2, 3, 4, 5, 6, 7}
	for _, v := range expect {
		require.NoError(t, enc.Encode(Uint64Value(v)))
	}
	require.NoError(t, enc.Flush())

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
		dec    = newBitmapDecoder(&buf)
		decBuf = make([]Value, batchSize)
	)

	expect := []uint64{0, 1, 2, 3, 4}
	for _, v := range expect {
		require.NoError(t, enc.Encode(Uint64Value(v)))
	}
	require.NoError(t, enc.Flush())

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
			enc    = newBitmapEncoder(&buf)
			dec    = newBitmapDecoder(&buf)
			decBuf = make([]Value, batchSize)
		)

		var numbers []uint64
		for i := 0; i < count; i++ {
			var mask uint64 = math.MaxUint64
			if width < 64 {
				mask = (1 << width) - 1
			}

			v := uint64(rnd.Int63()) & mask
			numbers = append(numbers, v)
			require.NoError(t, enc.Encode(Uint64Value(v)))
		}
		require.NoError(t, enc.Flush())

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

		require.Equal(t, numbers, actual)
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

func Benchmark_bitmapDecoder(b *testing.B) {
	b.Run("width=1", func(b *testing.B) { benchmarkBitmapDecoder(b, 1) })
	b.Run("width=3", func(b *testing.B) { benchmarkBitmapDecoder(b, 3) })
	b.Run("width=5", func(b *testing.B) { benchmarkBitmapDecoder(b, 5) })
	b.Run("width=8", func(b *testing.B) { benchmarkBitmapDecoder(b, 8) })
	b.Run("width=32", func(b *testing.B) { benchmarkBitmapDecoder(b, 32) })
	b.Run("width=64", func(b *testing.B) { benchmarkBitmapDecoder(b, 64) })
}

func benchmarkBitmapDecoder(b *testing.B, width int) {
	b.Run("variance=none", func(b *testing.B) {
		var buf bytes.Buffer

		var (
			enc = newBitmapEncoder(&buf)
			dec = newBitmapDecoder(&buf)
		)

		for i := 0; i < b.N; i++ {
			_ = enc.Encode(Uint64Value(1))
		}
		_ = enc.Flush()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = dec.decode()
		}
	})

	b.Run("variance=alternating", func(b *testing.B) {
		var buf bytes.Buffer

		var (
			enc = newBitmapEncoder(&buf)
			dec = newBitmapDecoder(&buf)
		)

		for i := 0; i < b.N; i++ {
			_ = enc.Encode(Uint64Value(uint64(i % width)))
		}
		_ = enc.Flush()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = dec.decode()
		}
	})

	b.Run("variance=random", func(b *testing.B) {
		rnd := rand.New(rand.NewSource(0))
		var buf bytes.Buffer

		var (
			enc = newBitmapEncoder(&buf)
			dec = newBitmapDecoder(&buf)
		)

		for i := 0; i < b.N; i++ {
			_ = enc.Encode(Uint64Value(uint64(rnd.Int63()) % uint64(width)))
		}
		_ = enc.Flush()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = dec.decode()
		}
	})
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
