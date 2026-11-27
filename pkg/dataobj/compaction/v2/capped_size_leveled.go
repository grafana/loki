package compactionv2

import (
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

// CappedSizeLeveledStrategy is a [SizeLeveledStrategy] that merges a group of
// runs only if their summed uncompressed size is below a maximum run size.
// It keeps the runs of every other group unmerged.
//
// The maximum is exclusive to match the level bounds. With the maximum at the
// lower bound of a level, no merge writes a run in that level or above,
// because a merge writes at most the summed size of its inputs.
type CappedSizeLeveledStrategy struct {
	inner *SizeLeveledStrategy

	// maxRunUncompressedBytes is the exclusive upper bound of the summed
	// uncompressed size of the runs in one merge. 0 means no maximum.
	maxRunUncompressedBytes uint64
}

// NewCappedSizeLeveledStrategy returns a strategy that plans like inner, but
// merges a group of runs only if their summed uncompressed size is below
// maxRunUncompressedBytes. A maxRunUncompressedBytes of 0 means no maximum.
func NewCappedSizeLeveledStrategy(inner *SizeLeveledStrategy, maxRunUncompressedBytes uint64) *CappedSizeLeveledStrategy {
	return &CappedSizeLeveledStrategy{inner: inner, maxRunUncompressedBytes: maxRunUncompressedBytes}
}

// NeedsCompaction reports whether Plan returns at least one merge task.
func (s *CappedSizeLeveledStrategy) NeedsCompaction(runs []Run) bool {
	merges, _, _ := s.plan(runs, "")
	return len(merges) > 0
}

// CappedRuns returns the number of runs that Plan does not merge, because
// their group sums to the maximum run size or above.
func (s *CappedSizeLeveledStrategy) CappedRuns(runs []Run) int {
	_, _, capped := s.plan(runs, "")
	return capped
}

// Plan returns the merge tasks of the inner strategy whose runs sum to below
// the maximum run size. It returns in unmerged the runs that the inner
// strategy does not merge, followed by the runs of the dropped tasks.
//
// Plan does not look for another group of runs that fits below the maximum.
func (s *CappedSizeLeveledStrategy) Plan(runs []Run, tenant string) (merges []*compactionv2pb.TaskSpec, unmerged []Run) {
	merges, unmerged, _ = s.plan(runs, tenant)
	return merges, unmerged
}

// plan is Plan that also returns the number of runs of the dropped tasks.
//
// It relies on the inner strategy to plan one task per group of runs, so the
// size of a task is the size of its group.
func (s *CappedSizeLeveledStrategy) plan(runs []Run, tenant string) (merges []*compactionv2pb.TaskSpec, unmerged []Run, capped int) {
	planned, unmerged := s.inner.Plan(runs, tenant)
	for _, task := range planned {
		if s.maxRunUncompressedBytes == 0 || taskSize(task) < s.maxRunUncompressedBytes {
			merges = append(merges, task)
			continue
		}
		for _, ref := range task.Runs {
			unmerged = append(unmerged, runRef{ref})
		}
		capped += len(task.Runs)
	}
	return merges, unmerged, capped
}

// taskSize returns the summed uncompressed size of the sections of the runs
// of task.
func taskSize(task *compactionv2pb.TaskSpec) uint64 {
	var size uint64
	for _, ref := range task.Runs {
		size += runRef{ref}.Size()
	}
	return size
}

// runRef is a [Run] backed by a planned run reference.
type runRef struct {
	ref *compactionv2pb.RunRef
}

func (r runRef) Sections() []*compactionv2pb.SectionRef { return r.ref.Sections }

func (r runRef) Size() uint64 {
	var size uint64
	for _, section := range r.ref.Sections {
		size += uint64(section.UncompressedSize)
	}
	return size
}
