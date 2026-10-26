package compactionv2

import (
	"cmp"
	"testing"

	"github.com/stretchr/testify/require"
)

func keyPtr(key int) *int { return &key }

func TestSplitKeyRanges(t *testing.T) {
	const budget = 100

	tests := []struct {
		name string
		keys []KeySize[int]
		want []KeyRange[int]
	}{
		{
			name: "returns no ranges for no keys",
			keys: nil,
			want: nil,
		},
		{
			name: "returns one unbounded range when all keys fit the budget",
			keys: []KeySize[int]{{1, 40}, {2, 60}},
			want: []KeyRange[int]{{First: 1, Size: 100}},
		},
		{
			name: "cuts before the key that would exceed the budget",
			keys: []KeySize[int]{{1, 60}, {2, 30}, {3, 20}, {4, 50}, {5, 45}},
			want: []KeyRange[int]{
				{End: keyPtr(3), First: 1, Size: 90},
				{Start: keyPtr(3), End: keyPtr(5), First: 3, Size: 70},
				{Start: keyPtr(5), First: 5, Size: 45},
			},
		},
		{
			name: "adds up equal keys from many sections and keeps them in one range",
			keys: []KeySize[int]{{2, 30}, {1, 50}, {2, 30}, {3, 51}},
			want: []KeyRange[int]{
				{End: keyPtr(2), First: 1, Size: 50},
				{Start: keyPtr(2), End: keyPtr(3), First: 2, Size: 60},
				{Start: keyPtr(3), First: 3, Size: 51},
			},
		},
		{
			name: "folds the last range into the previous one at exactly 110 percent of the budget",
			keys: []KeySize[int]{{1, 100}, {2, 10}},
			want: []KeyRange[int]{{First: 1, Size: 110}},
		},
		{
			name: "keeps the last range separate just above 110 percent of the budget",
			keys: []KeySize[int]{{1, 100}, {2, 11}},
			want: []KeyRange[int]{
				{End: keyPtr(2), First: 1, Size: 100},
				{Start: keyPtr(2), First: 2, Size: 11},
			},
		},
		{
			name: "gives an oversized first key its own range",
			keys: []KeySize[int]{{1, 250}, {2, 50}, {3, 70}},
			want: []KeyRange[int]{
				{End: keyPtr(2), First: 1, Size: 250},
				{Start: keyPtr(2), End: keyPtr(3), First: 2, Size: 50},
				{Start: keyPtr(3), First: 3, Size: 70},
			},
		},
		{
			name: "gives an oversized middle key its own range",
			keys: []KeySize[int]{{1, 50}, {2, 250}, {3, 50}},
			want: []KeyRange[int]{
				{End: keyPtr(2), First: 1, Size: 50},
				{Start: keyPtr(2), End: keyPtr(3), First: 2, Size: 250},
				{Start: keyPtr(3), First: 3, Size: 50},
			},
		},
		{
			name: "does not fold a small last range into an oversized key",
			keys: []KeySize[int]{{1, 101}, {2, 1}},
			want: []KeyRange[int]{
				{End: keyPtr(2), First: 1, Size: 101},
				{Start: keyPtr(2), First: 2, Size: 1},
			},
		},
		{
			name: "does not fold an oversized last key into a small range",
			keys: []KeySize[int]{{1, 1}, {2, 101}},
			want: []KeyRange[int]{
				{End: keyPtr(2), First: 1, Size: 1},
				{Start: keyPtr(2), First: 2, Size: 101},
			},
		},
		{
			name: "puts a key that exactly fills the budget in the current range",
			keys: []KeySize[int]{{1, 40}, {2, 60}, {3, 100}},
			want: []KeyRange[int]{
				{End: keyPtr(3), First: 1, Size: 100},
				{Start: keyPtr(3), First: 3, Size: 100},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SplitKeyRanges(tc.keys, cmp.Compare[int], budget)
			require.Equal(t, tc.want, got)
		})
	}

	t.Run("returns the same ranges for any input order", func(t *testing.T) {
		keys := []KeySize[int]{{1, 60}, {2, 30}, {3, 20}, {4, 50}, {5, 40}}
		reversed := []KeySize[int]{{5, 40}, {4, 50}, {3, 20}, {2, 30}, {1, 60}}
		require.Equal(t,
			SplitKeyRanges(keys, cmp.Compare[int], budget),
			SplitKeyRanges(reversed, cmp.Compare[int], budget),
		)
	})

	t.Run("places every key in exactly one range", func(t *testing.T) {
		keys := []KeySize[int]{{1, 60}, {2, 30}, {3, 250}, {4, 50}, {5, 40}, {6, 5}}
		ranges := SplitKeyRanges(keys, cmp.Compare[int], budget)
		for key := 0; key <= 7; key++ {
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
			SplitKeyRanges([]KeySize[int]{{1, 1}}, cmp.Compare[int], 0)
		})
	})
}

func TestKeyRange_Contains(t *testing.T) {
	tests := []struct {
		name string
		r    KeyRange[int]
		key  int
		want bool
	}{
		{name: "includes the start key", r: KeyRange[int]{Start: keyPtr(2), End: keyPtr(4)}, key: 2, want: true},
		{name: "excludes the end key", r: KeyRange[int]{Start: keyPtr(2), End: keyPtr(4)}, key: 4, want: false},
		{name: "excludes a key below the start", r: KeyRange[int]{Start: keyPtr(2), End: keyPtr(4)}, key: 1, want: false},
		{name: "includes any low key without a start", r: KeyRange[int]{End: keyPtr(4)}, key: -100, want: true},
		{name: "includes any high key without an end", r: KeyRange[int]{Start: keyPtr(2)}, key: 100, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.r.Contains(tc.key, cmp.Compare[int]))
		})
	}
}
