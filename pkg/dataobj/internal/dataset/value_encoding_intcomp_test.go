package dataset

import (
	"bytes"
	"errors"
	"io"
	"math"
	"math/rand"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
)

func Test_intComp(t *testing.T) {
	numbers := []int64{
		1234,
		543,
		2345,
		1432,
	}

	var buf bytes.Buffer

	var (
		enc    = newIntCompEncoder(&buf)
		dec    = newIntCompDecoder(&buf)
		decBuf = make([]Value, batchSize)
	)

	for _, num := range numbers {
		require.NoError(t, enc.Encode(Int64Value(num)))
	}
	err := enc.Flush()
	require.NoError(t, err)

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

func Fuzz_intComp(f *testing.F) {
	f.Add(int64(rand.Int63()), 10)
	f.Add(int64(rand.Int63()), 25)

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

func Benchmark_intCompEncoder_Encode(b *testing.B) {
	b.Run("Sequential", func(b *testing.B) {
		enc := newIntCompEncoder(streamio.Discard)

		b.ResetTimer()
		for i := int64(0); i < int64(b.N); i++ {
			_ = enc.Encode(Int64Value(i))
		}

		b.ReportMetric(float64(b.N)*float64(unsafe.Sizeof(int64(0)))/float64(b.Elapsed().Seconds())*1e-9, "GB/s")
	})

	b.Run("Largest delta", func(b *testing.B) {
		enc := newIntCompEncoder(streamio.Discard)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if i%2 == 0 {
				_ = enc.Encode(Int64Value(0))
			} else {
				_ = enc.Encode(Int64Value(math.MaxInt64))
			}
		}
		b.ReportMetric(float64(b.N)*float64(unsafe.Sizeof(int64(0)))/float64(b.Elapsed().Seconds())*1e-9, "GB/s")
	})

	b.Run("Random", func(b *testing.B) {
		rnd := rand.New(rand.NewSource(0))
		enc := newIntCompEncoder(streamio.Discard)

		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			_ = enc.Encode(Int64Value(rnd.Int63()))
		}
		b.ReportMetric(float64(b.N)*float64(unsafe.Sizeof(int64(0)))/float64(b.Elapsed().Seconds())*1e-9, "GB/s")
	})

}

func Benchmark_intCompDecoder_Decode(b *testing.B) {
	b.Run("Sequential", func(b *testing.B) {
		var buf bytes.Buffer

		var (
			enc = newIntCompEncoder(&buf)
			dec = newIntCompDecoder(&buf)
		)

		for i := 0; i < b.N; i++ {
			_ = enc.Encode(Int64Value(int64(i)))
		}
		err := enc.Flush()
		require.NoError(b, err)

		b.ResetTimer()
		b.ReportMetric(float64(buf.Len())/float64(b.N), "bytes/value")
		for i := 0; i < b.N; i++ {
			_, _ = dec.decode()
		}
		b.ReportMetric(float64(b.N)*float64(unsafe.Sizeof(int64(0)))/float64(b.Elapsed().Seconds())*1e-9, "GB/s")
	})

	b.Run("Largest delta", func(b *testing.B) {
		var buf bytes.Buffer

		var (
			enc = newIntCompEncoder(&buf)
			dec = newIntCompDecoder(&buf)
		)

		for i := 0; i < b.N; i++ {
			if i%2 == 0 {
				_ = enc.Encode(Int64Value(0))
			} else {
				_ = enc.Encode(Int64Value(math.MaxInt64))
			}
		}
		err := enc.Flush()
		require.NoError(b, err)

		b.ResetTimer()
		b.ReportMetric(float64(buf.Len())/float64(b.N), "bytes/value")
		for i := 0; i < b.N; i++ {
			_, _ = dec.decode()
		}
		b.ReportMetric(float64(b.N)*float64(unsafe.Sizeof(int64(0)))/float64(b.Elapsed().Seconds())*1e-9, "GB/s")
	})

	b.Run("Random", func(b *testing.B) {
		rnd := rand.New(rand.NewSource(0))

		var buf bytes.Buffer

		var (
			enc = newIntCompEncoder(&buf)
			dec = newIntCompDecoder(&buf)
		)

		for i := 0; i < b.N; i++ {
			_ = enc.Encode(Int64Value(rnd.Int63()))
		}
		err := enc.Flush()
		require.NoError(b, err)

		b.ResetTimer()
		b.ReportMetric(float64(buf.Len())/float64(b.N), "bytes/value")
		for i := 0; i < b.N; i++ {
			_, _ = dec.decode()
		}
		b.ReportMetric(float64(b.N)*float64(unsafe.Sizeof(int64(0)))/float64(b.Elapsed().Seconds())*1e-9, "GB/s")
	})
}
