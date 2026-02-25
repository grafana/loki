package worker

import (
	"context"
	"errors"
	"fmt"
	"runtime/pprof"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/thanos-io/objstore"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/arrowagg"
	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/storage/bucket"
	utillog "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// RecordSink sends record batches to a destination. Used by drainPipeline so callers can inject mocks in tests.
type recordSink interface {
	Send(ctx context.Context, rec arrow.RecordBatch) error
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
		job, err := t.JobManager.Recv(ctx)
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

	logger := log.With(t.Logger, "task_id", job.Task.ULID)
	logger = utillog.WithContext(ctx, logger) // Extract trace ID

	root, err := job.Task.Fragment.Root()
	if err != nil {
		level.Error(logger).Log("msg", "failed to get root node", "err", err)
		return
	}
	defer pprof.SetGoroutineLabels(ctx)
	ctx = pprof.WithLabels(ctx, pprof.Labels("node_type", root.Type().String()))
	pprof.SetGoroutineLabels(ctx)

	// Count external sources and sinks for observability.
	// This is useful to know which tasks are scan tasks (no sources)
	var countSources, countSinks int
	for _, streams := range job.Task.Sources {
		countSources += len(streams)
	}
	for _, streams := range job.Task.Sinks {
		countSinks += len(streams)
	}

	startTime := time.Now()
	level.Info(logger).Log(
		"msg", "starting task",
		"plan", physical.PrintAsTree(job.Task.Fragment),
		"external_sources", countSources,
		"external_sinks", countSinks,
	)

	cfg := executor.Config{
		BatchSize:      t.BatchSize,
		PrefetchBytes:  t.PrefetchBytes,
		Bucket:         bucket.NewXCapBucket(t.Bucket),
		Metastore:      t.Metastore,
		StreamFilterer: t.StreamFilterer,

		GetExternalInputs: func(_ context.Context, node physical.Node) []executor.Pipeline {
			streams := job.Task.Sources[node]
			if len(streams) == 0 {
				return nil
			}

			// Create a single nodeSource for all streams. Having streams
			// forward to a node source allows for backpressure to be applied
			// based on the number of nodeSource (typically 1) rather than the
			// number of source streams (unbounded).
			//
			// Binding the [streamSource] to the input in the loop below
			// increases the reference count. As sources are closed, the
			// reference count decreases. Once the reference count reaches 0,
			// the nodeSource is closed, and reads return EOF.
			input := new(nodeSource)

			var errs []error

			for _, stream := range streams {
				source, found := job.Sources[stream.ULID]
				if !found {
					level.Warn(logger).Log("msg", "no source found for input stream", "stream_id", stream.ULID)
					continue
				} else if err := source.Bind(input); err != nil {
					level.Error(logger).Log("msg", "failed to bind source", "err", err)
					errs = append(errs, fmt.Errorf("binding source %s: %w", stream.ULID, err))
					continue
				}
			}

			if len(errs) > 0 {
				// Since we're returning an error pipeline, we need to close
				// input so that writing to any already bound streams doesn't
				// block.
				input.Close()
				return []executor.Pipeline{errorPipeline(errs)}
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

	span.Record(xcap.TaskExternalSourcesCount.Observe(int64(countSources)))
	span.Record(xcap.TaskExternalSinksCount.Observe(int64(countSinks)))

	pipeline := executor.Run(ctx, cfg, job.Task.Fragment, logger)

	// If the root pipeline can be interested in some specific contributing time range
	// then subscribe to changes.
	// TODO(spiridonov): find a way to subscribe on non-root pipelines.
	notifier, ok := executor.Unwrap(pipeline).(executor.ContributingTimeRangeChangedNotifier)
	if ok {
		notifier.SubscribeToTimeRangeChanges(func(ts time.Time, lessThan bool) {
			// Send a Running task status update with the current time range
			err := job.Scheduler.SendMessage(ctx, wire.TaskStatusMessage{
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

	err = job.Scheduler.SendMessageAsync(ctx, wire.TaskStatusMessage{
		ID:     job.Task.ULID,
		Status: workflow.TaskStatus{State: workflow.TaskStateRunning},
	})
	if err != nil {
		// For now, we'll continue even if the scheduler didn't get the message
		// about the task status.
		level.Warn(logger).Log("msg", "failed to inform scheduler of task status", "err", err)
	}

	_, err = t.drainPipeline(ctx, pipeline, sinksForJob(job), t.BatchSize, logger)
	if err != nil {
		level.Warn(logger).Log("msg", "task failed", "err", err)
		_ = job.Scheduler.SendMessageAsync(ctx, wire.TaskStatusMessage{
			ID: job.Task.ULID,
			Status: workflow.TaskStatus{
				State: workflow.TaskStateFailed,
				Error: err,
			},
		})

		pipeline.Close()
		return
	}

	// Finally, close all sinks.
	for _, sink := range job.Sinks {
		err := sink.Close(ctx)
		if err != nil {
			level.Warn(logger).Log("msg", "failed to close sink", "err", err)
		}
	}

	// Close before ending capture to ensure all observations are recorded.
	pipeline.Close()
	// Explicitly call End() here (even though we have a defer statement)
	// to finalize the capture before it's included in the TaskStatusMessage.
	span.End()
	capture.End()

	duration := time.Since(startTime)

	logValues := []any{
		"msg", "task completed",
		"duration", duration,
	}
	logValues = append(logValues, xcap.SummaryLogValues(capture)...)

	level.Info(logger).Log(logValues...)
	t.Metrics.taskExecSeconds.Observe(duration.Seconds())

	// Wait for the scheduler to confirm the task has completed before
	// requesting a new one. This allows the scheduler to update its bookkeeping
	// for how many threads have capacity for requesting tasks.
	err = job.Scheduler.SendMessage(ctx, wire.TaskStatusMessage{
		ID:     job.Task.ULID,
		Status: workflow.TaskStatus{State: workflow.TaskStateCompleted, Capture: capture},
	})
	if err != nil {
		level.Warn(logger).Log("msg", "failed to inform scheduler of task status", "err", err)
	}
}

func (t *thread) drainPipeline(ctx context.Context, pipeline executor.Pipeline, sinks []recordSink, batchSizeRecords int64, logger log.Logger) (int, error) {
	region := xcap.RegionFromContext(ctx)

	if err := pipeline.Open(ctx); err != nil {
		return 0, err
	}

	var totalRows int
	var currentBatchRecordCount int64
	batchAggregator := arrowagg.NewRecords(memory.DefaultAllocator)

	// When batchSizeRecords <= 0, no batching: each record is sent alone.
	flush := func(toSend arrow.RecordBatch) {
		startSend := time.Now()
		for _, sink := range sinks {
			// If a sink doesn't accept the result, we'll continue
			// best-effort processing our task. It's possible that one of
			// the receiving sinks got canceled, and other sinks may still
			// need data.
			err := sink.Send(ctx, toSend)
			if err != nil {
				level.Warn(logger).Log("msg", "failed to send result", "err", err)
				continue
			}
		}
		region.Record(xcap.TaskRecordsSent.Observe(1))
		region.Record(xcap.TaskRowsSent.Observe(toSend.NumRows()))
		region.Record(xcap.TaskSendDuration.Observe(time.Since(startSend).Seconds()))
	}

	// flushBatch flushes the batch aggregator's accumulated records (aggregate with schema reconciliation and send), then resets it.
	flushBatch := func() {
		if currentBatchRecordCount == 0 {
			return
		}

		combined, err := batchAggregator.Aggregate()

		// Regardless of errors, reset the aggregator to prepare for the next batch.
		batchAggregator.Reset()
		currentBatchRecordCount = 0

		if err != nil {
			level.Warn(logger).Log("msg", "failed to aggregate record batch", "err", err)
			return
		}

		region.Record(xcap.TaskDrainBatchesProduced.Observe(1))
		flush(combined)
	}

	for {
		rec, err := pipeline.Read(ctx)
		if err != nil && errors.Is(err, executor.EOF) {
			break
		} else if err != nil {
			return totalRows, err
		}

		region.Record(xcap.TaskDrainRecordsReceived.Observe(1))
		totalRows += int(rec.NumRows())

		// Don't bother writing empty records to our peers.
		if rec.NumRows() == 0 {
			continue
		}

		// If batching is disabled, send this record alone.
		if batchSizeRecords <= 0 {
			flush(rec)
			continue
		}

		// If adding this record would exceed the batch size, flush before the next iteration.
		if currentBatchRecordCount+rec.NumRows() > batchSizeRecords {
			flushBatch()
		}

		// Add the record to the batch aggregator.
		batchAggregator.Append(rec)
		currentBatchRecordCount += rec.NumRows()
	}

	// Flush any remaining batch.
	flushBatch()

	return totalRows, nil
}
