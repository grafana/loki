package workflow

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-kit/log"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

// Test performs an end-to-end test of a workflow, asserting that:
//
//  1. All streams get registered before running tasks.
//  2. There is never more than one sender/receiver for a stream.
//  3. The root task has a pipeline listener.
//  4. Closing the pipeline properly cleans up tasks and streams.
//
// Some of these assertions are handled by [fakeRunner], which returns an error
// when used improperly.
func Test(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var physicalGraph dag.Graph[physical.Node]

		var (
			scan      = physicalGraph.Add(&physical.DataObjScan{})
			rangeAgg  = physicalGraph.Add(&physical.RangeAggregation{})
			vectorAgg = physicalGraph.Add(&physical.VectorAggregation{})
		)

		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: rangeAgg, Child: scan})
		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: vectorAgg, Child: rangeAgg})

		physicalPlan := physical.FromGraph(physicalGraph)

		fr := newFakeRunner()

		wf, err := New(t.Context(), Options{}, log.NewNopLogger(), fr, physicalPlan)
		require.NoError(t, err, "workflow should construct properly")
		require.NotNil(t, wf.resultsStream, "workflow should have created results stream")

		defer func() {
			wf.Close()

			// Closing the workflow should remove all remaining streams and tasks.
			require.Len(t, fr.streams, 0, "all streams should be removed after closing the workflow")
			require.Len(t, fr.tasks, 0, "all tasks should be removed after closing the workflow")
		}()

		defer func() {
			if !t.Failed() {
				return
			}

			t.Log("Failing workflow:")
			t.Log(Sprint(wf))
		}()

		// Run returns an error if any of the methods in our fake runner failed.
		p, err := wf.Run(t.Context())
		require.NoError(t, err, "Workflow should start properly")
		defer p.Close()

		synctest.Wait()
		require.Equal(t, len(wf.taskStates), 2, "workflow should have enqueued two tasks")

		rs, ok := fr.streams[wf.resultsStream.ULID]
		require.True(t, ok, "results stream should be registered in runner")
		require.NotEqual(t, ulid.Zero, rs.Sender, "results stream should have a sender")
		require.Equal(t, ulid.Zero, rs.TaskReceiver, "results stream should not have a task receiver")
		require.NotNil(t, rs.Listener, "results stream should have a listener")

		// Check to make sure all known tasks have been given to the runner.
		for _, task := range wf.allTasks() {
			_, exist := fr.tasks[task.ULID]
			require.True(t, exist, "workflow should give all tasks to runner (task %s is missing)", task.ULID)
		}
	})
}

// TestCancellation tests that a task entering a terminal state cancels all
// downstream tasks.
func TestCancellation(t *testing.T) {
	var physicalGraph dag.Graph[physical.Node]

	var (
		scan        = physicalGraph.Add(&physical.DataObjScan{})
		parallelize = physicalGraph.Add(&physical.Parallelize{})
		rangeAgg    = physicalGraph.Add(&physical.RangeAggregation{})
		vectorAgg   = physicalGraph.Add(&physical.VectorAggregation{})
	)

	_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: parallelize, Child: scan})
	_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: rangeAgg, Child: parallelize})
	_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: vectorAgg, Child: rangeAgg})

	physicalPlan := physical.FromGraph(physicalGraph)

	terminalStates := []TaskState{TaskStateCancelled, TaskStateCompleted, TaskStateFailed}
	for _, state := range terminalStates {
		t.Run(state.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				fr := newFakeRunner()
				wf, err := New(t.Context(), Options{}, log.NewNopLogger(), fr, physicalPlan)
				require.NoError(t, err, "workflow should construct properly")
				require.NotNil(t, wf.resultsStream, "workflow should have created results stream")

				defer func() {
					if !t.Failed() {
						return
					}

					t.Log("Failing workflow:")
					t.Log(Sprint(wf))
				}()

				// Run returns an error if any of the methods in our fake runner failed.
				p, err := wf.Run(t.Context())
				require.NoError(t, err, "Workflow should start properly")
				defer p.Close()

				// Wait for the workflow to register all tasks with the runner.
				synctest.Wait()

				rootTask, err := wf.graph.Root()
				require.NoError(t, err, "should be able to retrieve singular root task")

				rt, ok := fr.tasks[rootTask.ULID]
				require.True(t, ok, "root task should be registered with runner")

				// Notify the workflow that the root task has entered a terminal
				// state.
				rt.handler(t.Context(), rootTask, TaskStatus{State: state})

				// Wait for the workflow to process all status updates.
				synctest.Wait()

				_ = wf.graph.Walk(rootTask, func(n *Task) error {
					if n == rootTask {
						return nil
					}

					require.Equal(t, TaskStateCancelled, wf.taskStates[n], "downstream task %s should be canceled", n.ULID)
					return nil
				}, dag.PreOrderWalk)
			})
		})
	}
}

// TestFailedTaskSurfacesErrorBeforeEOF reproduces a race where a failed task's
// error is lost because the worker closes the results-stream sink (signaling
// EOF) before the workflow learns the task failed and calls
// resultsPipeline.SetError. A sink-only task (the root task emits no records
// and is its own root) maximizes the window: the worker closes the results
// sink immediately, then reports TaskStateFailed.
//
// The consumer (e.g. the dataobj compactor's runPlan) reads the results
// pipeline to io.EOF and treats EOF as success. If the error is lost, a failed
// compaction task looks successful and the coordinator swaps the ToC to point
// at index objects that were never written.
//
// This drives the handlers in the order a real worker produces them:
//  1. results stream -> StreamStateClosed   (worker closes all sinks on failure)
//  2. root task      -> TaskStateFailed      (worker reports terminal status)
//
// and asserts the consumer pipeline surfaces the failure rather than EOF.
func TestFailedTaskSurfacesErrorBeforeEOF(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var physicalGraph dag.Graph[physical.Node]
		_ = physicalGraph.Add(&physical.DataObjScan{})
		physicalPlan := physical.FromGraph(physicalGraph)

		fr := newFakeRunner()
		wf, err := New(t.Context(), Options{}, log.NewNopLogger(), fr, physicalPlan)
		require.NoError(t, err, "workflow should construct properly")

		p, err := wf.Run(t.Context())
		require.NoError(t, err, "workflow should start properly")
		defer p.Close()

		synctest.Wait()

		rootTask, err := wf.graph.Root()
		require.NoError(t, err, "should be able to retrieve singular root task")
		rt, ok := fr.tasks[rootTask.ULID]
		require.True(t, ok, "root task should be registered with runner")

		wantErr := errors.New("index merge failed: object not found")

		// The consumer drains the results pipeline in a goroutine, blocked on
		// Read, exactly as runPlan does.
		require.NoError(t, p.Open(t.Context()))
		type readResult struct {
			err error
		}
		done := make(chan readResult, 1)
		go func() {
			_, readErr := p.Read(t.Context())
			done <- readResult{err: readErr}
		}()

		// Let the consumer goroutine block in Read.
		synctest.Wait()

		// 1. Worker closes the results-stream sink first (the EOF signal), before
		//    the workflow has learned the task failed, so errCond is not closed.
		rs, ok := fr.streams[wf.resultsStream.ULID]
		require.True(t, ok, "results stream should be registered with runner")
		rs.Handler(t.Context(), wf.resultsStream, StreamStateClosed)

		// Let the consumer goroutine react to the stream close.
		synctest.Wait()

		// 2. Worker then reports the task failure.
		rt.handler(t.Context(), rootTask, TaskStatus{State: TaskStateFailed, Error: wantErr})

		synctest.Wait()

		// The consumer must observe the failure, not a clean EOF. A lost error
		// makes a failed task look successful.
		res := <-done
		require.ErrorIs(t, res.err, wantErr,
			"consumer must surface the task failure instead of EOF")
	})
}

// TestShortCircuiting tests that a running task updating its contributing time range cancels irrelevant
// downstream tasks.
func TestShortCircuiting(t *testing.T) {
	var physicalGraph dag.Graph[physical.Node]

	now := time.Now()
	var (
		scan = physicalGraph.Add(&physical.ScanSet{
			Targets: []*physical.ScanTarget{
				{
					Type: physical.ScanTypeDataObject,
					DataObject: &physical.DataObjScan{
						MaxTimeRange: physical.TimeRange{
							Start: now.Add(-time.Hour),
							End:   now,
						},
					},
				},
				{
					Type: physical.ScanTypeDataObject,
					DataObject: &physical.DataObjScan{
						MaxTimeRange: physical.TimeRange{
							Start: now.Add(-2 * time.Hour),
							End:   now.Add(-time.Hour),
						},
					},
				},
				{
					Type: physical.ScanTypeDataObject,
					DataObject: &physical.DataObjScan{
						MaxTimeRange: physical.TimeRange{
							Start: now.Add(-3 * time.Hour),
							End:   now.Add(-2 * time.Hour),
						},
					},
				},
			},
		})
		parallelize = physicalGraph.Add(&physical.Parallelize{})
		topk        = physicalGraph.Add(&physical.TopK{
			SortBy:    &physical.ColumnExpr{Ref: semconv.ColumnIdentTimestamp.ColumnRef()},
			Ascending: false,
			K:         100,
		})
	)

	_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: parallelize, Child: scan})
	_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: topk, Child: parallelize})

	physicalPlan := physical.FromGraph(physicalGraph)

	synctest.Test(t, func(t *testing.T) {
		fr := newFakeRunner()
		wf, err := New(t.Context(), Options{}, log.NewNopLogger(), fr, physicalPlan)
		require.NoError(t, err, "workflow should construct properly")
		require.NotNil(t, wf.resultsStream, "workflow should have created results stream")

		defer func() {
			if !t.Failed() {
				return
			}

			t.Log("Failing workflow:")
			t.Log(Sprint(wf))
		}()

		// Run returns an error if any of the methods in our fake runner failed.
		p, err := wf.Run(t.Context())
		require.NoError(t, err, "Workflow should start properly")
		defer p.Close()

		// Wait for the workflow to register all tasks with the runner.
		synctest.Wait()

		rootTask, err := wf.graph.Root()
		require.NoError(t, err, "should be able to retrieve singular root task")

		rt, ok := fr.tasks[rootTask.ULID]
		require.True(t, ok, "root task should be registered with runner")

		// Notify the workflow that the root task has updated its contributing time range.
		rt.handler(t.Context(), rootTask, TaskStatus{
			State: TaskStateRunning,
			ContributingTimeRange: ContributingTimeRange{
				Timestamp: now.Add(-45 * time.Minute),
				LessThan:  false,
			},
		})

		// Wait for the workflow to process all status updates.
		synctest.Wait()

		_ = wf.graph.Walk(rootTask, func(n *Task) error {
			// Skip root task
			if n == rootTask {
				return nil
			}

			// Skip tasks that have intersecting time range
			if n.MaxTimeRange.End.After(now.Add(-45 * time.Minute)) {
				return nil
			}

			// Tasks should be canceled
			require.Equal(t, TaskStateCancelled, wf.taskStates[n], "downstream task %s should be canceled", n.ULID)
			return nil
		}, dag.PreOrderWalk)
	})
}

func TestWorkflowDispatchesAllTasks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const numScanTasks = 100

		var physicalGraph dag.Graph[physical.Node]

		scanSet := &physical.ScanSet{}
		for i := range numScanTasks {
			scanSet.Targets = append(scanSet.Targets, &physical.ScanTarget{
				Type: physical.ScanTypeDataObject,
				DataObject: &physical.DataObjScan{
					Section: i,
				},
			})
		}

		var (
			rangeAgg = physicalGraph.Add(&physical.RangeAggregation{})
			parallel = physicalGraph.Add(&physical.Parallelize{})
			_        = physicalGraph.Add(scanSet)
		)

		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: rangeAgg, Child: parallel})
		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: parallel, Child: scanSet})

		fr := newFakeRunner()
		opts := Options{Tenant: "tenant"}
		wf, err := New(t.Context(), opts, log.NewNopLogger(), fr, physical.FromGraph(physicalGraph))
		require.NoError(t, err, "workflow should construct properly")
		require.NotNil(t, wf.resultsStream, "workflow should have created results stream")

		defer func() {
			if !t.Failed() {
				return
			}

			t.Log("Failing workflow:")
			t.Log(Sprint(wf))
		}()

		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		t.Cleanup(cancel)

		p, err := wf.Run(ctx)
		require.NoError(t, err, "Workflow should start properly")
		defer p.Close()

		synctest.Wait()
		require.Len(t, wf.taskStates, len(wf.allTasks()), "expected every task to be dispatched")
	})
}

type fakeRunner struct {
	streams  map[ulid.ULID]*runnerStream
	tasks    map[ulid.ULID]*runnerTask
	tasksMtx sync.RWMutex
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{
		streams: make(map[ulid.ULID]*runnerStream),
		tasks:   make(map[ulid.ULID]*runnerTask),
	}
}

func (f *fakeRunner) RegisterManifest(_ context.Context, manifest *Manifest) error {
	f.tasksMtx.Lock()
	defer f.tasksMtx.Unlock()

	var (
		manifestStreams = make(map[ulid.ULID]*runnerStream, len(manifest.Streams))
		manifestTasks   = make(map[ulid.ULID]*runnerTask, len(manifest.Tasks))
	)

	for _, stream := range manifest.Streams {
		if _, exist := f.streams[stream.ULID]; exist {
			return fmt.Errorf("stream %s already added", stream.ULID)
		} else if _, exist := manifestStreams[stream.ULID]; exist {
			return fmt.Errorf("stream %s already added in manifest", stream.ULID)
		}

		manifestStreams[stream.ULID] = &runnerStream{
			Stream:  stream,
			Handler: manifest.StreamEventHandler,
		}
	}
	for _, task := range manifest.Tasks {
		if _, exist := f.tasks[task.ULID]; exist {
			return fmt.Errorf("task %s already added", task.ULID)
		} else if _, exist := manifestTasks[task.ULID]; exist {
			return fmt.Errorf("task %s already added in manifest", task.ULID)
		}

		for _, streams := range task.Sinks {
			for _, stream := range streams {
				rs, exist := manifestStreams[stream.ULID]
				if !exist {
					return fmt.Errorf("sink stream %s not found in manifest", stream.ULID)
				} else if rs.Sender != ulid.Zero {
					return fmt.Errorf("stream %s already bound to sender %s", stream.ULID, rs.Sender)
				}

				rs.Sender = task.ULID
			}
		}
		for _, streams := range task.Sources {
			for _, stream := range streams {
				rs, exist := manifestStreams[stream.ULID]
				if !exist {
					return fmt.Errorf("source stream %s not found in manifest", stream.ULID)
				} else if rs.TaskReceiver != ulid.Zero {
					return fmt.Errorf("source stream %s already bound to %s", stream.ULID, rs.TaskReceiver)
				} else if rs.Listener != nil {
					return fmt.Errorf("source stream %s already bound to local receiver", stream.ULID)
				}

				rs.TaskReceiver = task.ULID
			}
		}

		manifestTasks[task.ULID] = &runnerTask{
			task:    task,
			handler: manifest.TaskEventHandler,
		}
	}

	// If we got to this point, the manifest is valid and we can copy everything
	// over into the map.
	maps.Copy(f.streams, manifestStreams)
	maps.Copy(f.tasks, manifestTasks)
	return nil
}

func (f *fakeRunner) UnregisterManifest(_ context.Context, manifest *Manifest) error {
	f.tasksMtx.Lock()
	defer f.tasksMtx.Unlock()

	// Validate that everything in the manifest was registered.
	for _, stream := range manifest.Streams {
		if _, exist := f.streams[stream.ULID]; !exist {
			return fmt.Errorf("stream %s not found", stream.ULID)
		}
	}
	for _, task := range manifest.Tasks {
		if _, exist := f.tasks[task.ULID]; !exist {
			return fmt.Errorf("task %s not found", task.ULID)
		}
	}

	for _, stream := range manifest.Streams {
		delete(f.streams, stream.ULID)
	}
	for _, task := range manifest.Tasks {
		delete(f.tasks, task.ULID)
	}

	return nil
}

func (f *fakeRunner) Listen(_ context.Context, writer RecordWriter, stream *Stream) error {
	f.tasksMtx.Lock()
	defer f.tasksMtx.Unlock()

	rs, exist := f.streams[stream.ULID]
	if !exist {
		return fmt.Errorf("stream %s not found", stream.ULID)
	} else if rs.Listener != nil || rs.TaskReceiver != ulid.Zero {
		return fmt.Errorf("stream %s already bound", stream.ULID)
	}

	rs.Listener = writer
	return nil
}

func (f *fakeRunner) Start(ctx context.Context, tasks ...*Task) error {
	var errs []error

	for _, task := range tasks {
		f.tasksMtx.Lock()
		var (
			rt, exist = f.tasks[task.ULID]
		)
		f.tasksMtx.Unlock()

		if !exist {
			errs = append(errs, fmt.Errorf("task %s not registered", task.ULID))
			continue
		}

		// Inform handler of task state change.
		rt.handler(ctx, task, TaskStatus{State: TaskStatePending})
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (f *fakeRunner) Cancel(ctx context.Context, tasks ...*Task) error {
	var errs []error

	for _, task := range tasks {
		f.tasksMtx.RLock()
		var (
			rt, exist = f.tasks[task.ULID]
		)
		f.tasksMtx.RUnlock()

		if !exist {
			errs = append(errs, fmt.Errorf("task %s not found", task.ULID))
			continue
		}

		// Inform handler of task state change.
		rt.handler(ctx, task, TaskStatus{State: TaskStateCancelled})
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// runnerStream wraps a stream with the edges of the connection. A stream sender
// is always another task, while the receiver is either another task or a
// pipeline owned by the runner, given to the workflow.
type runnerStream struct {
	Stream  *Stream
	Handler StreamEventHandler

	TaskReceiver ulid.ULID    // Task listening for messages on stream.
	Listener     RecordWriter // Pipeline listening for messages on stream.

	Sender ulid.ULID
}

type runnerTask struct {
	task    *Task
	handler TaskEventHandler
}
