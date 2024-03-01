package drain

import (
	"reflect"
	"testing"
)

func TestForRange(t *testing.T) {
	testCases := []struct {
		name     string
		volume   *Volume
		start    int64
		end      int64
		expected *Volume
	}{
		{
			name:     "Empty Volume",
			volume:   &Volume{},
			start:    1,
			end:      10,
			expected: &Volume{},
		},
		{
			name:     "No Overlap",
			volume:   &Volume{Values: [][2]int64{{1, 2}, {3, 4}, {5, 6}}},
			start:    10,
			end:      20,
			expected: &Volume{},
		},
		{
			name:     "Complete Overlap",
			volume:   &Volume{Values: [][2]int64{{1, 2}, {3, 4}, {5, 6}}},
			start:    0,
			end:      10,
			expected: &Volume{Values: [][2]int64{{1, 2}, {3, 4}, {5, 6}}},
		},
		{
			name:     "Partial Overlap",
			volume:   &Volume{Values: [][2]int64{{1, 2}, {3, 4}, {5, 6}}},
			start:    2,
			end:      4,
			expected: &Volume{Values: [][2]int64{{3, 4}}},
		},
		{
			name:     "Single Element in Range",
			volume:   &Volume{Values: [][2]int64{{1, 2}, {3, 4}, {5, 6}}},
			start:    3,
			end:      4,
			expected: &Volume{Values: [][2]int64{{3, 4}}},
		},
		{
			name:     "Start Before First Element",
			volume:   &Volume{Values: [][2]int64{{1, 2}, {3, 4}, {5, 6}}},
			start:    0,
			end:      4,
			expected: &Volume{Values: [][2]int64{{1, 2}, {3, 4}}},
		},
		{
			name:     "End After Last Element",
			volume:   &Volume{Values: [][2]int64{{1, 2}, {3, 4}, {5, 6}}},
			start:    4,
			end:      10,
			expected: &Volume{Values: [][2]int64{{5, 6}}},
		},
		{
			name:     "Start and End Before First Element",
			volume:   &Volume{Values: [][2]int64{{1, 2}, {3, 4}, {5, 6}}},
			start:    0,
			end:      1,
			expected: &Volume{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.volume.ForRange(tc.start, tc.end)
			if !reflect.DeepEqual(result, tc.expected) {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}
