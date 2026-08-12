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

// CalculateRuns sorts sections in place and groups them into strict runs. It panics if a section has a nil Ref.
func CalculateRuns[K any](sections []Section[K], compare CompareFunc[K]) []Run {
	calculated := calculateRuns(sections, compare)
	runs := make([]Run, len(calculated))
	for i, run := range calculated {
		runs[i] = run
	}
	return runs
}

// CalculateObjectRuns groups sections by physical object before calculating
// runs. Sections within an object are kept in section-index order and treated
// as an indivisible, already-ordered chain.
func CalculateObjectRuns[K any](sections []Section[K], compare CompareFunc[K]) []Run {
	calculated := calculateObjectRuns(sections, compare)
	runs := make([]Run, len(calculated))
	for i, run := range calculated {
		runs[i] = run
	}
	return runs
}

// GroupObjectRuns returns exactly one run per physical object. Sections remain
// in section-index order and each object is ordered deterministically by its
// key envelope and path.
func GroupObjectRuns[K any](sections []Section[K], compare CompareFunc[K]) []Run {
	chains := buildObjectChains(sections, compare)
	runs := make([]Run, 0, len(chains))
	for _, chain := range chains {
		refs := make([]*compactionv2pb.SectionRef, len(chain.sections))
		for i, section := range chain.sections {
			refs[i] = section.Ref
		}
		runs = append(runs, &run[K]{sections: refs, topMax: chain.max})
	}
	return runs
}

// IsConverged reports whether sections have no positive overlap. Touching
// bounds are converged because rewriting cannot remove their ambiguity. It
// does not mutate sections.
func IsConverged[K any](sections []Section[K], compare CompareFunc[K]) bool {
	sections = append([]Section[K](nil), sections...)
	sortSections(sections, compare)
	if len(sections) <= 1 {
		return true
	}

	maxKey := sections[0].Max
	for _, section := range sections[1:] {
		if compare(maxKey, section.Min) > 0 {
			return false
		}
		if compare(section.Max, maxKey) > 0 {
			maxKey = section.Max
		}
	}
	return true
}

// AreObjectsConverged reports whether physical objects have no positive overlap.
// Sections within an object are trusted to be ordered by section index and are
// compared as one object envelope. Touching object bounds are converged because
// rewriting cannot remove their ambiguity. It does not mutate sections.
func AreObjectsConverged[K any](sections []Section[K], compare CompareFunc[K]) bool {
	sections = append([]Section[K](nil), sections...)
	chains := buildObjectChains(sections, compare)
	if len(chains) <= 1 {
		return true
	}

	maxKey := chains[0].max
	for _, chain := range chains[1:] {
		if compare(maxKey, chain.min) > 0 {
			return false
		}
		if compare(chain.max, maxKey) > 0 {
			maxKey = chain.max
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
	return PlanWithOptions(runs, tenant, k, PlanOptions{SortSchema: sortSchema})
}

// PlanOptions configures the physical layout and operation of planned tasks.
type PlanOptions struct {
	SortSchema  []string
	SortOnly    bool
	StreamOrder compactionv2pb.StreamOrder
	ShardCount  uint32
}

// PlanWithOptions groups runs into task batches and stamps each task with its
// target physical layout.
func PlanWithOptions(runs []Run, tenant string, k int, opts PlanOptions) []*compactionv2pb.TaskSpec {
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
			Tenant:      tenant,
			Runs:        refs[start:end],
			SortSchema:  opts.SortSchema,
			SortOnly:    opts.SortOnly,
			StreamOrder: opts.StreamOrder,
			ShardCount:  opts.ShardCount,
		})
	}
	return tasks
}
