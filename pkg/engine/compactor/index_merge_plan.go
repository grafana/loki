package compactor

import (
	"fmt"
	"slices"
	"time"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

// indexMergePlan is the IndexMerge tasks of one layout group. Each task merges
// some runs of the group into one new index run.
//
// The plan holds only the tasks. It derives the physical plans, the output
// runs and the source paths from the tasks and the group when the merge runs,
// so a change to the tasks needs no other update.
//
// The zero value has no work.
type indexMergePlan struct {
	key   layoutKey
	group *layoutGroup
	tasks []*compactionv2pb.TaskSpec
}

// hasWork reports whether p dispatches any task.
func (p indexMergePlan) hasWork() bool {
	return len(p.tasks) > 0
}

// physicalPlans returns the physical plan of each task, in task order.
func (p indexMergePlan) physicalPlans(tenant string, window time.Time) []*physical.Plan {
	plans := make([]*physical.Plan, len(p.tasks))
	for i, task := range p.tasks {
		plans[i] = buildIndexMergePlan(tenant, window, task)
	}
	return plans
}

// plannedRuns returns the planned run of each task, in task order. It returns
// an error if a task reads no index, or an index outside the group.
func (p indexMergePlan) plannedRuns() ([]plannedRun, error) {
	entries := p.group.entries()
	entriesByPath := make(map[string]indexpointers.IndexPointer, len(entries))
	for _, entry := range entries {
		entriesByPath[entry.Path] = entry
	}

	planned := make([]plannedRun, len(p.tasks))
	for i, task := range p.tasks {
		inputRange, err := indexTaskBounds(task, entriesByPath)
		if err != nil {
			return nil, fmt.Errorf("task %d: %w", i, err)
		}
		// A merge keeps the layout of its inputs, so the output has the
		// layout of the group.
		planned[i] = plannedRun{timeRange: inputRange, shardBuckets: p.group.shardBuckets()}
	}
	return planned, nil
}

// sourcePaths returns the paths of the index objects that the tasks of p read,
// sorted and without duplicates. The swap removes them from the ToC.
func (p indexMergePlan) sourcePaths() []string {
	var paths []string
	for _, task := range p.tasks {
		for _, run := range task.Runs {
			for _, section := range run.Sections {
				paths = append(paths, section.ObjectPath)
			}
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}
