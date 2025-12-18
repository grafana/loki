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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/xcap"
)

var shortCircuitsTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "loki_engine_v2_task_short_circuits_total",
	Help: "Total number of tasks preemptively canceled by short circuiting.",
})

// Options configures a [Workflow].
type Options struct {
	// MaxRunningScanTasks specifies the maximum number of scan tasks that may
	// run concurrently within a single workflow. 0 means no limit.
	MaxRunningScanTasks int

	// MaxRunningOtherTasks specifies the maximum number of non-scan tasks that
	// may run concurrently within a single workflow. 0 means no limit.
	MaxRunningOtherTasks int

	// DebugTasks toggles debug messages for a task. This is very verbose and
	// should only be enabled for debugging purposes.
	//
	// Regardless of the value of DebugTasks, workers still log when
	// they start and finish assigned tasks.
	DebugTasks bool

	// DebugStreams toggles debug messages for data streams. This is very
	// verbose and should only be enabled for debugging purposes.
	DebugStreams bool
}

// Workflow represents a physical plan that has been partitioned into
// parallelizable tasks.
type Workflow struct {
	opts            Options
	logger          log.Logger
	runner          Runner
	graph           dag.Graph[*Task]
	resultsStream   *Stream
	resultsPipeline *streamPipe
	manifest        *Manifest

	statsMut sync.Mutex
	stats    stats.Result

	captureMut sync.Mutex
	capture    *xcap.Capture

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

	wf := &Workflow{
		opts:            opts,
		logger:          logger,
		runner:          runner,
		graph:           graph,
		resultsStream:   results,
		resultsPipeline: newStreamPipe(),

		taskStates:   make(map[*Task]TaskState),
		streamStates: make(map[*Stream]StreamState),
	}
	if err := wf.init(context.Background()); err != nil {
		wf.Close()
		return nil, err
	}
	return wf, nil
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

// init initializes the workflow.
func (wf *Workflow) init(ctx context.Context) error {
	wf.manifest = &Manifest{
		Streams: wf.allStreams(),
		Tasks:   wf.allTasks(),

		StreamEventHandler: wf.onStreamChange,
		TaskEventHandler:   wf.onTaskChange,
	}
	if err := wf.runner.RegisterManifest(ctx, wf.manifest); err != nil {
		return err
	}
	return wf.runner.Listen(ctx, wf.resultsPipeline, wf.resultsStream)
}

// Close releases resources associated with the workflow.
func (wf *Workflow) Close() {
	if err := wf.runner.UnregisterManifest(context.Background(), wf.manifest); err != nil {
		level.Warn(wf.logger).Log("msg", "failed to unregister workflow manifest", "err", err)
	}
}

// Run executes the workflow, returning a pipeline to read results from. The
// provided context is used for the lifetime of the workflow execution.
//
// The returned pipeline must be closed when the workflow is complete to release
// resources.
func (wf *Workflow) Run(ctx context.Context) (pipeline executor.Pipeline, err error) {
	wf.capture = xcap.CaptureFromContext(ctx)

	wrapped := &wrappedPipeline{
		inner: wf.resultsPipeline,
		onClose: func() {
			// Merge final stats results back into the caller.
			wf.statsMut.Lock()
			defer wf.statsMut.Unlock()
			stats.JoinResults(ctx, wf.stats)
		},
	}

	// Start dispatching in background goroutine
	go func() {
		err := wf.dispatchTasks(ctx, wf.manifest.Tasks)
		if err != nil {
			wf.resultsPipeline.SetError(err)
			wrapped.Close()
		}
	}()

	return wrapped, nil
}

// dispatchTasks groups the slice of tasks by their associated "admission lane" (token bucket)
// and dispatches them to the runner.
// Tasks from different admission lanes are dispatched concurrently.
// The caller needs to wait on the returned error group.
func (wf *Workflow) dispatchTasks(ctx context.Context, tasks []*Task) error {
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

		var offset, batchSize int64
		total := int64(len(tasks))

		for ; offset < total; offset += batchSize {
			batchSize = int64(1)
			if err := lane.Acquire(ctx, batchSize); err != nil {
				return fmt.Errorf("failed to acquire tokens from admission lane %s: %w", taskType, err)
			}
			if err := wf.runner.Start(ctx, tasks[offset:offset+batchSize]...); err != nil {
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
	if wf.opts.DebugStreams {
		level.Debug(wf.logger).Log("msg", "stream state change", "stream_id", stream.ULID, "new_state", newState)
	}

	wf.streamsMut.Lock()
	defer wf.streamsMut.Unlock()

	wf.streamStates[stream] = newState

	if newState == StreamStateClosed && stream.ULID == wf.resultsStream.ULID {
		// Close the results pipeline once the results stream has closed.
		wf.resultsPipeline.Close()
	}
}

func (wf *Workflow) onTaskChange(ctx context.Context, task *Task, newStatus TaskStatus) {
	if wf.opts.DebugTasks {
		level.Debug(wf.logger).Log("msg", "task state change", "task_id", task.ULID, "new_state", newStatus.State)
	}

	wf.tasksMut.Lock()
	oldState := wf.taskStates[task]
	wf.taskStates[task] = newStatus.State
	wf.tasksMut.Unlock()

	if newStatus.State.Terminal() {
		wf.handleTerminalStateChange(ctx, task, oldState, newStatus)
	} else {
		wf.handleNonTerminalStateChange(ctx, task, newStatus)
	}
}

func (wf *Workflow) handleTerminalStateChange(ctx context.Context, task *Task, oldState TaskState, newStatus TaskStatus) {
	// State has not changed
	if oldState == newStatus.State {
		return
	}

	if newStatus.State == TaskStateFailed {
		// Use the first failure from a task as the failure for the entire
		// workflow.
		wf.resultsPipeline.SetError(newStatus.Error)
	}

	if wf.admissionControl == nil {
		level.Warn(wf.logger).Log("msg", "admission control was not initialized")
	} else if oldState == TaskStatePending || oldState == TaskStateRunning {
		// Release tokens only if the task was already enqueued and therefore either pending or running.
		defer wf.admissionControl.laneFor(task).Release(1)
	}

	if newStatus.Capture != nil {
		wf.mergeCapture(newStatus.Capture)
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

	wf.cancelTasks(ctx, tasksToCancel)
}

func (wf *Workflow) handleNonTerminalStateChange(ctx context.Context, task *Task, newStatus TaskStatus) {
	// If the task is running, but its contributing time range has been changed
	if newStatus.State == TaskStateRunning && !newStatus.ContributingTimeRange.Timestamp.IsZero() {
		// We need to detect if task's immediate children should be canceled because they can no longer contribute
		// to the state of the running task. We only look at immediate unterminated
		// children, since canceling them will trigger onTaskChange to process indirect children.
		var tasksToCancel []*Task

		ts := newStatus.ContributingTimeRange.Timestamp
		lessThan := newStatus.ContributingTimeRange.LessThan

		wf.tasksMut.RLock()
		{
			for _, child := range wf.graph.Children(task) {
				// Ignore children in terminal states.
				if childState := wf.taskStates[child]; childState.Terminal() {
					continue
				}

				// Ignore if time ranges intersect, so they can contribute
				if lessThan && child.MaxTimeRange.Start.Before(ts) ||
					!lessThan && child.MaxTimeRange.End.After(ts) {
					continue
				}

				// TODO(spiridonov): We do not check parents here right now, there is only 1 parent now,
				// but in general a task can be canceled only if all its parents are in terminal states OR
				// have non-inersecting contributing time range.
				tasksToCancel = append(tasksToCancel, child)
				shortCircuitsTotal.Inc()
			}
		}
		wf.tasksMut.RUnlock()

		wf.cancelTasks(ctx, tasksToCancel)
	}
}

func (wf *Workflow) cancelTasks(ctx context.Context, tasks []*Task) {
	// Runners may re-invoke onTaskChange, so we don't want to hold the mutex
	// when calling this.
	if err := wf.runner.Cancel(ctx, tasks...); err != nil {
		level.Warn(wf.logger).Log("msg", "failed to cancel tasks", "err", err)
	}
}

func (wf *Workflow) mergeCapture(capture *xcap.Capture) {
	wf.captureMut.Lock()
	defer wf.captureMut.Unlock()

	if wf.capture == nil || capture == nil {
		return
	}

	// Merge all regions from the task's capture into the workflow's capture.
	for _, region := range capture.Regions() {
		wf.capture.AddRegion(region)
	}
}

func (wf *Workflow) mergeResults(results stats.Result) {
	wf.statsMut.Lock()
	defer wf.statsMut.Unlock()

	wf.stats.Merge(results)
}

type wrappedPipeline struct {
	initOnce sync.Once

	inner   executor.Pipeline
	onClose func()
}

func (p *wrappedPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	return p.inner.Read(ctx)
}

// Close closes the resources of the pipeline.
func (p *wrappedPipeline) Close() {
	p.inner.Close()
	p.onClose()
}
