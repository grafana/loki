package worker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"runtime/pprof"
	gotrace "runtime/trace"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/thanos-io/objstore"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/metrictimer"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/worker/workerstat"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/scratch"
	utillog "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// RecordSink sends record batches to a destination. Used by drainPipeline so callers can inject mocks in tests.
type recordSink interface {
	Send(ctx context.Context, rec arrow.RecordBatch) error
}

// closeSinks releases the local connections for every task sink.
func closeSinks(job *threadJob) {
	for _, sink := range job.Sinks {
		sink.Close()
	}
}

// sinksForJob returns the job's sinks as a slice for use with drainPipeline.
func sinksForJob(job *threadJob) []recordSink {
	sinks := make([]recordSink, 0, len(job.Sinks))
	for _, s := range job.Sinks {
		sinks = append(sinks, s)
	}
	return sinks
}

type threadState int

const (
	// threadStateIdle reports that a thread is not running.
	threadStateIdle threadState = iota

	// threadStateReady reports that a thread is ready to run a task.
	threadStateReady

	// threadStateBusy reports that a thread is currently running a task.
	threadStateBusy
)

var (
	tracer = otel.Tracer("pkg/engine/internal/worker")
)

func (s threadState) String() string {
	switch s {
	case threadStateIdle:
		return "idle"
	case threadStateReady:
		return "ready"
	case threadStateBusy:
		return "busy"
	default:
		return fmt.Sprintf("threadState(%d)", s)
	}
}

// thread represents a worker thread that executes one task at a time.
type thread struct {
	BatchSize      int64
	PrefetchBytes  int64
	Bucket         objstore.Bucket
	DataBucket     objstore.Bucket
	Metastore      metastore.Metastore
	Logger         log.Logger
	StreamFilterer executor.RequestStreamFilterer
	TaskCaches     executor.TaskCacheRegistry
	ScratchStore   scratch.Store
	IndexobjCfg    logsobj.BuilderBaseConfig

	// IndexMergeObserver is optional; nil for query-only workers.
	IndexMergeObserver executor.IndexMergeObserver

	// LogMergeObserver is optional; nil for query-only workers.
	LogMergeObserver executor.LogMergeObserver

	Metrics    *metrics
	JobManager *jobManager

	stateMut sync.RWMutex
	state    threadState
}

// State returns the current state of the thread.
func (t *thread) State() threadState {
	t.stateMut.RLock()
	defer t.stateMut.RUnlock()
	return t.state
}

// Run starts the thread. Run will request and run tasks in a loop until the
// context is canceled.
func (t *thread) Run(ctx context.Context) error {
	defer t.setState(threadStateIdle)

	for {
		level.Debug(t.Logger).Log("msg", "requesting task")

		t.setState(threadStateReady)

		// slot_ready_wait_seconds is the single "how long a thread slot sat
		// unfilled" signal: time the Recv that fills (or exits) the slot.
		var job *threadJob
		waited, err := metrictimer.Time(func() error {
			var recvErr error
			job, recvErr = t.JobManager.Recv(ctx)
			return recvErr
		})
		readyOutcome := outcomeAssigned
		if err != nil {
			readyOutcome = outcomeShutdown
		}
		t.Metrics.observeSlotReadyWait(readyOutcome, waited)
		if err != nil {
			return nil
		}

		t.setState(threadStateBusy)
		t.runJob(job.Context, job)
	}
}

func (t *thread) setState(state threadState) {
	t.stateMut.Lock()
	defer t.stateMut.Unlock()
	t.state = state
}

func (t *thread) runJob(ctx context.Context, job *threadJob) {
	defer job.Close()

	startTime := time.Now()
	taskType := taskTypeLabel(job.Task)
	slotPhase := newSlotPhaseTracker(t.Metrics, taskType, startTime)
	slotOutcome := outcomeUnknown
	defer func() { slotPhase.Observe(time.Now(), slotOutcome) }()

	ctx, task := gotrace.NewTask(ctx, "thread.runJob")
	defer task.End()

	logger := log.With(t.Logger, "task_id", job.Task.ULID)
	logger = utillog.WithContext(ctx, logger) // Extract trace ID

	ctx, capture := xcap.NewCapture(ctx, nil)
	defer capture.End()

	ctx, span := xcap.StartSpan(ctx, tracer, "thread.runJob",
		trace.WithAttributes(attribute.Stringer("task_id", job.Task.ULID)),
	)
	defer span.End()

	root, err := job.Task.Fragment.Root()
	if err != nil {
		level.Error(logger).Log("msg", "failed to get root node", "err", err)

		closeSinks(job)
		span.End()
		capture.End()
		result := workflow.TaskResult{Outcome: workflow.TaskOutcomeFailed, Error: err, Capture: capture}
		slotOutcome = t.sendTaskResult(ctx, job, taskType, result, slotPhase, logger)
		return
	}
	defer pprof.SetGoroutineLabels(ctx)
	ctx = pprof.WithLabels(ctx, pprof.Labels("node_type", root.Type().String()))
	pprof.SetGoroutineLabels(ctx)

	// Count external sources and sinks for observability.
	// This is useful to know which tasks are scan tasks (no sources)
	var countSources, countCachedSources, countSinks int
	for _, streams := range job.Task.Sources {
		countSources += len(streams)
	}
	for _, streams := range job.Task.Sinks {
		countSinks += len(streams)
	}
	for _, streams := range job.Task.CachedSources {
		countCachedSources += len(streams)
	}

	level.Info(logger).Log(
		"msg", "starting task",
		"plan", physical.PrintAsTree(job.Task.Fragment),
		"external_sources", countSources,
		"cached_sources", countCachedSources,
		"external_sinks", countSinks,
	)

	cfg := executor.Config{
		BatchSize:      t.BatchSize,
		PrefetchBytes:  t.PrefetchBytes,
		Bucket:         t.Bucket,
		DataBucket:     t.DataBucket,
		Metastore:      t.Metastore,
		StreamFilterer: t.StreamFilterer,
		TaskCaches:     t.TaskCaches,
		ScratchStore:   t.ScratchStore,
		IndexobjCfg:    t.IndexobjCfg,

		IndexMergeObserver: t.IndexMergeObserver,
		LogMergeObserver:   t.LogMergeObserver,

		GetExternalInputs: func(fnCtx context.Context, node physical.Node) []executor.Pipeline {
			streams := job.Task.Sources[node]
			nodeCachedSrcs := job.Task.CachedSources[node]

			if len(streams) == 0 && len(nodeCachedSrcs) == 0 {
				return nil
			}

			level.Debug(logger).Log(
				"msg", "getting external inputs for node",
				"node", node.ID(),
				"node_type", node.Type(),
				"external_sources", len(streams),
				"cached_sources", len(nodeCachedSrcs),
			)

			// Create a single nodeSource for all inputs (live streams and/or
			// cached sources). A single pipeline avoids "got N inputs" errors
			// from operators that expect exactly one upstream.
			//
			// Binding a [streamSource] increases the reference count; each
			// source decrements it when done. Spawning drainCachedSources also
			// increments the count once and decrements it on return. When the
			// count reaches zero, the nodeSource closes automatically.
			input := &nodeSource{Metrics: t.Metrics, TaskType: taskType}

			var errs []error

			for _, stream := range streams {
				source, found := job.Sources[stream.ULID]
				if !found {
					level.Warn(logger).Log("msg", "no source found for input stream", "stream_id", stream.ULID)
					continue
				}

				if err := source.Bind(input); err != nil && errors.Is(err, wire.ErrConnClosed) {
					// Depending on load to workers, it's possible for a job to
					// have some already-closed sources, such as if their upstream
					// task was cancelled. This can be safely ignored.
					level.Debug(logger).Log("msg", "skipping closed source", "source", stream.ULID)
				} else if err != nil {
					level.Error(logger).Log("msg", "failed to bind source", "err", err)
					errs = append(errs, fmt.Errorf("binding source %s: %w", stream.ULID, err))
				}
			}

			if len(errs) > 0 {
				// Since we're returning an error pipeline, we need to close
				// input so that writing to any already bound streams doesn't
				// block.
				level.Error(logger).Log("msg", "failed to bind external sources", "errors", errors.Join(errs...))
				input.Close()
				return []executor.Pipeline{errorPipeline(errs)}
			}

			if len(nodeCachedSrcs) > 0 {
				input.Add(1)
				go drainCachedSources(fnCtx, input, nodeCachedSrcs, logger)
			}

			return []executor.Pipeline{executor.NewObservedPipeline("nodeSource", nil, input)}
		},
	}

	ctx = user.InjectOrgID(ctx, job.Task.TenantID)

	pipeline := executor.Run(ctx, cfg, job.Task.Fragment, logger)

	// Record the time spent preparing the task before we begin draining its
	// pipeline (planning, pipeline construction, and source binding setup).
	setupDuration := time.Since(startTime)
	span.Record(workerstat.TaskExecutionSetupDuration.Observe(setupDuration.Nanoseconds()))
	t.Metrics.setupSeconds.WithLabelValues(taskType.String()).Observe(setupDuration.Seconds())

	gotrace.Log(ctx, "drain_pipeline", "start")
	_, drainErr := t.drainPipeline(ctx, taskType, slotPhase, pipeline, sinksForJob(job), logger)
	gotrace.Log(ctx, "drain_pipeline", "done")

	var result workflow.TaskResult
	switch {
	case drainErr == nil:
		result = workflow.TaskResult{Outcome: workflow.TaskOutcomeCompleted}
	case errors.Is(drainErr, context.Canceled) && job.interrupted.Load():
		// Context cancellation only counts as a task cancellation if the scheduler
		// specifically requested it. In all other cases we treat it as a failure.
		result = workflow.TaskResult{Outcome: workflow.TaskOutcomeCancelled}
		level.Info(logger).Log("msg", "task cancelled", "err", drainErr)
	default:
		result = workflow.TaskResult{Outcome: workflow.TaskOutcomeFailed, Error: drainErr}
		level.Warn(logger).Log("msg", "task failed", "err", drainErr)
	}

	// Release local sink connections before finalizing the capture and reporting
	// the task result.
	closeSinks(job)

	// Close before ending capture to ensure all observations are recorded.
	pipeline.Close()
	// Explicitly call End() here (even though we have a defer statement)
	// to finalize the capture before it's included in the TaskResultMessage.
	span.End()
	capture.End()
	result.Capture = capture

	// Build the task's operator tree once from the finalized capture (cold
	// path), then both log it and record per-operator-type cost.
	pipelineNodes := buildPipelineNodes(pipelineRegionsFromCapture(capture))
	logPipelineTrace(logger, pipelineNodes)
	t.Metrics.observeOperatorCost(pipelineNodes)

	// Expose task I/O counts as worker counters by reading the values already
	// accumulated in the per-task capture (the same stats logged in the task
	// summary). Only leaf tasks download from object storage today, so these are
	// zero for non-leaf tasks; recording unconditionally keeps the counters
	// correct if a non-leaf task ever starts downloading.
	pagesDownloaded := xcap.Value[int64](capture, dataobj.StatDatasetPrimaryPagesDownloaded) +
		xcap.Value[int64](capture, dataobj.StatDatasetSecondaryPagesDownloaded)
	t.Metrics.pagesDownloadedTotal.Add(float64(pagesDownloaded))
	t.Metrics.pagesPrunedTotal.Add(float64(xcap.Value[int64](capture, dataobj.StatDatasetPagesPruned)))
	t.Metrics.bytesDownloadedTotal.Add(float64(xcap.Value[int64](capture, dataobj.StatObjectBytesDownloaded)))

	duration := time.Since(startTime)

	logValues := []any{
		"msg", "task completed",
		"duration", duration,
		"status", result.Outcome,
	}

	level.Info(logger).Log(logValues...)
	t.Metrics.taskExecSeconds.WithLabelValues(taskType.String()).Observe(duration.Seconds())

	slotOutcome = t.sendTaskResult(ctx, job, taskType, result, slotPhase, logger)

}

// sendTaskResult sends one terminal result to the scheduler and returns the
// resulting worker-slot outcome.
func (t *thread) sendTaskResult(ctx context.Context, job *threadJob, taskType taskType, result workflow.TaskResult, slotPhase *slotPhaseTracker, logger log.Logger) metrictimer.Outcome {
	// Send synchronously so the scheduler can update task bookkeeping and merge
	// the capture before the worker thread requests its next job. Detach from
	// the job context so cancellation cannot prevent final result delivery.
	sendCtx := context.WithoutCancel(ctx)
	sendWait, sendErr := t.Metrics.timeSend(sendCtx, job.Scheduler, "task_result_terminal_sync", taskType, wire.TaskResultMessage{
		ID:     job.Task.ULID,
		Result: result,
	})
	slotPhase.AddComm(sendWait)
	t.Metrics.taskResultSendSeconds.Observe(sendWait.Seconds())

	outcome := outcomeFailed
	if result.Outcome == workflow.TaskOutcomeCompleted && sendErr == nil {
		outcome = outcomeSuccess
	} else if result.Outcome == workflow.TaskOutcomeCancelled {
		outcome = outcomeCanceled
	}
	if sendErr != nil {
		t.Metrics.taskResultSendErrorsTotal.WithLabelValues(taskResultSendErrorClass(sendErr).String()).Inc()
		level.Warn(logger).Log("msg", "failed to inform scheduler of task result", "err", sendErr)
	}
	return outcome
}

// taskTypeLabel returns the task_type metric label for task: [taskTypeLeaf] if
// the task has no external task sources (it reads directly from storage),
// otherwise [taskTypeNonLeaf]. This mirrors the leaf/non-leaf classification
// used in the per-task summary log.
func taskTypeLabel(task *workflow.Task) taskType {
	if len(task.Sources) == 0 {
		return taskTypeLeaf
	}
	return taskTypeNonLeaf
}

// taskResultSendErrorClass maps an error from the task-result send path to a
// bounded label value for the task_result_send_errors_total counter.
func taskResultSendErrorClass(err error) metrictimer.Outcome {
	switch {
	case err == nil:
		return outcomeNone
	case errors.Is(err, context.Canceled):
		return outcomeCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return outcomeTimeout
	case errors.Is(err, wire.ErrConnClosed):
		return outcomeConnClosed
	}

	var wireErr *wire.Error
	if errors.As(err, &wireErr) {
		switch {
		case wireErr.Code >= 400 && wireErr.Code < 500:
			return outcomeRejected
		case wireErr.Code >= 500:
			return outcomeServerError
		}
	}

	return outcomeOther
}

func commOutcome(err error) metrictimer.Outcome {
	switch {
	case err == nil:
		return outcomeSuccess
	case errors.Is(err, executor.EOF):
		return outcomeSuccess
	case errors.Is(err, context.Canceled):
		return outcomeCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return outcomeTimeout
	case errors.Is(err, wire.ErrConnClosed):
		return outcomeConnClosed
	}

	var wireErr *wire.Error
	if errors.As(err, &wireErr) {
		if wireErr.Code == http.StatusTooManyRequests {
			return outcomeNack429
		}
		return outcomeNack
	}
	return outcomeError
}

func (t *thread) drainPipeline(ctx context.Context, taskType taskType, slotPhase *slotPhaseTracker, pipeline executor.Pipeline, sinks []recordSink, logger log.Logger) (totalRows int, retErr error) {
	region := xcap.RegionFromContext(ctx)

	// Draining runs on the worker thread's goroutine, which is not covered by any
	// request-level recovery middleware. An unrecovered panic here (e.g. from a
	// pipeline operator) would crash the entire worker process and abort every
	// in-flight task. Convert it into a task error so only this task fails.
	defer func() {
		if p := recover(); p != nil {
			stack := make([]byte, 8*1024)
			stack = stack[:runtime.Stack(stack, false)]
			level.Error(logger).Log("msg", "recovered from panic while draining pipeline", "panic", p, "stack", string(stack))
			retErr = fmt.Errorf("panic while draining pipeline: %v", p)
		}
	}()

	var (
		openDuration  time.Duration
		totalReadTime time.Duration
		totalSendTime time.Duration
	)

	// Record on every exit, including failures, so a task that errors still
	// reports where its time went; the region observations feed the summary log.
	defer func() {
		t.Metrics.taskOpenSeconds.WithLabelValues(taskType.String()).Observe(openDuration.Seconds())
		t.Metrics.taskReadSeconds.WithLabelValues(taskType.String()).Observe(totalReadTime.Seconds())
		t.Metrics.taskSendSeconds.WithLabelValues(taskType.String()).Observe(totalSendTime.Seconds())

		region.Record(workerstat.TaskExecutionOpenDuration.Observe(openDuration.Nanoseconds()))
		region.Record(workerstat.TaskExecutionReadDuration.Observe(totalReadTime.Nanoseconds()))
		region.Record(workerstat.TaskExecutionSendDuration.Observe(totalSendTime.Nanoseconds()))
	}()

	startOpen := time.Now()
	openErr := pipeline.Open(ctx)
	openDuration = time.Since(startOpen)
	if openErr != nil {
		return 0, openErr
	}

	for {
		startRead := time.Now()
		rec, err := pipeline.Read(ctx)
		readDuration := time.Since(startRead)
		// The main goroutine is blocked reading its input; count it toward the
		// slot's comm-blocked time.
		slotPhase.AddComm(readDuration)
		// Count every pass toward the task total; the per-pass histogram skips
		// error passes to avoid skewing pass latencies.
		totalReadTime += readDuration
		if err == nil || errors.Is(err, executor.EOF) {
			t.Metrics.passReadSeconds.Observe(readDuration.Seconds())
		}
		if errors.Is(err, executor.EOF) {
			break
		} else if err != nil {
			return totalRows, err
		}

		region.Record(workerstat.TaskDrainRecordsReceived.Observe(1))
		totalRows += int(rec.NumRows())

		// Don't bother writing empty records to our peers.
		if rec.NumRows() == 0 {
			continue
		}

		startSend := time.Now()
		for _, sink := range sinks {
			// If a sink doesn't accept the result, we'll continue
			// best-effort processing our task. It's possible that one of
			// the receiving sinks got canceled, and other sinks may still
			// need data.
			if err := sink.Send(ctx, rec); err != nil {
				level.Warn(logger).Log("msg", "failed to send result", "err", err)
			}
		}
		sendDuration := time.Since(startSend)
		// The main goroutine is blocked sending its output; count it toward the
		// slot's comm-blocked time.
		slotPhase.AddComm(sendDuration)
		t.Metrics.passSendSeconds.Observe(sendDuration.Seconds())
		totalSendTime += sendDuration

		region.Record(workerstat.TaskRecordsSent.Observe(1))
		region.Record(workerstat.TaskRowsSent.Observe(rec.NumRows()))
	}

	return totalRows, nil
}
