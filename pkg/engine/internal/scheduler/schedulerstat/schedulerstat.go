// Package schedulerstat declares scheduler-recorded per-task observation
// statistics.
package schedulerstat

import "github.com/grafana/loki/v3/pkg/xcap"

// These stats live in their own package rather than in
// [pkg/engine/internal/scheduler] to avoid a cyclic dependency between workflow
// and the scheduler.

var (
	// TaskStagingDuration is the time (in nanoseconds) a task spent staged
	// between scheduler registration and being enqueued for assignment. Not
	// recorded for tasks that never reached the queue.
	TaskStagingDuration = xcap.NewStatisticInt64("scheduler.task.staging.duration", xcap.AggregationTypeSum)

	// TaskQueueDuration is the time (in nanoseconds) a task spent enqueued before
	// either being assigned to a worker or reaching a terminal state without
	// ever being assigned.
	TaskQueueDuration = xcap.NewStatisticInt64("scheduler.task.queue.duration", xcap.AggregationTypeSum)

	// TaskExecutionDuration is the time (in nanoseconds) a task spent assigned to
	// a worker before reaching a terminal state. Not recorded for tasks that
	// never reached a worker.
	TaskExecutionDuration = xcap.NewStatisticInt64("scheduler.task.execution.duration", xcap.AggregationTypeSum)

	// TaskTotalDuration is the total time (in nanoseconds) from scheduler
	// registration to terminal state, regardless of which phases the task
	// passed through.
	//
	// The other scheduler.task.*.duration stats partition this duration into
	// individual phases; the difference between TaskTotalDuration and the
	// sum of the partitions is a residual that indicates an unaccounted-for
	// phase.
	TaskTotalDuration = xcap.NewStatisticInt64("scheduler.task.duration", xcap.AggregationTypeSum)
)
