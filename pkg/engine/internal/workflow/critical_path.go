package workflow

import (
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

// logCriticalPath emits the per-query critical-path log lines, one per task on
// the path. It is called from Close, after UnregisterManifest has driven every
// task to a terminal state and recorded its finish time, so the task DAG and
// all per-task finish times are available.
func (wf *Workflow) logCriticalPath() {
	wf.tasksMut.RLock()
	path := criticalPath(&wf.graph, wf.taskFinish)
	wf.tasksMut.RUnlock()

	total := len(path)
	for position, task := range path {
		level.Info(wf.logger).Log(
			"msg", "critical-path",
			"query_id", wf.opts.ID,
			"task_id", task.ULID,
			"cp_position", position,
			"cp_total_length", total,
		)
	}
}

// criticalPath approximates the query's critical path through the task DAG,
// returning the tasks on the path ordered from root (position 0) down to a
// leaf.
//
// The path is built by walking the DAG downward from a root: at each node the
// child with the latest finish time is chosen as the gating child. Finish time
// (the absolute terminal timestamp recorded by the scheduler in
// [schedulerstat.TaskFinishTime]) is a more honest "what gated this node"
// signal than raw duration, but this is still an approximation: the true
// critical path is determined by which child delivered the last batch a parent
// needed, which the workflow does not observe.
//
// Tasks participate regardless of terminal status (success, failure, or
// cancellation): selection is purely by finish time. Short-circuited or
// pre-assignment-cancelled tasks tend to finish early and so are rarely chosen
// as the gating child, which matches the intent.
//
// If the graph has multiple roots, criticalPath starts from the root with the
// latest finish time. Query workflows have a single root today (the physical
// planner enforces exactly one root node), so this only matters for
// hypothetical multi-root graphs.
//
// A task missing from finish is treated as having finish time 0, so a path is
// still produced even if some finish times were never recorded.
func criticalPath(graph *dag.Graph[*Task], finish map[*Task]int64) []*Task {
	current := latestFinisher(graph.Roots(), finish)
	if current == nil {
		return nil
	}

	var path []*Task
	for current != nil {
		path = append(path, current)
		current = latestFinisher(graph.Children(current), finish)
	}
	return path
}

// latestFinisher returns the task in candidates with the greatest finish time,
// or nil if candidates is empty. Ties are broken by keeping the first task in
// iteration order, which keeps selection deterministic for a given graph.
func latestFinisher(candidates []*Task, finish map[*Task]int64) *Task {
	var (
		best     *Task
		bestTime int64
	)
	for _, candidate := range candidates {
		t := finish[candidate]
		if best == nil || t > bestTime {
			best, bestTime = candidate, t
		}
	}
	return best
}
