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
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
)

// Workflow represents a physical plan that has been partitioned into
// parallelizable tasks.
type Workflow struct {
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
func New(logger log.Logger, runner Runner, plan *physical.Plan) (*Workflow, error) {
	graph, err := planWorkflow(plan)
	if err != nil {
		return nil, err
	}

	// Inject a stream for final task results.
	results, err := injectResultsStream(&graph)
	if err != nil {
		return nil, err
	}

	return &Workflow{
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
func injectResultsStream(graph *dag.Graph[*Task]) (*Stream, error) {
	results := &Stream{ULID: ulid.Make()}

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

	g := wf.dispatchTasks(ctx, tasks)
	if err := g.Wait(); err != nil {
		pipeline.Close()
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
	return wrapped, nil
}

// dispatchTasks groups the slice of tasks by their associated "admission lane" (token bucket)
// and dispatches them to the runner.
// Tasks from different admission lanes are dispatched concurrently.
// The caller needs to wait on the returned error group.
func (wf *Workflow) dispatchTasks(ctx context.Context, tasks []*Task) *errgroup.Group {
	// TODO(chaudum): Make the capacity of the admission lanes configurable
	wf.admissionControl = newAdmissionControl(ctx, defaultAdmissionControlOpts)

	g, _ := errgroup.WithContext(ctx)
	for tokenBucket, tasks := range wf.admissionControl.groupByBucket(tasks) {
		g.Go(func() error {
			for _, t := range tasks {
				// Claim t.Cost amount of tokens from the token bucket.
				// This blocks until the claim is fulfilled or returns an error.
				if err := tokenBucket.Claim(t.Cost); err != nil {
					return fmt.Errorf("failed to start task %s: %w", t.ID(), err)
				}
				if err := wf.runner.Start(ctx, wf.onTaskChange, t); err != nil {
					return fmt.Errorf("failed to start task %s: %w", t.ID(), err)
				}
			}
			return nil
		})
	}
	return g
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
	wf.streamsMut.Lock()
	defer wf.streamsMut.Unlock()

	wf.streamStates[stream] = newState

	// TODO(rfratto): Do we need to do anything if a stream changes? Figuring
	// out what to do here will need to wait until the scheduler is available.
}

func (wf *Workflow) onTaskChange(ctx context.Context, task *Task, newStatus TaskStatus) {
	wf.tasksMut.Lock()
	wf.taskStates[task] = newStatus.State
	wf.tasksMut.Unlock()

	if !newStatus.State.Terminal() {
		return
	}

	if wf.admissionControl == nil {
		level.Warn(wf.logger).Log("msg", "admission control was not initialised")
	} else {
		// Return the tokens to the bucket when the function completed
		defer wf.admissionControl.tokenBucketFor(task).Return(task.Cost)
	}

	// TODO(rfratto): If newStatus represents a failure, should we propagate the
	// error to the workflow owner?

	if newStatus.Statistics != nil {
		wf.mergeResults(*newStatus.Statistics)
	}

	// task reached a terminal state. We need to detect if task's immediate
	// children should be canceled. We only look at immediate children, since
	// canceling them will trigger onTaskChange to process indirect children.
	var tasksToCancel []*Task

	wf.tasksMut.RLock()
	{
	NextChild:
		for _, child := range wf.graph.Children(task) {
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
	inner   executor.Pipeline
	onClose func()
}

func (p *wrappedPipeline) Read(ctx context.Context) (arrow.Record, error) {
	return p.inner.Read(ctx)
}

// Close closes the resources of the pipeline.
func (p *wrappedPipeline) Close() {
	p.inner.Close()
	p.onClose()
}
