// Package workflow defines how to represent physical plans as distributed
// workflows.
package workflow

import (
	"context"
	"fmt"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
)

type Options struct {
	MaxRunningScanTasks  int
	MaxRunningOtherTasks int
}

// Workflow represents a physical plan that has been partitioned into
// parallelizable tasks.
type Workflow struct {
	opts          Options
	logger        log.Logger
	runner        Runner
	graph         dag.Graph[*Task]
	resultsStream *Stream

	statsMut sync.Mutex
	stats    stats.Result

	tasksMut   sync.RWMutex
	taskStates map[*Task]TaskState

	streamsMut   sync.RWMutex
	streamStates map[*Stream]StreamState

	admissionControl *admissionControl
}

// New creates a new Workflow from a physical plan. New returns an error if the
// physical plan does not have exactly one root node, or if the physical plan
// cannot be partitioned into a Workflow.
//
// The provided Runner will be used for Workflow execution.
func New(opts Options, logger log.Logger, tenantID string, runner Runner, plan *physical.Plan) (*Workflow, error) {
	graph, err := planWorkflow(tenantID, plan)
	if err != nil {
		return nil, err
	}

	// Inject a stream for final task results.
	results, err := injectResultsStream(tenantID, &graph)
	if err != nil {
		return nil, err
	}

	return &Workflow{
		opts:          opts,
		logger:        logger,
		runner:        runner,
		graph:         graph,
		resultsStream: results,

		taskStates:   make(map[*Task]TaskState),
		streamStates: make(map[*Stream]StreamState),
	}, nil
}

// injectResultsStream injects a new stream into the sinks of the root task for
// the workflow to receive final results.
func injectResultsStream(tenantID string, graph *dag.Graph[*Task]) (*Stream, error) {
	results := &Stream{ULID: ulid.Make(), TenantID: tenantID}

	// Inject a stream for final task results.
	rootTask, err := graph.Root()
	if err != nil {
		return nil, err
	}

	rootNode, err := rootTask.Fragment.Root()
	if err != nil {
		return nil, err
	}

	rootTask.Sinks[rootNode] = append(rootTask.Sinks[rootNode], results)
	return results, nil
}

// Run executes the workflow, returning a pipeline to read results from. The
// provided context is used for the lifetime of the workflow execution.
//
// The returned pipeline must be closed when the workflow is complete to release
// resources.
func (wf *Workflow) Run(ctx context.Context) (pipeline executor.Pipeline, err error) {
	var (
		streams = wf.allStreams()
		tasks   = wf.allTasks()
	)

	if err := wf.runner.AddStreams(ctx, wf.onStreamChange, streams...); err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = wf.runner.RemoveStreams(context.Background(), streams...)
		}
	}()

	pipeline, err = wf.runner.Listen(ctx, wf.resultsStream)
	if err != nil {
		return nil, err
	}

	wrapped := &wrappedPipeline{
		inner: pipeline,
		onClose: func() {
			// Cancel will return an error for any tasks that were already
			// canceled, but for convenience we give all the known tasks anyway.
			//
			// The same thing applies to RemoveStreams.
			_ = wf.runner.Cancel(context.Background(), tasks...)
			_ = wf.runner.RemoveStreams(context.Background(), streams...)

			// Merge final stats results back into the caller.
			wf.statsMut.Lock()
			defer wf.statsMut.Unlock()
			stats.JoinResults(ctx, wf.stats)
		},
	}

	wrappedHandler := func(ctx context.Context, task *Task, newStatus TaskStatus) {
		wf.onTaskChange(ctx, task, newStatus)

		if newStatus.State == TaskStateFailed {
			wrapped.SetError(newStatus.Error)
		}
	}

	// Start dispatching in background goroutine
	go func() {
		err := wf.dispatchTasks(ctx, wrappedHandler, tasks)
		if err != nil {
			wrapped.SetError(err)
			wrapped.Close()
		}
	}()

	return wrapped, nil
}

// dispatchTasks groups the slice of tasks by their associated "admission lane" (token bucket)
// and dispatches them to the runner.
// Tasks from different admission lanes are dispatched concurrently.
// The caller needs to wait on the returned error group.
func (wf *Workflow) dispatchTasks(ctx context.Context, handler TaskEventHandler, tasks []*Task) error {
	wf.admissionControl = newAdmissionControl(
		int64(wf.opts.MaxRunningScanTasks),
		int64(wf.opts.MaxRunningOtherTasks),
	)

	groups := wf.admissionControl.groupByType(tasks)
	for _, taskType := range []taskType{
		taskTypeOther,
		taskTypeScan,
	} {
		lane := wf.admissionControl.get(taskType)
		tasks := groups[taskType]

		var offset int64
		total := int64(len(tasks))
		maxBatchSize := min(total, lane.capacity)

		for ; offset < total; offset += maxBatchSize {
			batchSize := min(maxBatchSize, total-offset)
			if err := lane.Acquire(ctx, batchSize); err != nil {
				return fmt.Errorf("failed to acquire tokens from admission lane %s: %w", taskType, err)
			}
			if err := wf.runner.Start(ctx, handler, tasks[offset:offset+batchSize]...); err != nil {
				return fmt.Errorf("failed to start tasks: %w", err)
			}
		}
	}

	return nil
}

func (wf *Workflow) allStreams() []*Stream {
	var (
		result      []*Stream
		seenStreams = map[*Stream]struct{}{}
	)

	// We only iterate over sources below (for convenience), and since
	// wf.results is only used as a sink, we need to manually add it here.
	result = append(result, wf.resultsStream)
	seenStreams[wf.resultsStream] = struct{}{}

	for _, root := range wf.graph.Roots() {
		_ = wf.graph.Walk(root, func(t *Task) error {
			// Task construction guarantees that there is a sink for each source
			// (minus the results stream we generate), so there's no point in
			// iterating over Sources and Sinks.
			for _, streams := range t.Sources {
				for _, stream := range streams {
					if _, seen := seenStreams[stream]; seen {
						continue
					}
					seenStreams[stream] = struct{}{}
					result = append(result, stream)
				}
			}

			return nil
		}, dag.PreOrderWalk)
	}

	return result
}

func (wf *Workflow) allTasks() []*Task {
	var tasks []*Task

	for _, root := range wf.graph.Roots() {
		// [dag.Graph.Walk] guarantees that each node is only visited once, so
		// we can safely append the task to the list without checking if it's
		// already been seen.
		_ = wf.graph.Walk(root, func(t *Task) error {
			tasks = append(tasks, t)
			return nil
		}, dag.PreOrderWalk)
	}

	return tasks
}

func (wf *Workflow) onStreamChange(_ context.Context, stream *Stream, newState StreamState) {
	level.Debug(wf.logger).Log("msg", "stream state change", "stream_id", stream.ULID, "new_state", newState)

	wf.streamsMut.Lock()
	defer wf.streamsMut.Unlock()

	wf.streamStates[stream] = newState

	// TODO(rfratto): Do we need to do anything if a stream changes? Figuring
	// out what to do here will need to wait until the scheduler is available.
}

func (wf *Workflow) onTaskChange(ctx context.Context, task *Task, newStatus TaskStatus) {
	level.Debug(wf.logger).Log("msg", "task state change", "task_id", task.ULID, "new_state", newStatus.State)

	wf.tasksMut.Lock()
	oldState := wf.taskStates[task]
	wf.taskStates[task] = newStatus.State
	wf.tasksMut.Unlock()

	if oldState == newStatus.State {
		return
	}

	if !newStatus.State.Terminal() {
		return
	}

	if wf.admissionControl == nil {
		level.Warn(wf.logger).Log("msg", "admission control was not initialised")
	} else if oldState == TaskStatePending || oldState == TaskStateRunning {
		// Release tokens only if the task was already enqueued and therefore either pending or running.
		defer wf.admissionControl.laneFor(task).Release(1)
	}

	if newStatus.Statistics != nil {
		wf.mergeResults(*newStatus.Statistics)
	}

	// task reached a terminal state. We need to detect if task's immediate
	// children should be canceled. We only look at immediate unterminated
	// children, since canceling them will trigger onTaskChange to process
	// indirect children.
	var tasksToCancel []*Task

	wf.tasksMut.RLock()
	{
	NextChild:
		for _, child := range wf.graph.Children(task) {
			// Ignore children in terminal states.
			if childState := wf.taskStates[child]; childState.Terminal() {
				continue
			}

			// Cancel the child if and only if all of the child's parents (which
			// includes the task that just updated) are in a terminal state.
			for _, parent := range wf.graph.Parents(child) {
				parentState := wf.taskStates[parent]
				if !parentState.Terminal() {
					continue NextChild
				}
			}

			tasksToCancel = append(tasksToCancel, child)
		}
	}
	wf.tasksMut.RUnlock()

	// Runners may re-invoke onTaskChange, so we don't want to hold the mutex
	// when calling this.
	if err := wf.runner.Cancel(ctx, tasksToCancel...); err != nil {
		level.Warn(wf.logger).Log("msg", "failed to cancel tasks", "err", err)
	}
}

func (wf *Workflow) mergeResults(results stats.Result) {
	wf.statsMut.Lock()
	defer wf.statsMut.Unlock()

	wf.stats.Merge(results)
}

type wrappedPipeline struct {
	initOnce sync.Once

	err      error
	errCond  chan struct{}
	failOnce sync.Once

	inner   executor.Pipeline
	onClose func()
}

func (p *wrappedPipeline) Read(ctx context.Context) (arrow.Record, error) {
	p.lazyInit()

	rec, err := p.inner.Read(ctx)

	// Before returning the actual record and error, check to see if an internal
	// error was set.
	//
	// This needs to be checked _after_ doing the inner read, since it's
	// unlikely that our error condition will be set before that call.
	select {
	case <-p.errCond:
		return rec, p.err
	default:
		return rec, err
	}
}

func (p *wrappedPipeline) lazyInit() {
	p.initOnce.Do(func() {
		p.errCond = make(chan struct{})
	})
}

// Close closes the resources of the pipeline.
func (p *wrappedPipeline) Close() {
	p.inner.Close()
	p.onClose()
}

// SetError sets the error for the pipeline. Calls to SetError with a non-nil
// error after the first are ignored.
func (p *wrappedPipeline) SetError(err error) {
	if err == nil {
		return
	}

	p.lazyInit()
	p.failOnce.Do(func() {
		p.err = err
		close(p.errCond)
	})
}
