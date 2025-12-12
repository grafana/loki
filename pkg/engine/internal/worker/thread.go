package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/storage/bucket"
	utillog "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/xcap"
)

type threadState int

const (
	// threadStateIdle reports that a thread is not running.
	threadStateIdle threadState = iota

	// threadStateReady reports that a thread is ready to run a task.
	threadStateReady

	// threadStateBusy reports that a thread is currently running a task.
	threadStateBusy
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
	BatchSize int64
	Bucket    objstore.Bucket
	Logger    log.Logger

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

	startTime := time.Now()
	level.Info(logger).Log("msg", "starting task")

	cfg := executor.Config{
		BatchSize: t.BatchSize,
		Bucket:    bucket.NewXCapBucket(t.Bucket),

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

			return []executor.Pipeline{input}
		},
	}

	ctx = user.InjectOrgID(ctx, job.Task.TenantID)

	ctx, capture := xcap.NewCapture(ctx, nil)
	defer capture.End()

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

	err := job.Scheduler.SendMessageAsync(ctx, wire.TaskStatusMessage{
		ID:     job.Task.ULID,
		Status: workflow.TaskStatus{State: workflow.TaskStateRunning},
	})
	if err != nil {
		// For now, we'll continue even if the scheduler didn't get the message
		// about the task status.
		level.Warn(logger).Log("msg", "failed to inform scheduler of task status", "err", err)
	}

	_, err = t.drainPipeline(ctx, pipeline, job, logger)
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
	capture.End()

	duration := time.Since(startTime)
	level.Info(logger).Log("msg", "task completed", "duration", duration)
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

func (t *thread) drainPipeline(ctx context.Context, pipeline executor.Pipeline, job *threadJob, logger log.Logger) (int, error) {
	var totalRows int
	for {
		rec, err := pipeline.Read(ctx)
		if err != nil && errors.Is(err, executor.EOF) {
			break
		} else if err != nil {
			return totalRows, err
		}

		totalRows += int(rec.NumRows())

		// Don't bother writing empty records to our peers.
		if rec.NumRows() == 0 {
			continue
		}

		for _, sink := range job.Sinks {
			err := sink.Send(ctx, rec)
			if err != nil {
				// If a sink doesn't accept the result, we'll continue
				// best-effort processing our task. It's possible that one of
				// the receiving sinks got canceled, and other sinks may still
				// need data.
				level.Warn(logger).Log("msg", "failed to send result", "err", err)
				continue
			}
		}
	}

	return totalRows, nil
}
