package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStringSliceContains(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		inputSlice []string
		inputValue string
		expected   bool
	}{
		"should return false on missing value in the slice": {
			inputSlice: []string{"one", "two"},
			inputValue: "three",
			expected:   false,
		},
		"should return true on existing value in the slice": {
			inputSlice: []string{"one", "two"},
			inputValue: "two",
			expected:   true,
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			actual := StringSliceContains(testData.inputSlice, testData.inputValue)
			assert.Equal(t, testData.expected, actual)
		})
	}
}
