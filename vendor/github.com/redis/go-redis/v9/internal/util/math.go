package util

import "time"

// Max returns the maximum of two integers
func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Min returns the minimum of two integers
func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// MinDuration returns the minimum of two time.Duration values
func MinDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
