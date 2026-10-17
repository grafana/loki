package workflow

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// waitCollected forces GC repeatedly and reports whether done was closed (i.e.
// the finalizer for a released object fired) within a short window.
func waitCollected(done <-chan struct{}) bool {
	for range 100 {
		runtime.GC()
		select {
		case <-done:
			return true
		case <-time.After(5 * time.Millisecond):
		}
	}
	return false
}

// TestRecordTaskResult_ReleasesFragment verifies that producing a terminal
// result captures the fragment-derived summary fields and then releases the
// task's heavy Fragment/CachedSources, so they aren't retained until the
// workflow is torn down at Close.
func TestRecordTaskResult_ReleasesFragment(t *testing.T) {
	g := dag.Graph[physical.Node]{}
	scan := &physical.DataObjScan{}
	g.Add(scan)

	task := &Task{
		ULID:          ulid.Make(),
		Fragment:      physical.FromGraph(g),
		CachedSources: map[physical.Node]CachedSources{scan: {[]byte("cached")}},
	}

	wf := &Workflow{taskResults: map[*Task]pendingSummary{}}

	_, capture := xcap.NewCapture(context.Background(), nil)

	wf.tasksMut.Lock()
	wf.recordTaskResult(task, TaskResult{Outcome: TaskOutcomeCompleted, Capture: capture})
	wf.tasksMut.Unlock()

	// The fragment-derived summary fields were captured before release, so the
	// deferred task-summary log still has everything it needs.
	summary := wf.taskResults[task]
	require.NotEmpty(t, summary.operatorType, "operator type should be captured before the fragment is released")
	require.True(t, summary.isScanTask, "scan-task flag should be captured before the fragment is released")

	// The heavy fragment state is released immediately on terminal.
	require.Nil(t, task.Fragment, "fragment should be released once the task is terminal")
	require.Nil(t, task.CachedSources, "cached sources should be released once the task is terminal")
}

// TestRecordTaskResult_FragmentIsCollectable proves the memory win of releasing
// the fragment on terminal: once a task has a result, its physical plan
// fragment becomes unreachable and is collected even though the *Task (kept
// alive by wf.manifest/graph) is still live.
//
// Without the release in recordTaskResult, the fragment stays reachable via the
// task until Workflow.Close, so the finalizer would not fire and this would fail.
func TestRecordTaskResult_FragmentIsCollectable(t *testing.T) {
	g := dag.Graph[physical.Node]{}
	g.Add(&physical.DataObjScan{})
	frag := physical.FromGraph(g)

	task := &Task{ULID: ulid.Make(), Fragment: frag}
	wf := &Workflow{taskResults: map[*Task]pendingSummary{}}

	collected := make(chan struct{})
	runtime.SetFinalizer(frag, func(*physical.Plan) { close(collected) })
	frag = nil //nolint:ineffassign // drop the test's own reference to the plan

	wf.tasksMut.Lock()
	wf.recordTaskResult(task, TaskResult{Outcome: TaskOutcomeCompleted})
	wf.tasksMut.Unlock()

	require.True(t, waitCollected(collected),
		"task fragment must be GC-eligible once the task is terminal; without the release in recordTaskResult it stays reachable via the *Task until Workflow.Close")

	// The *Task itself is still alive; only its Fragment was released.
	require.Nil(t, task.Fragment)
	runtime.KeepAlive(task)
}

// TestRecordTaskResult_ReleasesFragmentMemory quantifies the win: it gives many
// finished tasks a large CachedSources payload and shows that releasing it on
// terminal frees the payload while the workflow (the tasks slice) stays alive.
//
// Revert the `task.Fragment = nil` / `task.CachedSources = nil` lines in
// recordTaskResult and `freed` drops to ~0, failing the assertion.
func TestRecordTaskResult_ReleasesFragmentMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping heap-measurement test in -short mode")
	}

	const (
		numTasks    = 500
		payloadSize = 256 << 10 // 256 KiB per task => ~128 MiB total
	)

	wf := &Workflow{taskResults: make(map[*Task]pendingSummary, numTasks)}
	tasks := make([]*Task, numTasks)
	for i := range tasks {
		scan := &physical.DataObjScan{}
		g := dag.Graph[physical.Node]{}
		g.Add(scan)
		tasks[i] = &Task{
			ULID:          ulid.Make(),
			Fragment:      physical.FromGraph(g),
			CachedSources: map[physical.Node]CachedSources{scan: {make([]byte, payloadSize)}},
		}
	}

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	wf.tasksMut.Lock()
	for _, task := range tasks {
		wf.recordTaskResult(task, TaskResult{Outcome: TaskOutcomeCompleted})
	}
	wf.tasksMut.Unlock()

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	// The tasks slice is still alive (simulating an open workflow), yet each
	// task's Fragment/CachedSources were released, so the payload is freed.
	freed := int64(before.HeapAlloc) - int64(after.HeapAlloc)
	total := int64(numTasks) * payloadSize
	t.Logf("HeapAlloc before=%d MiB after=%d MiB freed=%d MiB (payload ~%d MiB)",
		before.HeapAlloc>>20, after.HeapAlloc>>20, freed>>20, total>>20)

	require.Greater(t, freed, total/2,
		"releasing Fragment/CachedSources on terminal should free most of the payload while the workflow stays open")

	runtime.KeepAlive(tasks)
}
