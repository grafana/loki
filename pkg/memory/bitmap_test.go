package memory

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_bitset_Set(t *testing.T) {
	var set bitmap
	set.Resize(64)

	// Set some individual bits
	set.Set(0, true)
	set.Set(3, true)
	set.Set(7, true)

	var expect uint64 = 0b1000_1001

	require.Len(t, set, 1)
	require.Equal(t, expect, set[0])
}

func Test_bitset_Clear(t *testing.T) {
	var set bitmap
	set.Resize(64)

	// First set some bits
	set.SetRange(0, 10, true)

	// Clear some bits
	set.Set(1, false)
	set.Set(3, false)
	set.Set(8, false)

	var expect uint64 = 0b0010_1111_0101

	require.Len(t, set, 1)
	require.Equal(t, expect, set[0])
}

func Test_bitset_SetRange(t *testing.T) {
	var set bitmap
	set.Resize(64)
	set.SetRange(0, 5, true)
	set.SetRange(7, 10, true)

	var expect uint64 = 0b0011_1001_1111 // Should be set in LSB ordering.

	require.Len(t, set, 1)
	require.Equal(t, expect, set[0])
}

func Test_bitset_ClearRange(t *testing.T) {
	var set bitmap
	set.Resize(64)

	// First set some bits
	set.SetRange(0, 15, true)

	// Clear some ranges
	set.SetRange(2, 6, false)
	set.SetRange(8, 12, false)

	var expect uint64 = 0b0111_0000_1100_0011

	require.Len(t, set, 1)
	require.Equal(t, expect, set[0], "expected %b, got %b", expect, set[0])
}

func Test_bitset_Resize(t *testing.T) {
	tt := []struct {
		elements int
		words    int
	}{
		{elements: 0, words: 0},
		{elements: 1, words: 1},
		{elements: 64, words: 1},
		{elements: 65, words: 2},
		{elements: 128, words: 2},
	}
	for _, tc := range tt {
		t.Run(fmt.Sprintf("elements=%d", tc.elements), func(t *testing.T) {
			var set bitmap
			set.Resize(tc.elements)
			require.Len(t, set, tc.words)
		})
	}
}

func Test_bitset_IterValue_true(t *testing.T) {
	var set bitmap
	set.Resize(128) // Two words: 64 bits each

	bitsToSet := []int{1, 3, 5, 65, 70, 127}
	for _, bit := range bitsToSet {
		set.Set(bit, true)
	}

	var indices []int
	for index := range set.IterValues(128, true) {
		indices = append(indices, index)
	}

	expected := []int{1, 3, 5, 65, 70, 127}
	require.Equal(t, expected, indices)
}

func Test_bitset_IterValue_false(t *testing.T) {
	var set bitmap
	set.Resize(128) // Two words: 64 bits each

	// Set all bits first
	set.SetRange(0, 128, true)

	bitsToClear := []int{0, 2, 4, 64, 69, 126}
	for _, bit := range bitsToClear {
		set.Set(bit, false)
	}

	var indices []int
	for index := range set.IterValues(128, false) {
		indices = append(indices, index)
	}

	expected := []int{0, 2, 4, 64, 69, 126}
	require.Equal(t, expected, indices)
}
