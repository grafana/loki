package parquet

import "github.com/parquet-go/parquet-go/internal/bytealg"

func countLevelsEqual(levels []byte, value byte) int {
	return bytealg.Count(levels, value)
}

func countLevelsNotEqual(levels []byte, value byte) int {
	return len(levels) - countLevelsEqual(levels, value)
}

func appendLevel(levels []byte, value byte, count int) []byte {
	i := len(levels)
	n := len(levels) + count

	if cap(levels) < n {
		newLevels := make([]byte, n, 2*n)
		copy(newLevels, levels)
		levels = newLevels
	} else {
		levels = levels[:n]
	}

	bytealg.Broadcast(levels[i:], value)
	return levels
}
