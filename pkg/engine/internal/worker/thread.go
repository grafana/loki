package worker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
	"github.com/grafana/loki/v3/pkg/storage/bucket"
	utillog "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// RecordSink sends record batches to a destination. Used by drainPipeline so callers can inject mocks in tests.
type recordSink interface {
	Send(ctx context.Context, rec arrow.RecordBatch) error
}

// closableSink closes a sink at task end. Used by closeSinks so tests can inject
// a sink whose close blocks.
type closableSink interface {
	Close(ctx context.Context) error
}

// closeSinks closes each sink so downstream tasks unblock. Every close issues a
// StreamStatusClosed send on the calling (main runJob) goroutine, so its wait is
// added to the slot's comm-blocked total, not counted as compute.
func closeSinks(ctx context.Context, sinks []closableSink, slotPhase *slotPhaseTracker, logger log.Logger) {
	for _, sink := range sinks {
		closeStart := time.Now()
		err := sink.Close(ctx)
		slotPhase.AddComm(time.Since(closeStart))
		if err != nil {
			level.Warn(logger).Log("msg", "failed to close sink", "err", err)
		}
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
	Metastore      metastore.Metastore
	Logger         log.Logger
	StreamFilterer executor.RequestStreamFilterer
	TaskCaches     executor.TaskCacheRegistry
	ScratchStore   scratch.Store
	IndexobjCfg    logsobj.BuilderBaseConfig

	// IndexMergeObserver is optional; nil for query-only workers.
	IndexMergeObserver executor.IndexMergeObserver

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

	root, err := job.Task.Fragment.Root()
	if err != nil {
		slotOutcome = outcomeFailed
		level.Error(logger).Log("msg", "failed to get root node", "err", err)
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
		Bucket:         bucket.NewXCapBucket(t.Bucket),
		Metastore:      t.Metastore,
		StreamFilterer: t.StreamFilterer,
		TaskCaches:     t.TaskCaches,
		ScratchStore:   t.ScratchStore,
		IndexobjCfg:    t.IndexobjCfg,

		IndexMergeObserver: t.IndexMergeObserver,

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
					// have some already-closed sources, such as if they got
					// canceled due to short circuiting. This can be safely
					// ignored.
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

	ctx, capture := xcap.NewCapture(ctx, nil)
	defer capture.End()

	ctx, span := xcap.StartSpan(ctx, tracer, "thread.runJob",
		trace.WithAttributes(attribute.Stringer("task_id", job.Task.ULID)),
	)
	defer span.End()

	pipeline := executor.Run(ctx, cfg, job.Task.Fragment, logger)

	// If the root pipeline can be interested in some specific contributing time range
	// then subscribe to changes.
	// TODO(spiridonov): find a way to subscribe on non-root pipelines.
	notifier, ok := executor.Unwrap(pipeline).(executor.ContributingTimeRangeChangedNotifier)
	if ok {
		notifier.SubscribeToTimeRangeChanges(func(ts time.Time, lessThan bool) {
			// Send a Running task status update with the current time range.
			// This callback fires synchronously from within pipeline.Read, so its
			// wait is already inside the Read time counted toward the slot's
			// comm-blocked total; it is recorded here for per-site attribution
			// only and is not added to the slot total a second time.
			_, err := t.Metrics.timeSend(ctx, job.Scheduler, "task_status_contributing_range_sync", sendModeSync, taskType, wire.TaskStatusMessage{
				ID: job.Task.ULID,
				Status: workflow.TaskStatus{
					State: workflow.TaskStateRunning,
					ContributingTimeRange: workflow.ContributingTimeRange{
						Timestamp: ts,
						LessThan:  lessThan,
					},
				},
			})
			if err != nil {
				level.Warn(logger).Log("msg", "failed to inform scheduler of task status", "err", err)
			}
		})
	}

	runningWait, err := t.Metrics.timeSend(ctx, job.Scheduler, "task_status_running_async", sendModeAsync, taskType, wire.TaskStatusMessage{
		ID:     job.Task.ULID,
		Status: workflow.TaskStatus{State: workflow.TaskStateRunning},
	})
	slotPhase.AddComm(runningWait)
	if err != nil {
		// For now, we'll continue even if the scheduler didn't get the message
		// about the task status.
		level.Warn(logger).Log("msg", "failed to inform scheduler of task status", "err", err)
	}

	// Record the time spent preparing the task before we begin draining its
	// pipeline (planning, pipeline construction, and source binding setup).
	setupDuration := time.Since(startTime)
	span.Record(workerstat.TaskExecutionSetupDuration.Observe(setupDuration.Nanoseconds()))
	t.Metrics.setupSeconds.WithLabelValues(taskType.String()).Observe(setupDuration.Seconds())

	gotrace.Log(ctx, "drain_pipeline", "start")
	_, drainErr := t.drainPipeline(ctx, taskType, slotPhase, pipeline, sinksForJob(job), logger)
	gotrace.Log(ctx, "drain_pipeline", "done")

	var terminalStatus workflow.TaskStatus
	switch {
	case drainErr == nil:
		terminalStatus = workflow.TaskStatus{State: workflow.TaskStateCompleted}
	case errors.Is(drainErr, context.Canceled) && job.interrupted.Load():
		// Context cancellation only counts as a task cancellation if the scheduler
		// specifically requested it. In all other cases we treat it as a failure.
		terminalStatus = workflow.TaskStatus{State: workflow.TaskStateCancelled}
		level.Info(logger).Log("msg", "task cancelled", "err", drainErr)
	default:
		terminalStatus = workflow.TaskStatus{State: workflow.TaskStateFailed, Error: drainErr}
		level.Warn(logger).Log("msg", "task failed", "err", drainErr)
	}

	// Close all sinks regardless of outcome so downstream tasks unblock.
	toClose := make([]closableSink, 0, len(job.Sinks))
	for _, sink := range job.Sinks {
		toClose = append(toClose, sink)
	}
	closeSinks(ctx, toClose, slotPhase, logger)

	// Close before ending capture to ensure all observations are recorded.
	pipeline.Close()
	// Explicitly call End() here (even though we have a defer statement)
	// to finalize the capture before it's included in the TaskStatusMessage.
	span.End()
	capture.End()
	terminalStatus.Capture = capture

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
		"status", terminalStatus.State,
	}

	level.Info(logger).Log(logValues...)
	t.Metrics.taskExecSeconds.WithLabelValues(taskType.String()).Observe(duration.Seconds())

	// Send the terminal status synchronously so the scheduler can update its
	// thread-capacity bookkeeping and merge the Capture before the worker
	// thread requests its next job.
	//
	// We use a detached context so a cancelled job context does not prevent us
	// from delivering the final status (and in particular, the Capture).
	sendCtx := context.WithoutCancel(ctx)
	sendWait, sendErr := t.Metrics.timeSend(sendCtx, job.Scheduler, "task_status_terminal_sync", sendModeSync, taskType, wire.TaskStatusMessage{
		ID:     job.Task.ULID,
		Status: terminalStatus,
	})
	slotPhase.AddComm(sendWait)
	t.Metrics.statusUpdateSeconds.Observe(sendWait.Seconds())
	if terminalStatus.State == workflow.TaskStateCompleted && sendErr == nil {
		slotOutcome = outcomeSuccess
	} else if terminalStatus.State == workflow.TaskStateCancelled {
		slotOutcome = outcomeCanceled
	} else {
		slotOutcome = outcomeFailed
	}
	if sendErr != nil {
		t.Metrics.statusUpdateErrorsTotal.WithLabelValues(statusUpdateErrorClass(sendErr).String()).Inc()
		level.Warn(logger).Log("msg", "failed to inform scheduler of task status", "err", sendErr)
	}
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

// statusUpdateErrorClass maps an error from the status-update send path to a
// bounded label value for the status_update_errors_total counter.
func statusUpdateErrorClass(err error) metrictimer.Outcome {
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

// recordBatchBytes returns the total in-memory size in bytes of all column
// buffers in a RecordBatch.
func recordBatchBytes(rec arrow.RecordBatch) int64 {
	var n int64
	for i := 0; i < int(rec.NumCols()); i++ {
		n += int64(rec.Column(i).Data().SizeInBytes())
	}
	return n
}

func (t *thread) drainPipeline(ctx context.Context, taskType taskType, slotPhase *slotPhaseTracker, pipeline executor.Pipeline, sinks []recordSink, logger log.Logger) (int, error) {
	region := xcap.RegionFromContext(ctx)

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

	var totalRows int
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
		region.Record(workerstat.TaskWireBytes.Observe(recordBatchBytes(rec)))
	}

	return totalRows, nil
}
