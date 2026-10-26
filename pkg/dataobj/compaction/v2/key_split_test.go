package compactionv2

import (
	"cmp"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	splitStart = 0
	splitEnd   = 100
)

func TestSplitKeyRanges(t *testing.T) {
	const budget = 100

	tests := []struct {
		name string
		keys []KeySize[int]
		want []KeyRange[int]
	}{
		{
			name: "returns the full range for no keys",
			keys: nil,
			want: []KeyRange[int]{{Start: splitStart, End: splitEnd, First: splitStart}},
		},
		{
			name: "returns one unbounded range when all keys fit the budget",
			keys: []KeySize[int]{{1, 40}, {2, 60}},
			want: []KeyRange[int]{{Start: splitStart, End: splitEnd, First: 1, Size: 100}},
		},
		{
			name: "cuts before the key that would exceed the budget",
			keys: []KeySize[int]{{1, 60}, {2, 30}, {3, 20}, {4, 50}, {5, 45}},
			want: []KeyRange[int]{
				{Start: splitStart, End: 3, First: 1, Size: 90},
				{Start: 3, End: 5, First: 3, Size: 70},
				{Start: 5, End: splitEnd, First: 5, Size: 45},
			},
		},
		{
			name: "adds up equal keys from many sections and keeps them in one range",
			keys: []KeySize[int]{{2, 30}, {1, 50}, {2, 30}, {3, 51}},
			want: []KeyRange[int]{
				{Start: splitStart, End: 2, First: 1, Size: 50},
				{Start: 2, End: 3, First: 2, Size: 60},
				{Start: 3, End: splitEnd, First: 3, Size: 51},
			},
		},
		{
			name: "folds the last range into the previous one at exactly 110 percent of the budget",
			keys: []KeySize[int]{{1, 100}, {2, 10}},
			want: []KeyRange[int]{{Start: splitStart, End: splitEnd, First: 1, Size: 110}},
		},
		{
			name: "keeps the last range separate just above 110 percent of the budget",
			keys: []KeySize[int]{{1, 100}, {2, 11}},
			want: []KeyRange[int]{
				{Start: splitStart, End: 2, First: 1, Size: 100},
				{Start: 2, End: splitEnd, First: 2, Size: 11},
			},
		},
		{
			name: "gives an oversized first key its own range",
			keys: []KeySize[int]{{1, 250}, {2, 50}, {3, 70}},
			want: []KeyRange[int]{
				{Start: splitStart, End: 2, First: 1, Size: 250},
				{Start: 2, End: 3, First: 2, Size: 50},
				{Start: 3, End: splitEnd, First: 3, Size: 70},
			},
		},
		{
			name: "gives an oversized middle key its own range",
			keys: []KeySize[int]{{1, 50}, {2, 250}, {3, 50}},
			want: []KeyRange[int]{
				{Start: splitStart, End: 2, First: 1, Size: 50},
				{Start: 2, End: 3, First: 2, Size: 250},
				{Start: 3, End: splitEnd, First: 3, Size: 50},
			},
		},
		{
			name: "does not fold a small last range into an oversized key",
			keys: []KeySize[int]{{1, 101}, {2, 1}},
			want: []KeyRange[int]{
				{Start: splitStart, End: 2, First: 1, Size: 101},
				{Start: 2, End: splitEnd, First: 2, Size: 1},
			},
		},
		{
			name: "does not fold an oversized last key into a small range",
			keys: []KeySize[int]{{1, 1}, {2, 101}},
			want: []KeyRange[int]{
				{Start: splitStart, End: 2, First: 1, Size: 1},
				{Start: 2, End: splitEnd, First: 2, Size: 101},
			},
		},
		{
			name: "puts a key that exactly fills the budget in the current range",
			keys: []KeySize[int]{{1, 40}, {2, 60}, {3, 100}},
			want: []KeyRange[int]{
				{Start: splitStart, End: 3, First: 1, Size: 100},
				{Start: 3, End: splitEnd, First: 3, Size: 100},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SplitKeyRanges(tc.keys, cmp.Compare[int], budget, splitStart, splitEnd)
			require.Equal(t, tc.want, got)
		})
	}

	t.Run("returns the same ranges for any input order", func(t *testing.T) {
		keys := []KeySize[int]{{1, 60}, {2, 30}, {3, 20}, {4, 50}, {5, 40}}
		reversed := []KeySize[int]{{5, 40}, {4, 50}, {3, 20}, {2, 30}, {1, 60}}
		require.Equal(t,
			SplitKeyRanges(keys, cmp.Compare[int], budget, splitStart, splitEnd),
			SplitKeyRanges(reversed, cmp.Compare[int], budget, splitStart, splitEnd),
		)
	})

	t.Run("places every key in exactly one range", func(t *testing.T) {
		keys := []KeySize[int]{{1, 60}, {2, 30}, {3, 250}, {4, 50}, {5, 40}, {6, 5}}
		ranges := SplitKeyRanges(keys, cmp.Compare[int], budget, splitStart, splitEnd)
		require.Equal(t, splitStart, ranges[0].Start)
		require.Equal(t, splitEnd, ranges[len(ranges)-1].End)
		for i := 1; i < len(ranges); i++ {
			require.Equal(t, ranges[i-1].End, ranges[i].Start, "ranges %d and %d must share a bound", i-1, i)
		}
		for key := splitStart; key < splitEnd; key++ {
			var matches int
			for _, r := range ranges {
				if r.Contains(key, cmp.Compare[int]) {
					matches++
				}
			}
			require.Equal(t, 1, matches, "key %d", key)
		}
	})

	t.Run("panics when the budget is zero", func(t *testing.T) {
		require.PanicsWithValue(t, "key range budget must be > 0, got 0", func() {
			SplitKeyRanges([]KeySize[int]{{1, 1}}, cmp.Compare[int], 0, splitStart, splitEnd)
		})
	})

	t.Run("panics when start is not below end", func(t *testing.T) {
		require.PanicsWithValue(t, "key range start must be below its end", func() {
			SplitKeyRanges(nil, cmp.Compare[int], budget, 5, 5)
		})
	})

	t.Run("panics when a key is outside the range", func(t *testing.T) {
		require.PanicsWithValue(t, "key outside the split range", func() {
			SplitKeyRanges([]KeySize[int]{{splitEnd, 1}}, cmp.Compare[int], budget, splitStart, splitEnd)
		})
	})
}

func TestKeyRange_Contains(t *testing.T) {
	r := KeyRange[int]{Start: 2, End: 4}
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
