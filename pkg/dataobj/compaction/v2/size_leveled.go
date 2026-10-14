package compactionv2

import (
	"fmt"
	"math"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

const (
	// DefaultSizeLevelBase is the default upper bound of level 0, in bytes of
	// uncompressed log data. Empirically, a fresh run is one object of about 6GiB. One
	// fresh run stays in level 0, and a merge of three or more moves up a
	// level.
	DefaultSizeLevelBase uint64 = 16 << 30

	// DefaultSizeLevelRatio is the default size ratio between levels. It
	// matches the default log merge fan-in (K) of 8. For levels 1 and above,
	// a merge of 8 runs from one level lands in the next level. Level 0 has
	// no lower bound, so this does not hold there.
	DefaultSizeLevelRatio uint64 = 8
)

// SizeLeveledStrategy groups runs into levels by their uncompressed size.
// Runs in the same level have a similar size, so merging them keeps write
// amplification low.
//
// Level 0 holds runs below base. Level n, for n >= 1, holds runs in
// [base*ratio^(n-1), base*ratio^n). The top level has no upper size bound.
//
// A level is full when it holds k runs, and a merge task holds at most k
// runs.
type SizeLeveledStrategy struct {
	base  uint64
	ratio uint64
	k     int
}

// NewSizeLeveledStrategy returns a strategy with the given level 0 bound,
// ratio between levels, and maximum runs per merge task k.
//
// base must be greater than 0 and ratio must be at least 2. k must be at
// least 2, because with k of 1 a single run always needs compaction and
// compaction never stops.
func NewSizeLeveledStrategy(base, ratio uint64, k int) (*SizeLeveledStrategy, error) {
	if base == 0 {
		return nil, fmt.Errorf("size level base must be greater than 0")
	}
	if ratio < 2 {
		return nil, fmt.Errorf("size level ratio must be at least 2, got %d", ratio)
	}
	if k < 2 {
		return nil, fmt.Errorf("runs per merge task must be at least 2, got %d", k)
	}
	return &SizeLeveledStrategy{base: base, ratio: ratio, k: k}, nil
}

// level returns the index of the level that holds a run of the given size.
func (s *SizeLeveledStrategy) level(size uint64) int {
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

// groupByLevels returns runs grouped by level based on each run's uncompressed size.
// The result has one entry per level up to the highest level that holds a run,
// and empty levels are nil. Runs keep their input order within each level.
func (s *SizeLeveledStrategy) groupByLevels(runs []Run) [][]Run {
	var levels [][]Run
	for _, run := range runs {
		level := s.level(run.Size())
		if level >= len(levels) {
			levels = append(levels, make([][]Run, level+1-len(levels))...)
		}
		levels[level] = append(levels[level], run)
	}
	return levels
}

// RunsPerLevel returns the number of runs in each level, from level 0 up to
// the highest level that holds a run.
func (s *SizeLeveledStrategy) RunsPerLevel(runs []Run) []int {
	levels := s.groupByLevels(runs)
	counts := make([]int, len(levels))
	for i, level := range levels {
		counts[i] = len(level)
	}
	return counts
}

// NeedsCompaction reports whether any level holds at least k runs.
//
// A window where every level holds fewer than k runs counts as converged,
// even if its runs overlap. Plan merges the k runs of a full level into one
// run, so each compaction reduces the number of runs and compaction stops.
func (s *SizeLeveledStrategy) NeedsCompaction(runs []Run) bool {
	for _, level := range s.groupByLevels(runs) {
		if len(level) >= s.k {
			return true
		}
	}
	return false
}

// Plan splits each level into groups of at most k runs, so no group mixes
// levels. Each group of two or more runs becomes a merge task.
//
// A run alone in its level, or left over after the split, has nothing to
// merge with. Plan returns it in unmerged instead of a task, so the caller can
// keep it without rewriting its data. Every run lands in exactly one merge
// task or in unmerged.
func (s *SizeLeveledStrategy) Plan(runs []Run, tenant string, sortSchema []string) (merges []*compactionv2pb.TaskSpec, unmerged []Run) {
	for _, level := range s.groupByLevels(runs) {
		for start := 0; start < len(level); start += s.k {
			group := level[start:min(start+s.k, len(level))]
			if len(group) == 1 {
				unmerged = append(unmerged, group[0])
				continue
			}
			merges = append(merges, Plan(group, tenant, len(group), sortSchema)...)
		}
	}
	return merges, unmerged
}
