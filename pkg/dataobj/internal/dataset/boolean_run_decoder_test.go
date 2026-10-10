package dataset

import (
	"errors"
	"io"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

// Test_booleanRunDecoder_MatchesBatchDecode fuzzes random packed-boolean
// pages and checks that reading them back incrementally (in random chunk
// sizes) through booleanRunDecoder produces the exact same packed bytes as
// parquet-go's own whole-buffer batch decode
// ([rle.Encoding.DecodeBoolean]).
func Test_booleanRunDecoder_MatchesBatchDecode(t *testing.T) {
	rnd := rand.New(rand.NewSource(1))

	for i := range 200 {
		n := rnd.Intn(3000)
		values := make([]bool, n)
		for j := range values {
			values[j] = rnd.Intn(3) == 0
		}

		src := make([]byte, (len(values)+7)/8)
		for j, v := range values {
			if v {
				src[j/8] |= 1 << (j % 8)
			}
		}

		encoded, err := bitmapRLEEncoding.EncodeBoolean(nil, src)
		require.NoError(t, err)

		want, err := bitmapRLEEncoding.DecodeBoolean(nil, encoded)
		require.NoError(t, err)

		var dec booleanRunDecoder
		require.NoError(t, dec.reset(encoded))

		var got []byte
		for {
			chunk := 1 + rnd.Intn(50)
			buf := make([]byte, chunk)
			c, err := dec.decodeBoolean(buf)
			got = append(got, buf[:c]...)
			if err != nil {
				if !errors.Is(err, io.EOF) {
					t.Fatalf("iteration %d: decodeBoolean: %v", i, err)
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
