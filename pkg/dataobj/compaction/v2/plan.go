package compactionv2

import (
	"fmt"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

// Run is a sorted sequence of sections that a K-way merge consumes as one input.
//
// Section bounds are inclusive [min, max]. Within a run each section's max is <=
// the next's min, so touching sections (max == min) stay in one run and
// overlapping sections (max > min) split.
type Run interface {
	// Sections returns the run's sections in sorted order.
	Sections() []*compactionv2pb.SectionRef
	// Size returns the sum of the run's sections' UncompressedSize.
	Size() uint64
}

// CalculateRuns sorts the provided sections in place and groups them into the
// fewest [Run]s. The result is deterministic for a given input.
func CalculateRuns(sections []*compactionv2pb.SectionRef) []Run {
	calculated := calculateRuns(sections)
	runs := make([]Run, len(calculated))
	for i, r := range calculated {
		runs[i] = r
	}
	return runs
}

// IsTerminal reports whether a converged window is already a single run (or
// none), or when the total data across all runs is below minCompactionSize.
func IsTerminal(runs []Run, minCompactionSize uint64) bool {
	if len(runs) <= 1 {
		return true
	}
	var total uint64
	for _, r := range runs {
		total += r.Size()
	}
	return total < minCompactionSize
}

// Plan groups runs into ceil(P/K) task batches: runs [0..K) -> task
// 0, runs [K..2K) -> task 1, ... The output is deterministic for a given input.
//
// Special cases:
//   - len(runs) == 0 -> returns nil (no tasks).
//   - k >= P         -> returns a single TaskSpec containing all runs.
func Plan(
	runs []Run,
	tenant string,
	k int,
	sortSchema []string,
) []*compactionv2pb.TaskSpec {
	if k <= 0 {
		panic(fmt.Sprintf("k must be > 0, got %d", k))
	}
	if len(runs) == 0 {
		return nil
	}

	refs := make([]*compactionv2pb.RunRef, len(runs))
	for i, r := range runs {
		refs[i] = &compactionv2pb.RunRef{Sections: r.Sections()}
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
