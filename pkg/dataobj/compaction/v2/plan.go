package compactionv2

import (
	"context"
	"fmt"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

// Plan sorts the input sections into P runs and groups them into
// ⌈P/K⌉ task batches: runs [0..K) -> task 0, runs [K..2K) -> task 1, ...
//
// The output is deterministic for a given input regardless of the input order.
//
// Special cases:
//   - len(sections) == 0  -> returns nil (no tasks).
//   - k >= P              -> returns a single TaskSpec containing all runs.
//
// K must be greater than 0. Plan sorts the input sections slice in place;
// callers that need the original order must copy beforehand. The contract
// requires non-nil SectionRef entries.
//
// The ctx parameter is currently not used. The algorithm runs to completion
// without honoring cancellation, because partial output would violate the
// determinism contract.
func Plan(
	ctx context.Context,
	sections []compactionv2pb.SectionRef,
	tenant string,
	k int,
) []*compactionv2pb.TaskSpec {
	_ = ctx
	if k <= 0 {
		panic(fmt.Sprintf("k must be > 0, got %d", k))
	}
	if len(sections) == 0 {
		return nil
	}

	calculated := calculateRuns(sections)
	if len(calculated) == 0 {
		return nil
	}

	// Materialize runs as RunRefs in creation order.
	runs := make([]compactionv2pb.RunRef, len(calculated))
	for i, r := range calculated {
		runs[i] = compactionv2pb.RunRef{Sections: r.sections}
	}

	// Group into ⌈P/K⌉ TaskSpec batches.
	numTasks := (len(runs) + k - 1) / k
	tasks := make([]*compactionv2pb.TaskSpec, 0, numTasks)
	for start := 0; start < len(runs); start += k {
		end := min(start+k, len(runs))
		tasks = append(tasks, &compactionv2pb.TaskSpec{
			Tenant: tenant,
			Runs:   runs[start:end],
		})
	}
	return tasks
}
