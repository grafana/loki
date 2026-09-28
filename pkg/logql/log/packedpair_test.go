package log

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPackedPairUsesRarestByte(t *testing.T) {
	f := newSubstrFinder([]byte("e5e650e85685"))
	require.True(t, f.packed)
	require.Equal(t, byte('6'), f.byte1)
	require.Equal(t, 3, f.index1)
	require.Equal(t, byte('8'), f.byte2)
	require.Equal(t, 7, f.index2)
}

func TestSubstrFinderMatchesBytesContains(t *testing.T) {
	needles := [][]byte{
		nil,
		{},
		[]byte("a"),
		[]byte("aa"),
		[]byte("ab"),
		[]byte("aaa"),
		[]byte("aaaa"),
		[]byte("e5e650e85685"),
		[]byte("error"),
		[]byte("\x00\x00"),
		[]byte("a\x00b"),
		bytes.Repeat([]byte("a"), 32),
		bytes.Repeat([]byte("a"), 33),
		bytes.Repeat([]byte("ab"), 16),
		append(bytes.Repeat([]byte("a"), 31), 'b'),
		append(bytes.Repeat([]byte("a"), 40), 'b'),
	}
	haystacks := [][]byte{
		nil,
		{},
		[]byte("a"),
		[]byte("aaaa"),
		[]byte("aaaabaaaa"),
		[]byte("bbb"),
		bytes.Repeat([]byte("a"), 100),
		bytes.Repeat([]byte("e"), 200),
		append(append(bytes.Repeat([]byte("e"), 50), []byte("e5e650e85685")...), bytes.Repeat([]byte("x"), 20)...),
		[]byte("prefix e5e650e85685"),
		[]byte("e5e650e85685 suffix"),
		[]byte("\x00\x00\x00"),
		[]byte("xa\x00by"),
	}
	for _, needle := range needles {
		for _, haystack := range haystacks {
			requireSameContains(t, haystack, needle)
		}
		// Plant the needle at every offset of a short buffer, including overlaps.
		if len(needle) == 0 || len(needle) > 16 {
			continue
		}
		buf := bytes.Repeat([]byte("xyz"), 8)
		for at := 0; at <= len(buf)-len(needle); at++ {
			planted := append([]byte{}, buf...)
			copy(planted[at:], needle)
			requireSameContains(t, planted, needle)
		}
	}
}

func TestSubstrFinderRandomMatchesBytesContains(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for n := 0; n < 200; n++ {
		haystack := randomBytes(rng, rng.Intn(300))
		needle := randomBytes(rng, rng.Intn(48))
		requireSameContains(t, haystack, needle)
		if len(needle) > 0 && len(needle) <= len(haystack) {
			at := rng.Intn(len(haystack) - len(needle) + 1)
			copy(haystack[at:], needle)
			requireSameContains(t, haystack, needle)
		}
	}
}

func TestContainsAllFilterUsesFinder(t *testing.T) {
	all := &containsAllFilter{}
	all.Add(*newContainsFilter([]byte("foo"), false).(*containsFilter))
	all.Add(*newContainsFilter([]byte("e5e650e85685"), false).(*containsFilter))
	require.True(t, all.Filter([]byte("foo e5e650e85685")))
	require.False(t, all.Filter([]byte("foo bar")))
	require.False(t, all.Filter([]byte("e5e650e85685")))
}

func TestContainsFilterCaseInsensitive(t *testing.T) {
	f := newContainsFilter([]byte("Error"), true)
	require.True(t, f.Filter([]byte("xx error yy")))
	require.False(t, f.Filter([]byte("xx err yy")))
}

func requireSameContains(t *testing.T, haystack, needle []byte) {
	t.Helper()
	want := bytes.Contains(haystack, needle)
	require.Equal(t, want, newSubstrFinder(needle).contains(haystack), "finder haystack %q needle %q", haystack, needle)
	got := newContainsFilter(append([]byte{}, needle...), false).Filter(haystack)
	require.Equal(t, want, got, "filter haystack %q needle %q", haystack, needle)
}

func randomBytes(rng *rand.Rand, n int) []byte {
	if n == 0 {
		return nil
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rng.Intn(256))
	}
	return b
}
