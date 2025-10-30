package workflow

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/go-kit/log"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
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

	wf, err := New(log.NewNopLogger(), "", fr, physicalPlan)
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

	defer func() {
		p.Close()

		// Closing the pipeline should remove all remaining streams and tasks.
		require.Len(t, fr.streams, 0, "all streams should be removed after closing the pipeline")
		require.Len(t, fr.tasks, 0, "all streams should be removed after closing the pipeline")
	}()

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
}

// TestCancellation tasks that a task entering a terminal state cancels all
// downstream tasks.
func TestCancellation(t *testing.T) {
	var physicalGraph dag.Graph[physical.Node]

	var (
		scan      = physicalGraph.Add(&physical.DataObjScan{})
		rangeAgg  = physicalGraph.Add(&physical.RangeAggregation{})
		vectorAgg = physicalGraph.Add(&physical.VectorAggregation{})
	)

	_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: rangeAgg, Child: scan})
	_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: vectorAgg, Child: rangeAgg})

	physicalPlan := physical.FromGraph(physicalGraph)

	terminalStates := []TaskState{TaskStateCancelled, TaskStateCancelled, TaskStateFailed}
	for _, state := range terminalStates {
		t.Run(state.String(), func(t *testing.T) {
			fr := newFakeRunner()
			wf, err := New(log.NewNopLogger(), "", fr, physicalPlan)
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

			rootTask, err := wf.graph.Root()
			require.NoError(t, err, "should be able to retrieve singular root task")

			rt, ok := fr.tasks[rootTask.ULID]
			require.True(t, ok, "root task should be registered with runner")

			// Notify the workflow that the root task has entered a terminal
			// state.
			rt.handler(t.Context(), rootTask, TaskStatus{State: state})

			_ = wf.graph.Walk(rootTask, func(n *Task) error {
				if n == rootTask {
					return nil
				}

				require.Equal(t, TaskStateCancelled, wf.taskStates[n], "downstream task %s should be canceled", n.ULID)
				return nil
			}, dag.PreOrderWalk)
		})
	}
}

type fakeRunner struct {
	streams map[ulid.ULID]*runnerStream
	tasks   map[ulid.ULID]*runnerTask
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{
		streams: make(map[ulid.ULID]*runnerStream),
		tasks:   make(map[ulid.ULID]*runnerTask),
	}
}

func (f *fakeRunner) AddStreams(_ context.Context, handler StreamEventHandler, streams ...*Stream) error {
	for _, stream := range streams {
		if _, exist := f.streams[stream.ULID]; exist {
			return fmt.Errorf("stream %s already added", stream.ULID)
		}

		f.streams[stream.ULID] = &runnerStream{
			Stream:  stream,
			Handler: handler,
		}
	}

	return nil
}

func (f *fakeRunner) RemoveStreams(_ context.Context, streams ...*Stream) error {
	var errs []error

	for _, stream := range streams {
		if _, exist := f.streams[stream.ULID]; !exist {
			errs = append(errs, fmt.Errorf("stream %s not found", stream.ULID))
		}
		delete(f.streams, stream.ULID)
	}

	return errors.Join(errs...)
}

func (f *fakeRunner) Listen(_ context.Context, stream *Stream) (executor.Pipeline, error) {
	rs, exist := f.streams[stream.ULID]
	if !exist {
		return nil, fmt.Errorf("stream %s not found", stream.ULID)
	} else if rs.Listener != nil || rs.TaskReceiver != ulid.Zero {
		return nil, fmt.Errorf("stream %s already bound", stream.ULID)
	}

	rs.Listener = noopPipeline{}
	return rs.Listener, nil
}

func (f *fakeRunner) Start(ctx context.Context, handler TaskEventHandler, tasks ...*Task) error {
	for _, task := range tasks {
		if _, exist := f.tasks[task.ULID]; exist {
			return fmt.Errorf("task %s already added", task.ULID)
		}

		f.tasks[task.ULID] = &runnerTask{
			task:    task,
			handler: handler,
		}

		for _, streams := range task.Sinks {
			for _, stream := range streams {
				rs, exist := f.streams[stream.ULID]
				if !exist {
					return fmt.Errorf("sink stream %s not found", stream.ULID)
				} else if rs.Sender != ulid.Zero {
					return fmt.Errorf("stream %s already bound to sender %s", stream.ULID, rs.Sender)
				}

				rs.Sender = task.ULID
			}
		}
		for _, streams := range task.Sources {
			for _, stream := range streams {
				rs, exist := f.streams[stream.ULID]
				if !exist {
					return fmt.Errorf("source stream %s not found", stream.ULID)
				} else if rs.TaskReceiver != ulid.Zero {
					return fmt.Errorf("source stream %s already bound to %s", stream.ULID, rs.TaskReceiver)
				} else if rs.Listener != nil {
					return fmt.Errorf("source stream %s already bound to local receiver", stream.ULID)
				}

				rs.TaskReceiver = task.ULID
			}
		}

		// Inform handler of task state change.
		handler(ctx, task, TaskStatus{State: TaskStatePending})
	}

	return nil
}

func (f *fakeRunner) Cancel(ctx context.Context, tasks ...*Task) error {
	var errs []error

	for _, task := range tasks {
		rt, exist := f.tasks[task.ULID]
		if !exist {
			errs = append(errs, fmt.Errorf("task %s not found", task.ULID))
			continue
		}

		rt.handler(ctx, task, TaskStatus{State: TaskStateCancelled})
		delete(f.tasks, task.ULID)
	}

	return errors.Join(errs...)
}

// runnerStream wraps a stream with the edges of the connection. A stream sender
// is always another task, while the receiver is either another task or a
// pipeline owned by the runner, given to the workflow.
type runnerStream struct {
	Stream  *Stream
	Handler StreamEventHandler

	TaskReceiver ulid.ULID         // Task listening for messages on stream.
	Listener     executor.Pipeline // Pipeline listening for messages on stream.

	Sender ulid.ULID
}

type runnerTask struct {
	task    *Task
	handler TaskEventHandler
}

type noopPipeline struct{}

func (noopPipeline) Read(ctx context.Context) (arrow.Record, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (noopPipeline) Close() {}
