package workflow

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/go-kit/log"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/schedulerstat"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/xcap"
)

func TestFlushTaskSummaries_FlagsCriticalPath(t *testing.T) {
	newTask := func(name byte) *Task {
		var id ulid.ULID
		id[len(id)-1] = name
		return &Task{ULID: id, Fragment: physical.FromGraph(dag.Graph[physical.Node]{})}
	}

	var graph dag.Graph[*Task]
	a := graph.Add(newTask('A'))
	b := graph.Add(newTask('B'))
	c := graph.Add(newTask('C'))
	require.NoError(t, graph.AddEdge(dag.Edge[*Task]{Parent: a, Child: b}))
	require.NoError(t, graph.AddEdge(dag.Edge[*Task]{Parent: a, Child: c}))

	// C finishes after B, so the critical path is A -> C; B is off the path.
	_, capture := xcap.NewCapture(context.Background(), nil)
	terminal := func(finish int64) pendingSummary {
		return pendingSummary{oldState: TaskStateRunning, status: TaskStatus{State: TaskStateCompleted, Capture: capture}, taskFinishNanos: finish}
	}

	var buf bytes.Buffer
	wf := &Workflow{
		logger:           log.NewLogfmtLogger(&buf),
		graph:            graph,
		pendingSummaries: map[*Task]pendingSummary{a: terminal(100), b: terminal(20), c: terminal(40)},
	}

	wf.flushTaskSummaries()

	out := buf.String()
	require.Equal(t, 3, strings.Count(out, "msg=task-summary"), "one summary per task")

	lineFor := func(task *Task) string {
		for _, ln := range strings.Split(out, "\n") {
			if strings.Contains(ln, " task_id="+task.ULID.String()) {
				return ln
			}
		}
		return ""
	}
	require.Contains(t, lineFor(a), "on_critical_path=true")
	require.Contains(t, lineFor(c), "on_critical_path=true")
	require.Contains(t, lineFor(b), "on_critical_path=false")
}

func TestOnTaskChange_DefersSummary(t *testing.T) {
	var graph dag.Graph[*Task]
	task := graph.Add(&Task{ULID: ulid.Make(), Fragment: physical.FromGraph(dag.Graph[physical.Node]{})})

	wf := &Workflow{
		logger:           log.NewNopLogger(),
		runner:           newFakeRunner(),
		graph:            graph,
		taskStates:       make(map[*Task]TaskState),
		resultsPipeline:  newStreamPipe(),
		pendingSummaries: make(map[*Task]pendingSummary),
	}
	capCtx, capture := xcap.NewCapture(context.Background(), nil)
	_, region := xcap.StartRegion(capCtx, "task")
	region.Record(schedulerstat.TaskFinishTime.Observe(42))

	// A terminal transition records the summary and finish time together,
	// deferred (not logged).
	wf.onTaskChange(context.Background(), task, TaskStatus{State: TaskStateCompleted, Capture: capture})
	require.Len(t, wf.pendingSummaries, 1)
	require.Contains(t, wf.pendingSummaries, task)
	require.Equal(t, int64(42), wf.pendingSummaries[task].taskFinishNanos)

	// A duplicate terminal notification does not double-record.
	wf.onTaskChange(context.Background(), task, TaskStatus{State: TaskStateCompleted, Capture: capture})
	require.Len(t, wf.pendingSummaries, 1)
}

func TestCriticalPath(t *testing.T) {
	// newTask returns a task with a deterministic, recognizable ULID so that
	// failures point back at a named node.
	newTask := func(name byte) *Task {
		var id ulid.ULID
		id[len(id)-1] = name
		return &Task{ULID: id}
	}

	// buildGraph constructs a DAG from parent->children edges. Tasks are
	// referenced by name and shared across edges via the returned lookup.
	buildGraph := func(edges map[byte][]byte) (dag.Graph[*Task], map[byte]*Task) {
		var graph dag.Graph[*Task]
		tasks := map[byte]*Task{}
		get := func(name byte) *Task {
			if _, ok := tasks[name]; !ok {
				tasks[name] = graph.Add(newTask(name))
			}
			return tasks[name]
		}
		for parent, children := range edges {
			p := get(parent)
			for _, child := range children {
				c := get(child)
				require.NoError(t, graph.AddEdge(dag.Edge[*Task]{Parent: p, Child: c}))
			}
		}
		return graph, tasks
	}

	tests := []struct {
		name string
		// edges describes the DAG as parent -> children.
		edges map[byte][]byte
		// finish maps task name to its finish time signal.
		finish map[byte]int64
		// want is the expected critical path by task name, root first.
		want []byte
	}{
		{
			name:   "single task",
			edges:  map[byte][]byte{'A': nil},
			finish: map[byte]int64{'A': 10},
			want:   []byte{'A'},
		},
		{
			name: "linear chain",
			edges: map[byte][]byte{
				'A': {'B'},
				'B': {'C'},
			},
			finish: map[byte]int64{'A': 30, 'B': 20, 'C': 10},
			want:   []byte{'A', 'B', 'C'},
		},
		{
			name: "picks latest-finishing child at fan-out",
			edges: map[byte][]byte{
				'A': {'B', 'C'},
				'B': {'D'},
				'C': {'E'},
			},
			// C finishes later than B, so the path follows A -> C -> E.
			finish: map[byte]int64{'A': 100, 'B': 40, 'C': 60, 'D': 10, 'E': 20},
			want:   []byte{'A', 'C', 'E'},
		},
		{
			name: "deeper branch can be shorter when its gating child finishes last",
			edges: map[byte][]byte{
				'A': {'B', 'C'},
				'C': {'D', 'E'},
			},
			// B finishes after C, so the path stops at the B leaf.
			finish: map[byte]int64{'A': 100, 'B': 90, 'C': 50, 'D': 10, 'E': 20},
			want:   []byte{'A', 'B'},
		},
		{
			name: "multiple roots starts from latest-finishing root",
			edges: map[byte][]byte{
				'A': {'C'},
				'B': {'D'},
			},
			// Root B finishes after root A, so the path starts at B.
			finish: map[byte]int64{'A': 50, 'B': 80, 'C': 10, 'D': 20},
			want:   []byte{'B', 'D'},
		},
		{
			name: "missing finish times still produce a path",
			edges: map[byte][]byte{
				'A': {'B', 'C'},
			},
			// No finish times recorded: ties resolve to first child in order.
			finish: map[byte]int64{},
			want:   []byte{'A', 'B'},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			graph, tasks := buildGraph(tc.edges)

			summaries := map[*Task]pendingSummary{}
			for name, value := range tc.finish {
				summary := pendingSummary{taskFinishNanos: value}
				summaries[tasks[name]] = summary
			}

			got := criticalPath(&graph, summaries)

			gotNames := make([]byte, len(got))
			for i, task := range got {
				gotNames[i] = task.ULID[len(task.ULID)-1]
			}
			require.Equal(t, tc.want, gotNames)
		})
	}
}

func TestCriticalPath_EmptyGraph(t *testing.T) {
	var graph dag.Graph[*Task]
	require.Nil(t, criticalPath(&graph, map[*Task]pendingSummary{}))
}
