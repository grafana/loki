package log

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLiteralDFA(t *testing.T) {
	randomBytes := func(rnd *rand.Rand, alphabet string, minLen, maxLen int) []byte {
		out := make([]byte, minLen+rnd.Intn(maxLen-minLen+1))
		for i := range out {
			out[i] = alphabet[rnd.Intn(len(alphabet))]
		}
		return out
	}

	t.Run("Contains matches bytes.Contains for each literal on random input", func(t *testing.T) {
		rnd := rand.New(rand.NewSource(1))
		// A small alphabet makes literals overlap, share prefixes, and end inside each other.
		const alphabet = "abc"

		for trial := range 2000 {
			literals := make([][]byte, 1+rnd.Intn(8))
			for i := range literals {
				literals[i] = randomBytes(rnd, alphabet, 1, 4)
			}
			dfa := newLiteralDFA(literals, false)

			for range 20 {
				line := randomBytes(rnd, alphabet, 0, 12)
				want := false
				for _, literal := range literals {
					want = want || bytes.Contains(line, literal)
				}
				require.Equal(t, want, dfa.Contains(line), "trial %d, literals %q, line %q", trial, literals, line)
			}
		}
	})

	t.Run("Contains with foldASCII matches either case of each ASCII letter", func(t *testing.T) {
		rnd := rand.New(rand.NewSource(1))

		for trial := range 2000 {
			literals := make([][]byte, 1+rnd.Intn(8))
			for i := range literals {
				literals[i] = randomBytes(rnd, "ab1", 1, 4)
			}
			dfa := newLiteralDFA(literals, true)

			for range 20 {
				line := randomBytes(rnd, "abAB1", 0, 12)
				want := false
				for _, literal := range literals {
					want = want || bytes.Contains(bytes.ToLower(line), literal)
				}
				require.Equal(t, want, dfa.Contains(line), "trial %d, literals %q, line %q", trial, literals, line)
			}
		}
	})

	t.Run("Contains matches a byte that is in no literal only as a mismatch", func(t *testing.T) {
		dfa := newLiteralDFA([][]byte{[]byte("ab")}, false)

		require.False(t, dfa.Contains([]byte("a\xffb")))
		require.True(t, dfa.Contains([]byte("\xffab\xff")))
	})
}
