package compactionv2

import (
	"fmt"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

// CompareFunc compares two ordering keys.
type CompareFunc[K any] func(a, b K) int

// Section associates a section reference with its inclusive ordering-key bounds.
type Section[K any] struct {
	Ref      *compactionv2pb.SectionRef
	Min, Max K
}

// Run is one strictly ordered sequence of sections.
type Run interface {
	// Sections returns the run's sections in sorted order.
	Sections() []*compactionv2pb.SectionRef
	// Size returns the sum of the run's sections' UncompressedSize.
	Size() uint64
}

// CalculateRuns groups sections into N disjoint, contiguous runs based on the Min and Max sort keys.
// Sections of the same object are treated as one indivisible unit and always land in the same run.
// It panics if a section has a nil Ref.
func CalculateRuns[K any](sections []Section[K], compare CompareFunc[K]) []Run {
	if len(sections) == 0 {
		return nil
	}
	groups := groupSectionsByObject(sections, compare)
	calculated := calculateRuns(groups, compare)

	runs := make([]Run, len(calculated))
	for i, run := range calculated {
		runs[i] = run
	}
	return runs
}

// IsConverged reports whether a set of sections have no positive
// overlap. Touching bounds are considered converged because rewriting cannot remove their
// ambiguity. It does not mutate sections.
func IsConverged[K any](sections []Section[K], compare CompareFunc[K]) bool {
	return isConverged(sections, compare, false)
}

// IsConvergedWithInclusiveOverlap reports whether a set of sections have no overlap,
// treating equal object boundaries as overlapping. It is useful when keys are
// prefixes whose omitted suffixes may interleave.
func IsConvergedWithInclusiveOverlap[K any](sections []Section[K], compare CompareFunc[K]) bool {
	return isConverged(sections, compare, true)
}

func isConverged[K any](sections []Section[K], compare CompareFunc[K], equalityOverlaps bool) bool {
	objects := groupSectionsByObject(sections, compare)
	if len(objects) <= 1 {
		return true
	}

	maxKey := objects[0].max
	for _, obj := range objects[1:] {
		comparison := compare(maxKey, obj.min)
		if comparison > 0 || (equalityOverlaps && comparison == 0) {
			return false
		}
		if compare(obj.max, maxKey) > 0 {
			maxKey = obj.max
		}
	}
	return true
}

// BelowMinCompactionSize reports whether the runs' total size is below minSize.
func BelowMinCompactionSize(runs []Run, minSize uint64) bool {
	var total uint64
	for _, run := range runs {
		total += run.Size()
	}
	return total < minSize
}

// Plan groups [runs] into ceil(P/K) task batches: runs [0..K) -> task
// 0, runs [K..2K) -> task 1, ... The output is deterministic for a given input.
//
// Special cases:
//   - len(runs) == 0 -> returns nil (no tasks).
//   - k >= P         -> returns a single TaskSpec containing all runs.
func Plan(runs []Run, tenant string, k int, sortSchema []string) []*compactionv2pb.TaskSpec {
	if k <= 0 {
		panic(fmt.Sprintf("k must be > 0, got %d", k))
	}
	if len(runs) == 0 {
		return nil
	}

	refs := make([]*compactionv2pb.RunRef, len(runs))
	for i, run := range runs {
		refs[i] = &compactionv2pb.RunRef{Sections: run.Sections()}
	}

	numTasks := (len(refs) + k - 1) / k
	tasks := make([]*compactionv2pb.TaskSpec, 0, numTasks)
	for start := 0; start < len(refs); start += k {
		end := min(start+k, len(refs))
		tasks = append(tasks, &compactionv2pb.TaskSpec{
			Tenant:     tenant,
			Runs:       refs[start:end],
			SortSchema: sortSchema,
		})
	}
	return tasks
}
