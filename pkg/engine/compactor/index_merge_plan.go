package compactor

import (
	"fmt"
	"slices"
	"time"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

// indexMergePlan is the IndexMerge merges of one layout group. Each merge
// merges some runs of the group into one new index run, with one or more
// tasks.
//
// The plan holds only the merges. It derives the physical plans, the output
// runs and the source paths from the merges and the group when they run, so a
// change to the merges needs no other update.
//
// The zero value has no work.
type indexMergePlan struct {
	key    layoutKey
	group  *layoutGroup
	merges []splitIndexMerge
}

// hasWork reports whether p dispatches any task.
func (p indexMergePlan) hasWork() bool {
	return len(p.merges) > 0
}

// taskCount returns the number of tasks of p.
func (p indexMergePlan) taskCount() int {
	var tasks int
	for _, merge := range p.merges {
		tasks += len(merge.tasks)
	}
	return tasks
}

// physicalPlans returns the physical plan of each task, in merge order and
// then in task order.
func (p indexMergePlan) physicalPlans(tenant string, window time.Time) []*physical.Plan {
	plans := make([]*physical.Plan, 0, p.taskCount())
	for _, merge := range p.merges {
		plans = append(plans, merge.plans(tenant, window)...)
	}
	return plans
}

// plannedRuns returns the planned run of each merge, in merge order. It
// returns an error if a merge reads no index, or an index outside the group.
func (p indexMergePlan) plannedRuns() ([]plannedRun, error) {
	entries := p.group.entries()
	entriesByPath := make(map[string]indexpointers.IndexPointer, len(entries))
	for _, entry := range entries {
		entriesByPath[entry.Path] = entry
	}

	planned := make([]plannedRun, len(p.merges))
	for i, merge := range p.merges {
		inputRange, err := indexTaskBounds(merge.spec, entriesByPath)
		if err != nil {
			return nil, fmt.Errorf("merge %d: %w", i, err)
		}
		// A merge keeps the layout of its inputs, so the output has the
		// layout of the group. Every task of the merge writes into one run,
		// so the run takes the time range of the whole merge.
		planned[i] = plannedRun{timeRange: inputRange, shardBuckets: p.group.shardBuckets()}
	}
	return planned, nil
}

// joinArtifacts returns the artifacts of each merge, in merge order. results
// holds the artifacts of each task, in the order of physicalPlans. It returns
// an error if results does not hold one entry for each task, or if a merge
// rejects the artifacts of its tasks.
func (p indexMergePlan) joinArtifacts(results [][]v2.ResultArtifact) ([][]v2.ResultArtifact, error) {
	if len(results) != p.taskCount() {
		return nil, fmt.Errorf("got the artifacts of %d index-compaction tasks for %d tasks", len(results), p.taskCount())
	}
	artifacts := make([][]v2.ResultArtifact, len(p.merges))
	for i, merge := range p.merges {
		joined, err := merge.join(results[:len(merge.tasks)])
		if err != nil {
			return nil, fmt.Errorf("index merge %d: %w", i, err)
		}
		artifacts[i] = joined
		results = results[len(merge.tasks):]
	}
	return artifacts, nil
}

// sourcePaths returns the paths of the index objects that the merges of p
// read, sorted and without duplicates. The swap removes them from the ToC.
func (p indexMergePlan) sourcePaths() []string {
	var paths []string
	for _, merge := range p.merges {
		for _, run := range merge.spec.Runs {
			for _, section := range run.Sections {
				paths = append(paths, section.ObjectPath)
			}
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}
