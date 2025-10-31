package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/oklog/ulid/v2"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
)

// threadJob is an individual task to run.
type threadJob struct {
	Context context.Context
	Cancel  context.CancelFunc

	Scheduler *wire.Peer     // Scheduler which owns the task.
	Task      *workflow.Task // Task to execute.

	Sources map[ulid.ULID]*streamSource // Sources to read task data from.
	Sinks   map[ulid.ULID]*streamSink   // Sinks to write task data to.

	Close func() // Close function to clean up resources for the job.
}

// thread represents a worker thread that executes one task at a time.
type thread struct {
	BatchSize int64
	Bucket    objstore.Bucket
	Logger    log.Logger

	Ready chan<- readyRequest
}

// Run starts the thread. Run will request and run tasks in a loop until the
// context is canceled.
func (t *thread) Run(ctx context.Context) error {
NextTask:
	for {
		level.Debug(t.Logger).Log("msg", "requesting task")

		// Channel used for task assignment. We create a new buffered channel
		// for each iteration to ensure that writes to respCh never block.
		respCh := make(chan readyResponse, 1)

		// When we create the request, we pass the thread's context. This
		// ensures that the context of tasks written to respCh are bound to the
		// lifetime of the thread, but can also be canceled by the scheduler.
		req := readyRequest{
			Context:  ctx,
			Response: respCh,
		}

		// Send our request.
		select {
		case <-ctx.Done():
			return nil
		case t.Ready <- req:
		}

		// Wait for a task assignment.
		select {
		case <-ctx.Done():
			// TODO(rfratto): This will silently drop tasks written to respCh.
			// But since Run only exits when the worker is exiting, this should
			// be handled gracefully by the scheduler (it will detect the
			// dropped connection and fail the assigned tasks).
			//
			// If, in the future, we dynamically change the number of threads,
			// we'll want a mechanism to gracefully handle this so the writer to
			// respCh knows that the task was dropped.
			return nil

		case resp := <-respCh:
			if resp.Error != nil {
				level.Warn(t.Logger).Log("msg", "task assignment failed, will request a new task", "err", resp.Error)
				continue NextTask
			} else if resp.Job == nil {
				// This may hit if the connection to the scheduler closed but
				// didn't result in an error. This shouldn't happen, but it's
				// better to handle it than to let runTask panic from nil
				// pointers.
				level.Warn(t.Logger).Log("msg", "missing task assignment, will request a new task")
				continue NextTask
			}

			t.runJob(resp.Job.Context, resp.Job)
		}
	}
}

func (t *thread) runJob(ctx context.Context, job *threadJob) {
	defer job.Close()

	logger := log.With(t.Logger, "task_id", job.Task.ULID)

	startTime := time.Now()
	level.Info(logger).Log("msg", "starting task")

	cfg := executor.Config{
		BatchSize: t.BatchSize,
		Bucket:    t.Bucket,

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
			// increases the reference count. As sources as closed, the
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

	statsCtx, ctx := stats.NewContext(ctx)
	ctx = user.InjectOrgID(ctx, job.Task.TenantID)

	pipeline := executor.Run(ctx, cfg, job.Task.Fragment, logger)
	defer pipeline.Close()

	err := job.Scheduler.SendMessageAsync(ctx, wire.TaskStatusMessage{
		ID:     job.Task.ULID,
		Status: workflow.TaskStatus{State: workflow.TaskStateRunning},
	})
	if err != nil {
		// For now, we'll continue even if the scheduler didn't get the message
		// about the task status.
		level.Warn(logger).Log("msg", "failed to inform scheduler of task status", "err", err)
	}

	var totalRows int

	for {
		rec, err := pipeline.Read(ctx)
		if err != nil && errors.Is(err, executor.EOF) {
			break
		} else if err != nil {
			level.Warn(logger).Log("msg", "task failed", "err", err)
			_ = job.Scheduler.SendMessageAsync(ctx, wire.TaskStatusMessage{
				ID: job.Task.ULID,
				Status: workflow.TaskStatus{
					State: workflow.TaskStateFailed,
					Error: err,
				},
			})
			return
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

	// Finally, close all sinks.
	for _, sink := range job.Sinks {
		err := sink.Close(ctx)
		if err != nil {
			level.Warn(logger).Log("msg", "failed to close sink", "err", err)
		}
	}

	// TODO(rfratto): We should find a way to expose queue time here.
	result := statsCtx.Result(time.Since(startTime), 0, totalRows)
	level.Info(logger).Log("msg", "task completed", "duration", time.Since(startTime))

	err = job.Scheduler.SendMessageAsync(ctx, wire.TaskStatusMessage{
		ID:     job.Task.ULID,
		Status: workflow.TaskStatus{State: workflow.TaskStateCompleted, Statistics: &result},
	})
	if err != nil {
		level.Warn(logger).Log("msg", "failed to inform scheduler of task status", "err", err)
	}
}
