package main

import "slices"

// PercentileStats and calcPercentiles are intentionally duplicated from
// cmd/dataobj-inspect/stats.go rather than shared, to keep this tool
// dependency-free from cmd/dataobj-inspect. Keep the two in sync by hand if
// either's percentile logic changes.
//
// PercentileStats holds a set of percentile statistics computed from a slice
// of numeric values.
type PercentileStats struct {
	Median float64
	P95    float64
	P99    float64
	Max    float64
}

// calcPercentiles calculates percentile statistics from the provided slice and
// stores them in s. The input slice is sorted in place.
func calcPercentiles[T int | uint | int64 | uint64 | float64](input []T, s *PercentileStats) {
	if len(input) == 0 {
		return
	}
	// Data must be sorted to calculate percentiles.
	slices.Sort(input)
	n := len(input)
	if n%2 == 0 {
		s.Median = float64(input[n/2-1]+input[n/2]) / 2.0
	} else {
		s.Median = float64(input[n/2])
	}
	idx := int(float64(n) * 0.95)
	if idx >= n {
		idx = n - 1
	}
	s.P95 = float64(input[idx])
	idx = int(float64(n) * 0.99)
	if idx >= n {
		idx = n - 1
	}
	s.P99 = float64(input[idx])
	s.Max = float64(input[n-1])
}
