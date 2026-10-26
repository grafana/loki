package compactionv2

import (
	"cmp"
	"testing"

	"github.com/stretchr/testify/require"
)

const splitEnd = 100

func rng(start, end int) KeyRange[int] { return KeyRange[int]{start: start, end: end} }

func TestSplitKeyRanges(t *testing.T) {
	const budget = 100

	tests := []struct {
		name string
		keys []KeySize[int]
		want []KeySplit[int]
	}{
		{
			name: "returns no ranges for no keys",
			keys: nil,
			want: nil,
		},
		{
			name: "returns one range from the first key to the end when all keys fit the budget",
			keys: []KeySize[int]{{1, 40}, {2, 60}},
			want: []KeySplit[int]{{Range: rng(1, splitEnd), Size: 100}},
		},
		{
			name: "cuts before the key that would exceed the budget",
			keys: []KeySize[int]{{1, 60}, {2, 30}, {3, 20}, {4, 50}, {5, 45}},
			want: []KeySplit[int]{
				{Range: rng(1, 3), Size: 90},
				{Range: rng(3, 5), Size: 70},
				{Range: rng(5, splitEnd), Size: 45},
			},
		},
		{
			name: "adds up equal keys from many sections and keeps them in one range",
			keys: []KeySize[int]{{2, 30}, {1, 50}, {2, 30}, {3, 51}},
			want: []KeySplit[int]{
				{Range: rng(1, 2), Size: 50},
				{Range: rng(2, 3), Size: 60},
				{Range: rng(3, splitEnd), Size: 51},
			},
		},
		{
			name: "folds the last range into the previous one at exactly 110 percent of the budget",
			keys: []KeySize[int]{{1, 100}, {2, 10}},
			want: []KeySplit[int]{{Range: rng(1, splitEnd), Size: 110}},
		},
		{
			name: "keeps the last range separate just above 110 percent of the budget",
			keys: []KeySize[int]{{1, 100}, {2, 11}},
			want: []KeySplit[int]{
				{Range: rng(1, 2), Size: 100},
				{Range: rng(2, splitEnd), Size: 11},
			},
		},
		{
			name: "gives an oversized first key its own range",
			keys: []KeySize[int]{{1, 250}, {2, 50}, {3, 70}},
			want: []KeySplit[int]{
				{Range: rng(1, 2), Size: 250, Oversized: true},
				{Range: rng(2, 3), Size: 50},
				{Range: rng(3, splitEnd), Size: 70},
			},
		},
		{
			name: "gives an oversized middle key its own range",
			keys: []KeySize[int]{{1, 50}, {2, 250}, {3, 50}},
			want: []KeySplit[int]{
				{Range: rng(1, 2), Size: 50},
				{Range: rng(2, 3), Size: 250, Oversized: true},
				{Range: rng(3, splitEnd), Size: 50},
			},
		},
		{
			name: "does not fold a small last range into an oversized key",
			keys: []KeySize[int]{{1, 101}, {2, 1}},
			want: []KeySplit[int]{
				{Range: rng(1, 2), Size: 101, Oversized: true},
				{Range: rng(2, splitEnd), Size: 1},
			},
		},
		{
			name: "does not fold an oversized last key into a small range",
			keys: []KeySize[int]{{1, 1}, {2, 101}},
			want: []KeySplit[int]{
				{Range: rng(1, 2), Size: 1},
				{Range: rng(2, splitEnd), Size: 101, Oversized: true},
			},
		},
		{
			name: "marks a merged last range above the budget as not oversized",
			keys: []KeySize[int]{{1, 60}, {2, 40}, {3, 5}},
			want: []KeySplit[int]{{Range: rng(1, splitEnd), Size: 105}},
		},
		{
			name: "puts a key that exactly fills the budget in the current range",
			keys: []KeySize[int]{{1, 40}, {2, 60}, {3, 100}},
			want: []KeySplit[int]{
				{Range: rng(1, 3), Size: 100},
				{Range: rng(3, splitEnd), Size: 100},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SplitKeyRanges(tc.keys, cmp.Compare[int], budget, splitEnd)
			require.Equal(t, tc.want, got)
		})
	}

	t.Run("returns the same ranges for any input order", func(t *testing.T) {
		keys := []KeySize[int]{{1, 60}, {2, 30}, {3, 20}, {4, 50}, {5, 40}}
		reversed := []KeySize[int]{{5, 40}, {4, 50}, {3, 20}, {2, 30}, {1, 60}}
		require.Equal(t,
			SplitKeyRanges(keys, cmp.Compare[int], budget, splitEnd),
			SplitKeyRanges(reversed, cmp.Compare[int], budget, splitEnd),
		)
	})

	t.Run("places every key from the first input key to the end in exactly one range", func(t *testing.T) {
		keys := []KeySize[int]{{1, 60}, {2, 30}, {3, 250}, {4, 50}, {5, 40}, {6, 5}}
		ranges := SplitKeyRanges(keys, cmp.Compare[int], budget, splitEnd)
		require.Equal(t, 1, ranges[0].Range.Start())
		require.Equal(t, splitEnd, ranges[len(ranges)-1].Range.End())
		for i := 1; i < len(ranges); i++ {
			require.Equal(t, ranges[i-1].Range.End(), ranges[i].Range.Start(), "ranges %d and %d must share a bound", i-1, i)
		}
		for key := 1; key < splitEnd; key++ {
			var matches int
			for _, r := range ranges {
				if r.Range.Contains(key, cmp.Compare[int]) {
					matches++
				}
			}
			require.Equal(t, 1, matches, "key %d", key)
		}
	})

	t.Run("panics when the budget is zero", func(t *testing.T) {
		require.PanicsWithValue(t, "key range budget must be > 0, got 0", func() {
			SplitKeyRanges([]KeySize[int]{{1, 1}}, cmp.Compare[int], 0, splitEnd)
		})
	})

	t.Run("panics when a key is not below the end", func(t *testing.T) {
		require.PanicsWithValue(t, "key not below the end of the split", func() {
			SplitKeyRanges([]KeySize[int]{{splitEnd, 1}}, cmp.Compare[int], budget, splitEnd)
		})
	})
}

func TestNewKeyRange(t *testing.T) {
	t.Run("returns the range when start is below end", func(t *testing.T) {
		r, err := NewKeyRange(2, 4, cmp.Compare[int])
		require.NoError(t, err)
		require.Equal(t, 2, r.Start())
		require.Equal(t, 4, r.End())
	})

	t.Run("returns an error when start equals end", func(t *testing.T) {
		_, err := NewKeyRange(3, 3, cmp.Compare[int])
		require.ErrorContains(t, err, "is not below its end")
	})

	t.Run("returns an error when start is above end", func(t *testing.T) {
		_, err := NewKeyRange(4, 2, cmp.Compare[int])
		require.ErrorContains(t, err, "is not below its end")
	})
}

func TestKeyRange_Contains(t *testing.T) {
	r := rng(2, 4)
	tests := []struct {
		name string
		key  int
		want bool
	}{
		{name: "includes the start key", key: 2, want: true},
		{name: "includes a key between the bounds", key: 3, want: true},
		{name: "excludes the end key", key: 4, want: false},
		{name: "excludes a key below the start", key: 1, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, r.Contains(tc.key, cmp.Compare[int]))
		})
	}
}
