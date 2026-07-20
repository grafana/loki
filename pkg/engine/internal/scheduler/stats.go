package scheduler

import (
	"github.com/grafana/loki/v3/pkg/xcap"
	"github.com/grafana/loki/v3/pkg/xcap/statid"
)

var (
	// StatPlannedTasks records the number of tasks that were planned for
	// execution.
	StatPlannedTasks = xcap.NewStatisticInt64(statid.SchedulerPlannedTasks, "scheduler.tasks.planned", xcap.AggregationTypeSum)

	// StatQueuedTasks records the number of tasks that were queued for
	// execution.
	StatQueuedTasks = xcap.NewStatisticInt64(statid.SchedulerQueuedTasks, "scheduler.tasks.queued", xcap.AggregationTypeSum)

	// StatAssignedTasks records the number of tasks that were assigned to lanes.
	StatAssignedTasks = xcap.NewStatisticInt64(statid.SchedulerAssignedTasks, "scheduler.tasks.assigned", xcap.AggregationTypeSum)

	// StatExecutedTasks records the number of tasks that were executed.
	StatExecutedTasks = xcap.NewStatisticInt64(statid.SchedulerExecutedTasks, "scheduler.tasks.executed", xcap.AggregationTypeSum)

	// StatCanceledTasks records the number of pending (not yet queued) tasks
	// that were canceled.
	StatCanceledPendingTasks = xcap.NewStatisticInt64(statid.SchedulerCanceledPendingTasks, "scheduler.tasks.canceled.pending", xcap.AggregationTypeSum)

	// StatCanceledQueuedTasks records the number of queued tasks that were
	// canceled.
	StatCanceledQueuedTasks = xcap.NewStatisticInt64(statid.SchedulerCanceledQueuedTasks, "scheduler.tasks.canceled.queued", xcap.AggregationTypeSum)

	// StatCanceledAssignedTasks records the number of assigned (in-progress)
	// tasks that were canceled.
	StatCanceledAssignedTasks = xcap.NewStatisticInt64(statid.SchedulerCanceledAssignedTasks, "scheduler.tasks.canceled.assigned", xcap.AggregationTypeSum)

	// StatFailedTasks records the number of tasks that failed during execution.
	StatFailedTasks = xcap.NewStatisticInt64(statid.SchedulerFailedTasks, "scheduler.tasks.failed", xcap.AggregationTypeSum)
)
