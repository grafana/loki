// Package schedulerstat declares scheduler-recorded per-task observation
// statistics.
package schedulerstat

import "github.com/grafana/loki/v3/pkg/xcap"

// These stats live in their own package rather than in
// [pkg/engine/internal/scheduler] to avoid a cyclic dependency between workflow
// and the scheduler.

var (
	// TaskStagingDuration is the time (in nanoseconds) a task spent staged
	// between scheduler registration and being enqueued for assignment. For
	// a task that was never enqueued, this stat captures the entire duration
	// from registration to terminal state.
	TaskStagingDuration = xcap.NewStatisticInt64("scheduler.task.staging.duration", xcap.AggregationTypeSum)

	// TaskQueueDuration is the time (in nanoseconds) a task spent enqueued before
	// either being assigned to a worker or reaching a terminal state without
	// ever being assigned.
	TaskQueueDuration = xcap.NewStatisticInt64("scheduler.task.queue.duration", xcap.AggregationTypeSum)

	// TaskExecutionDuration is the time (in nanoseconds) a task spent assigned to
	// a worker before reaching a terminal state. Not recorded for tasks that
	// never reached a worker.
	TaskExecutionDuration = xcap.NewStatisticInt64("scheduler.task.execution.duration", xcap.AggregationTypeSum)

	// TaskAssignmentRetries is the number of times a task was requeued after a
	// failed assignment attempt before it reached a terminal state.
	TaskAssignmentRetries = xcap.NewStatisticInt64("scheduler.task.assignment.retries", xcap.AggregationTypeSum)

	// TaskTotalDuration is the total time (in nanoseconds) from scheduler
	// registration to terminal state, regardless of which phases the task
	// passed through.
	//
	// The other scheduler.task.*.duration stats partition this duration into
	// individual phases; the difference between TaskTotalDuration and the
	// sum of the partitions is a residual that indicates an unaccounted-for
	// phase.
	TaskTotalDuration = xcap.NewStatisticInt64("scheduler.task.duration", xcap.AggregationTypeSum)

	// TaskFinishTime is the wall-clock time (Unix nanoseconds) at which the
	// scheduler observed the task reach a terminal state. It is recorded once
	// per task and rides the task's capture back to the workflow, which uses it
	// to approximate the query's critical path.
	//
	// AggregationTypeMax keeps the latest observed finish time when captures are
	// merged; the per-task read in the workflow is unaffected because each task
	// records this stat exactly once.
	TaskFinishTime = xcap.NewStatisticInt64("scheduler.task.finish.time", xcap.AggregationTypeMax)
)
