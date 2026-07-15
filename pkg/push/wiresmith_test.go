package push

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// requireAntisymmetric fails the test unless a.CompareWiresmith(b) and
// b.CompareWiresmith(a) have opposite signs (or are both zero) -- the
// property wiresmith-6azr found broken for Stream.
func requireAntisymmetric(t *testing.T, a, b *Stream) {
	t.Helper()
	ab := a.CompareWiresmith(b)
	ba := b.CompareWiresmith(a)
	switch {
	case ab == 0 || ba == 0:
		require.Zerof(t, ab, "a.Compare(b)=%d but b.Compare(a)=%d: only one side reports equality", ab, ba)
		require.Zerof(t, ba, "a.Compare(b)=%d but b.Compare(a)=%d: only one side reports equality", ab, ba)
	case ab > 0:
		require.Negativef(t, ba, "antisymmetry violated: a.Compare(b)=%d (>0) but b.Compare(a)=%d (want <0)", ab, ba)
	default: // ab < 0
		require.Positivef(t, ba, "antisymmetry violated: a.Compare(b)=%d (<0) but b.Compare(a)=%d (want >0)", ab, ba)
	}
}

// TestStreamCompareWiresmithAntisymmetry covers wiresmith-6azr: two Streams with equal
// Labels and equal Entries length, but differing entry content, used to both report
// CompareWiresmith==1 against each other (A.Compare(B)==1 && B.Compare(A)==1), which is
// not a valid ordering. The fix falls back to comparing the marshaled form once Labels
// and entry count are exhausted as ordering keys.
func TestStreamCompareWiresmithAntisymmetry(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("same labels and entry count, differing entry content", func(t *testing.T) {
		a := &Stream{
			Labels:  `{job="a"}`,
			Entries: []Entry{{Timestamp: base, Line: "line-a"}},
		}
		b := &Stream{
			Labels:  `{job="a"}`,
			Entries: []Entry{{Timestamp: base, Line: "line-b"}},
		}
		require.False(t, a.Equal(b))
		require.NotZero(t, a.CompareWiresmith(b), "differing entry content must not compare equal")
		requireAntisymmetric(t, a, b)
	})

	t.Run("same labels and entries, differing Hash only", func(t *testing.T) {
		a := &Stream{Labels: `{job="a"}`, Entries: []Entry{{Timestamp: base, Line: "line-a"}}, Hash: 1}
		b := &Stream{Labels: `{job="a"}`, Entries: []Entry{{Timestamp: base, Line: "line-a"}}, Hash: 2}
		require.False(t, a.Equal(b), "Stream.Equal compares Hash too")
		require.NotZero(t, a.CompareWiresmith(b), "differing Hash must not compare equal")
		requireAntisymmetric(t, a, b)
	})

	t.Run("differing labels", func(t *testing.T) {
		a := &Stream{Labels: `{job="a"}`}
		b := &Stream{Labels: `{job="b"}`}
		requireAntisymmetric(t, a, b)
	})

	t.Run("differing entry count, same labels", func(t *testing.T) {
		a := &Stream{Labels: `{job="a"}`, Entries: []Entry{{Timestamp: base, Line: "l1"}}}
		b := &Stream{Labels: `{job="a"}`, Entries: []Entry{{Timestamp: base, Line: "l1"}, {Timestamp: base, Line: "l2"}}}
		requireAntisymmetric(t, a, b)
	})

	t.Run("equal streams", func(t *testing.T) {
		a := &Stream{Labels: `{job="a"}`, Entries: []Entry{{Timestamp: base, Line: "l1"}}}
		b := &Stream{Labels: `{job="a"}`, Entries: []Entry{{Timestamp: base, Line: "l1"}}}
		require.Zero(t, a.CompareWiresmith(b))
		require.Zero(t, b.CompareWiresmith(a))
	})

	t.Run("wrong type", func(t *testing.T) {
		a := &Stream{Labels: `{job="a"}`}
		require.Equal(t, -1, a.CompareWiresmith("not a stream"))
	})

	t.Run("value receiver other", func(t *testing.T) {
		a := &Stream{Labels: `{job="a"}`, Entries: []Entry{{Timestamp: base, Line: "line-a"}}}
		bVal := Stream{Labels: `{job="a"}`, Entries: []Entry{{Timestamp: base, Line: "line-b"}}}
		require.NotZero(t, a.CompareWiresmith(bVal))
	})
}
