package compactionv2

import (
	"context"
	"fmt"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

// Plan patience-sorts the input sections into P runs and groups them into
// ⌈P/K⌉ task batches: runs [0..K) -> task 0, runs [K..2K) -> task 1, ...
//
// The output is deterministic for a given input regardless of the input order:
// see the spec § Compaction unit / Phase 1 for the algorithm.
//
// Special cases:
//   - len(sections) == 0  -> returns nil (no tasks).
//   - k >= P              -> returns a single TaskSpec containing all runs.
//
// Panics if k <= 0: that's a programmer error in the caller's config plumbing,
// not a runtime condition. k validation always runs first; if both k <= 0 and
// len(sections) == 0, the function panics (not nil-returns). Tests pin this
// ordering.
//
// Plan sorts the input sections slice in place; callers that need the original
// order must copy beforehand. The contract requires non-nil SectionRef entries;
// nil entries will panic.
//
// The ctx parameter is reserved for future use; the current implementation runs
// the algorithm to completion without honoring cancellation, because partial
// output would violate the determinism contract.
func Plan(
	ctx context.Context,
	sections []*compactionv2pb.SectionRef,
	tenant string,
	k int,
) []*compactionv2pb.TaskSpec {
	_ = ctx
	if k <= 0 {
		panic(fmt.Sprintf("compactionv2.Plan: k must be > 0, got %d", k))
	}
	if len(sections) == 0 {
		return nil
	}

	piles := patienceSort(sections)
	if len(piles) == 0 {
		return nil
	}

	// Materialize piles as RunRefs in creation order.
	runs := make([]*compactionv2pb.RunRef, len(piles))
	for i, p := range piles {
		runs[i] = &compactionv2pb.RunRef{Sections: p.sections}
	}

	// Group into ⌈P/K⌉ TaskSpec batches.
	numTasks := (len(runs) + k - 1) / k
	tasks := make([]*compactionv2pb.TaskSpec, 0, numTasks)
	for start := 0; start < len(runs); start += k {
		end := start + k
		if end > len(runs) {
			end = len(runs)
		}
		tasks = append(tasks, &compactionv2pb.TaskSpec{
			Tenant: tenant,
			Runs:   runs[start:end],
		})
	}
	return tasks
}
