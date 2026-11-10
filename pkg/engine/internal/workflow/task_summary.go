package workflow

import (
	"time"

	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/schedulerstat"
	"github.com/grafana/loki/v3/pkg/engine/internal/worker/workerstat"
	"github.com/grafana/loki/v3/pkg/xcap"
)

func (wf *Workflow) printTaskSummary(task *Task, result TaskResult, onCriticalPath bool) {
	capture := result.Capture
	if capture == nil {
		// Every terminal notification carries the per-task capture. Skip
		// rather than emit a log line that's all nils.
		return
	}

	var (
		durTotal     = xcap.Value[int64](capture, schedulerstat.TaskTotalDuration)
		durStaging   = xcap.Value[int64](capture, schedulerstat.TaskStagingDuration)
		durQueue     = xcap.Value[int64](capture, schedulerstat.TaskQueueDuration)
		durExecution = xcap.Value[int64](capture, schedulerstat.TaskExecutionDuration)
		durOther     = durTotal - durStaging - durQueue - durExecution

		durExecutionSetup    = xcap.Value[int64](capture, workerstat.TaskExecutionSetupDuration)
		durExecutionOpen     = xcap.Value[int64](capture, workerstat.TaskExecutionOpenDuration)
		durExecutionRead     = xcap.Value[int64](capture, workerstat.TaskExecutionReadDuration)
		durExecutionReadRecv = xcap.Value[int64](capture, workerstat.TaskExecutionReadRecvDuration)
		durExecutionSend     = xcap.Value[int64](capture, workerstat.TaskExecutionSendDuration)
		durExecutionOther    = durExecution - durExecutionSetup - durExecutionOpen - durExecutionRead - durExecutionSend

		pagesDownloaded = xcap.Value[int64](capture, dataobj.StatDatasetPrimaryPagesDownloaded) + xcap.Value[int64](capture, dataobj.StatDatasetSecondaryPagesDownloaded)
		bytesDownloaded = xcap.Value[int64](capture, dataobj.StatDatasetPrimaryColumnBytes) + xcap.Value[int64](capture, dataobj.StatDatasetSecondaryColumnBytes)
		bytesProcessed  = xcap.Value[int64](capture, dataobj.StatDatasetPrimaryRowBytes) + xcap.Value[int64](capture, dataobj.StatDatasetSecondaryRowBytes)
		linesProcessed  = xcap.Value[int64](capture, dataobj.StatDatasetPrimaryRowsRead) + xcap.Value[int64](capture, dataobj.StatDatasetSecondaryRowsRead)
	)

	level.Info(wf.logger).Log(
		"msg", "task-summary",

		// Identity
		"task_id", task.ULID,
		"workflow_id", wf.manifest.ID,
		"parent_task_id", wf.parentTaskID(task),
		"task_type", taskTypeName(task),
		"operator_type", taskOperatorType(task),

		// Outcome
		"status", taskOutcomeName(result.Outcome),
		"on_critical_path", onCriticalPath,
		"cancellation_phase", cancellationPhaseName(result),
		"error", result.Error,

		// Timings
		"duration_ms", time.Duration(durTotal).Milliseconds(),
		"duration_staging_ms", time.Duration(durStaging).Milliseconds(),
		"duration_queue_ms", time.Duration(durQueue).Milliseconds(),
		"duration_execution_ms", time.Duration(durExecution).Milliseconds(),
		"duration_other_ms", time.Duration(durOther).Milliseconds(),

		// Breakdown timings
		"duration_execution_setup_ms", time.Duration(durExecutionSetup).Milliseconds(),
		"duration_execution_open_ms", time.Duration(durExecutionOpen).Milliseconds(),
		"duration_execution_read_ms", time.Duration(durExecutionRead).Milliseconds(),
		"duration_execution_read_recv_ms", time.Duration(durExecutionReadRecv).Milliseconds(),
		"duration_execution_send_ms", time.Duration(durExecutionSend).Milliseconds(),
		"duration_execution_other_ms", time.Duration(durExecutionOther).Milliseconds(),

		// Assignment
		"assignment_retry_count", xcap.Value[int64](capture, schedulerstat.TaskAssignmentRetries),

		// Stage 9 (leaf) counters.
		"pages_total", xcap.Value[int64](capture, dataobj.StatDatasetPagesTotal),
		"pages_pruned", xcap.Value[int64](capture, dataobj.StatDatasetPagesPruned),
		"pages_downloaded", pagesDownloaded,
		"pages_bytes_downloaded", bytesDownloaded,
		"total_bytes_downloaded", xcap.Value[int64](capture, dataobj.StatObjectBytesDownloaded),

		// Stage 10 counters.
		"batches_emitted", xcap.Value[int64](capture, workerstat.TaskRecordsSent),
		"batches_consumed", xcap.Value[int64](capture, workerstat.TaskDrainRecordsReceived),
		"bytes_processed", bytesProcessed,
		"lines_processed", linesProcessed,
		"lines_emitted", xcap.Value[int64](capture, workerstat.TaskRowsSent),
	)
}

// parentTaskID returns the task's parent ULID, or nil if the task is a root.
//
// tasksMut must be held by the caller.
func (wf *Workflow) parentTaskID(task *Task) any {
	parents := wf.graph.Parents(task)

	if len(parents) == 0 {
		return nil
	}
	// A task has at most one parent. If multi-parent task graphs appear,
	// revisit this function and the per-task log schema.
	return parents[0].ULID
}

// taskTypeName returns "leaf" if the task has no external sources (i.e., it
// reads directly from storage rather than from another task), otherwise
// "non-leaf".
func taskTypeName(task *Task) string {
	if len(task.Sources) == 0 {
		return "leaf"
	}
	return "non-leaf"
}

// taskOperatorType returns the type name of the task's root operator. It
// returns the empty string if the task has no fragment, and "none" if the
// fragment has no single root.
func taskOperatorType(task *Task) string {
	if task.Fragment == nil {
		return ""
	}

	root, err := task.Fragment.Root()
	if err != nil {
		return "none"
	}
	return root.Type().String()
}

// taskOutcomeName maps a [TaskOutcome] to the summary's outcome label.
func taskOutcomeName(outcome TaskOutcome) string {
	switch outcome {
	case TaskOutcomeCompleted:
		return "success"
	case TaskOutcomeFailed:
		return "fail"
	case TaskOutcomeCancelled:
		return "cancel"
	default:
		return outcome.String()
	}
}

// cancellationPhaseName returns the cancellation phase for a cancelled result,
// or nil for other outcomes. The scheduler records execution duration only
// after a worker accepted the task, making its presence direct assignment
// evidence without retaining a lifecycle state solely for this log field.
func cancellationPhaseName(result TaskResult) any {
	if result.Outcome != TaskOutcomeCancelled {
		return nil
	}
	if result.Capture == nil {
		return "pre_assignment"
	}
	if _, assigned := xcap.TryValue[int64](result.Capture, schedulerstat.TaskExecutionDuration); !assigned {
		return "pre_assignment"
	}
	return "during_execution"
}
