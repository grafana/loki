//go:build purego || !amd64

package parquet

import "cmp"

func orderOfInt32(data []int32) int     { return orderOf(data) }
func orderOfInt64(data []int64) int     { return orderOf(data) }
func orderOfUint32(data []uint32) int   { return orderOf(data) }
func orderOfUint64(data []uint64) int   { return orderOf(data) }
func orderOfFloat32(data []float32) int { return orderOf(data) }
func orderOfFloat64(data []float64) int { return orderOf(data) }

func orderOf[T cmp.Ordered](data []T) int {
	if len(data) > 1 {
		if orderIsAscending(data) {
			return +1
		}
		if orderIsDescending(data) {
			return -1
		}
	}
	return 0
}

func orderIsAscending[T cmp.Ordered](data []T) bool {
	for i := len(data) - 1; i > 0; i-- {
		if data[i-1] > data[i] {
			return false
		}
	}
	return true
}

func orderIsDescending[T cmp.Ordered](data []T) bool {
	for i := len(data) - 1; i > 0; i-- {
		if data[i-1] < data[i] {
			return false
		}
	}
	return true
}
