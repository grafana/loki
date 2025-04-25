package dataset

import (
	"bytes"
	"errors"
	"io"
	"math"
	"math/rand"
	"testing"

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
	b.Run("Sequential", func(b *testing.B) {
		var buf bytes.Buffer

		var (
			enc = newDeltaEncoder(&buf)
			dec = newDeltaDecoder(&buf)
		)

		for i := 0; i < b.N; i++ {
			_ = enc.Encode(Int64Value(int64(i)))
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = dec.decode()
		}
	})

	b.Run("Largest delta", func(b *testing.B) {
		var buf bytes.Buffer

		var (
			enc = newDeltaEncoder(&buf)
			dec = newDeltaDecoder(&buf)
		)

		for i := 0; i < b.N; i++ {
			if i%2 == 0 {
				_ = enc.Encode(Int64Value(0))
			} else {
				_ = enc.Encode(Int64Value(math.MaxInt64))
			}
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = dec.decode()
		}
	})

	b.Run("Random", func(b *testing.B) {
		rnd := rand.New(rand.NewSource(0))

		var buf bytes.Buffer

		var (
			enc = newDeltaEncoder(&buf)
			dec = newDeltaDecoder(&buf)
		)

		for i := 0; i < b.N; i++ {
			_ = enc.Encode(Int64Value(rnd.Int63()))
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = dec.decode()
		}
	})
}
