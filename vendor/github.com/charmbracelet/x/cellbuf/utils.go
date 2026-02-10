package cellbuf

import (
	"strings"
)

// Height returns the height of a string.
func Height(s string) int {
	return strings.Count(s, "\n") + 1
}

func clamp(v, low, high int) int {
	if high < low {
		low, high = high, low
	}
	return min(high, max(low, v))
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}
