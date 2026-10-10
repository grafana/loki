package dataset

import (
	"errors"
	"io"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

// Test_binaryPackedDecoder_MatchesBatchDecode fuzzes random int64 sequences
// and checks that reading them back incrementally (in random chunk sizes)
// through binaryPackedDecoder produces the exact same values as parquet-go's
// own whole-buffer batch decode ([delta.BinaryPackedEncoding.DecodeInt64]).
func Test_binaryPackedDecoder_MatchesBatchDecode(t *testing.T) {
	rnd := rand.New(rand.NewSource(1))

	for i := range 200 {
		n := rnd.Intn(2000)
		values := make([]int64, n)
		for j := range values {
			values[j] = rnd.Int63() - (1 << 62) // Mix of positive and negative.
		}

		encoded, err := deltaEncoding.EncodeInt64(nil, values)
		require.NoError(t, err)

		want, err := deltaEncoding.DecodeInt64(make([]int64, n), encoded)
		require.NoError(t, err)

		var dec binaryPackedDecoder
		_, err = dec.reset(encoded)
		require.NoError(t, err)

		var got []int64
		for {
			chunk := 1 + rnd.Intn(50)
			buf := make([]int64, chunk)
			c, err := dec.decodeInt64(buf)
			got = append(got, buf[:c]...)
			if err != nil {
				if !errors.Is(err, io.EOF) {
					t.Fatalf("iteration %d: decodeInt64: %v", i, err)
				}
				break
			}
		}

		if len(want) == 0 && len(got) == 0 {
			continue // Avoid a nil-vs-empty-slice mismatch for n == 0.
		}
		require.Equal(t, want, got, "iteration %d", i)
	}
}
