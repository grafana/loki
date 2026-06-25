package workflow

import (
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

// pendingSummary is a terminal task's state, retained for its deferred summary
// (see Workflow.pendingSummaries).
type pendingSummary struct {
	oldState TaskState
	status   TaskStatus

	taskFinishNanos int64
}

// flushTaskSummaries logs the deferred per-task summaries at Close, flagging the
// tasks on the critical path. onTaskChange records each summary and finish time
// under the terminal-state lock, so on the normal path all are present once the
// results pipeline closes (every task terminal).
func (wf *Workflow) flushTaskSummaries() {
	wf.tasksMut.RLock()
	defer wf.tasksMut.RUnlock()

	onPath := make(map[*Task]struct{})
	for _, task := range criticalPath(&wf.graph, wf.pendingSummaries) {
		onPath[task] = struct{}{}
	}

	// TODO(rfratto): make Workflow.Close a hard barrier; a fail-fast query can drop
	// a sibling that finishes at the instant of teardown (failing task unaffected).
	for t, s := range wf.pendingSummaries {
		_, critical := onPath[t]
		wf.printTaskSummary(t, s.oldState, s.status, critical)
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
// A task missing from summaries is treated as having finish time 0, so a path
// is still produced even if some finish times were never recorded.
func criticalPath(graph *dag.Graph[*Task], summaries map[*Task]pendingSummary) []*Task {
	current := latestFinisher(graph.Roots(), summaries)
	if current == nil {
		return nil
	}

	var path []*Task
	for current != nil {
		path = append(path, current)
		current = latestFinisher(graph.Children(current), summaries)
	}
	return path
}

// latestFinisher returns the task in candidates with the greatest finish time,
// or nil if candidates is empty. Ties are broken by keeping the first task in
// iteration order, which keeps selection deterministic for a given graph.
func latestFinisher(candidates []*Task, summaries map[*Task]pendingSummary) *Task {
	var (
		best     *Task
		bestTime int64
	)
	for _, candidate := range candidates {
		t := summaries[candidate].taskFinishNanos
		if best == nil || t > bestTime {
			best, bestTime = candidate, t
		}
	}
	return best
}
