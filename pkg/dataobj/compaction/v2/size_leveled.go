package compactionv2

import (
	"fmt"
	"math"
)

const (
	// DefaultSizeLevelBase is the default upper bound of level 0, in bytes of
	// uncompressed log data. A typical fresh run (composed of a single object) holds about 6GiB
	// so 16GiB ensures that a merge will move up a level while accounting for size variance.
	DefaultSizeLevelBase uint64 = 16 << 30

	// DefaultSizeLevelRatio is the default size ratio between levels. It
	// matches a merge fan-in (K) of 8, so a merge of 8 runs from one level
	// lands in the next level.
	DefaultSizeLevelRatio uint64 = 8
)

// SizeLeveledStrategy groups runs into levels by their uncompressed size.
// Runs in the same level have a similar size, so merging them keeps write
// amplification low.
//
// Level 0 holds runs below base. Level n, for n >= 1, holds runs in
// [base*ratio^(n-1), base*ratio^n). The number of levels has no upper limit.
type SizeLeveledStrategy struct {
	base  uint64
	ratio uint64
}

// NewSizeLeveledStrategy returns a strategy with the given level 0 bound and
// ratio between levels. base must be greater than 0 and ratio must be at
// least 2.
func NewSizeLeveledStrategy(base, ratio uint64) (*SizeLeveledStrategy, error) {
	if base == 0 {
		return nil, fmt.Errorf("size level base must be greater than 0")
	}
	if ratio < 2 {
		return nil, fmt.Errorf("size level ratio must be at least 2, got %d", ratio)
	}
	return &SizeLeveledStrategy{base: base, ratio: ratio}, nil
}

// Level returns the index of the level that holds a run of the given size.
func (s *SizeLeveledStrategy) Level(size uint64) int {
	level := 0
	for bound := s.base; size >= bound; level++ {
		// The next bound would overflow uint64, so no size can reach it.
		if bound > math.MaxUint64/s.ratio {
			return level + 1
		}
		bound *= s.ratio
	}
	return level
}

// GroupByLevels returns runs grouped by level based on each run's uncompressed size.
// The result has one entry per level up to the highest level that holds a run,
// and empty levels are nil. Runs keep their input order within each level.
func (s *SizeLeveledStrategy) GroupByLevels(runs []Run) [][]Run {
	var levels [][]Run
	for _, run := range runs {
		level := s.Level(run.Size())
		if level >= len(levels) {
			levels = append(levels, make([][]Run, level+1-len(levels))...)
		}
		levels[level] = append(levels[level], run)
	}
	return levels
}
