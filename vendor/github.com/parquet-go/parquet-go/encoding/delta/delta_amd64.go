//go:build !purego

package delta

const (
	padding = 64
)

func findNegativeLength(lengths []int32) int {
	for _, n := range lengths {
		if n < 0 {
			return int(n)
		}
	}
	return -1
}
