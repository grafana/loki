package workflow

import (
	"fmt"
	"time"

	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/schedulerstat"
	"github.com/grafana/loki/v3/pkg/engine/internal/worker/workerstat"
	"github.com/grafana/loki/v3/pkg/xcap"
)

func (wf *Workflow) printTaskSummary(task *Task, oldState TaskState, newStatus TaskStatus) {
	capture := newStatus.Capture
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

		durExecutionOpen     = xcap.Value[int64](capture, workerstat.TaskExecutionOpenDuration)
		durExecutionRead     = xcap.Value[int64](capture, workerstat.TaskExecutionReadDuration)
		durExecutionReadRecv = xcap.Value[int64](capture, workerstat.TaskExecutionReadRecvDuration)
		durExecutionSend     = xcap.Value[int64](capture, workerstat.TaskExecutionSendDuration)
		durExecutionOther    = durExecution - durExecutionOpen - durExecutionRead - durExecutionSend

		pagesDownloaded = xcap.Value[int64](capture, xcap.StatDatasetPrimaryPagesDownloaded) + xcap.Value[int64](capture, xcap.StatDatasetSecondaryPagesDownloaded)
		bytesDownloaded = xcap.Value[int64](capture, xcap.StatDatasetPrimaryColumnBytes) + xcap.Value[int64](capture, xcap.StatDatasetSecondaryColumnBytes)
		bytesProcessed  = xcap.Value[int64](capture, xcap.StatDatasetPrimaryRowBytes) + xcap.Value[int64](capture, xcap.StatDatasetSecondaryRowBytes)
		linesProcessed  = xcap.Value[int64](capture, xcap.StatDatasetPrimaryRowsRead) + xcap.Value[int64](capture, xcap.StatDatasetSecondaryRowsRead)
	)

	level.Info(wf.logger).Log(
		"msg", "task-summary",

		// Identity
		"task_id", task.ULID,
		"query_id", wf.opts.ID,
		"parent_task_id", wf.parentTaskID(task),
		"task_type", taskTypeName(task),
		"operator_type", taskOperatorType(task),

		// Outcome
		"status", taskStatusName(newStatus.State),
		"cancellation_phase", cancellationPhaseName(oldState, newStatus.State),
		"error", newStatus.Error,

		// Timings
		"duration_ms", time.Duration(durTotal).Milliseconds(),
		"duration_staging_ms", time.Duration(durStaging).Milliseconds(),
		"duration_queue_ms", time.Duration(durQueue).Milliseconds(),
		"duration_execution_ms", time.Duration(durExecution).Milliseconds(),
		"duration_other_ms", time.Duration(durOther).Milliseconds(),

		// Breakdown timings
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
		"pages_downloaded", pagesDownloaded, // Pages downloaded by readerDownloader
		"pages_bytes_downloaded", bytesDownloaded, // Bytes downloaded for pages by readerDownloader
		"total_bytes_downloaded", xcap.Value[int64](capture, dataobj.StatObjectBytesDownloaded),

		// Stage 10 counters.
		"batches_emitted", xcap.Value[int64](capture, xcap.TaskRecordsSent),
		"batches_consumed", xcap.Value[int64](capture, xcap.TaskDrainRecordsReceived),
		"bytes_processed", bytesProcessed, // TODO(rfratto): missing semantics for non-leaf nodes
		"lines_processed", linesProcessed, // TODO(rfratto): missing semantics for non-leaf nodes
		"lines_emitted", xcap.Value[int64](capture, xcap.TaskRowsSent),

		// Task result cache.
		"cache_check", taskResultCacheOutcome(capture),
	)

	if isScanTask(task) {
		// print log section data locality as a separate log line.
		wf.printTaskLogLocalitySummary(task, capture)
	}
}

func (wf *Workflow) printTaskLogLocalitySummary(task *Task, capture *xcap.Capture) {
	rowsTotal := xcap.ValueFromRegion[int64](capture, logs.RegionPrefix, xcap.StatDatasetMaxRows)
	relevantRows := xcap.ValueFromRegion[int64](capture, logs.RegionPrefix, dataobj.StatStreamRelevantRows)
	streamPagesTotal := xcap.ValueFromRegion[int64](capture, logs.RegionPrefix, dataobj.StatStreamPagesTotal)
	streamRelevantPages := xcap.ValueFromRegion[int64](capture, logs.RegionPrefix, dataobj.StatStreamRelevantPages)
	streamPageRuns := xcap.ValueFromRegion[int64](capture, logs.RegionPrefix, dataobj.StatStreamPageRuns)

	level.Info(wf.logger).Log(
		"msg", "task-log-locality-summary",
		// Identity
		"task_id", task.ULID,
		"query_id", wf.opts.ID,
		"parent_task_id", wf.parentTaskID(task),

		// Locality
		"dataset_rows_total", rowsTotal,
		"stream_relevant_rows", relevantRows,
		"stream_pages_total", streamPagesTotal,
		"stream_relevant_pages", streamRelevantPages,
		"stream_page_runs", streamPageRuns,
		"stream_row_relevance", ratio(relevantRows, rowsTotal),
		"stream_page_relevance", ratio(streamRelevantPages, streamPagesTotal),
		"stream_page_fragmentation", ratio(streamPageRuns, streamRelevantPages),
	)
}

func ratio(part, total int64) float64 {
	if total <= 0 {
		return 0
	}

	return float64(part) / float64(total)
}

// parentTaskID returns the task's parent ULID, or the zero ULID if the task
// is a root (one-parent assumption per the workflow planner).
func (wf *Workflow) parentTaskID(task *Task) any {
	wf.tasksMut.RLock()
	parents := wf.graph.Parents(task)
	wf.tasksMut.RUnlock()

	if len(parents) == 0 {
		return nil
	}
	// One-parent assumption per the workflow planner. If multi-parent task
	// graphs are ever introduced, this and the per-task log schema will need
	// to be revisited.
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

// taskOperatorType returns the type name of the task's root operator, or
// the empty string if the fragment has no usable root.
func taskOperatorType(task *Task) string {
	if task.Fragment == nil {
		return ""
	}

	root, err := task.Fragment.Root()
	if err != nil {
		return "none"
	}
	root, err = unwrapPhysicalNode(task.Fragment, root)
	if err != nil {
		return "none"
	}
	return root.Type().String()
}

// unwrapPhysicalNode unwraps "helper" physical nodes to reveal the actual root
// node.
func unwrapPhysicalNode(plan *physical.Plan, root physical.Node) (physical.Node, error) {
	// TODO(rfratto): this should really be behaviour defined in the physical
	// package.

	switch root := root.(type) {
	case *physical.Cache:
		next, err := getRootChild("caching", plan.Children(root))
		if err != nil {
			return nil, err
		}
		return unwrapPhysicalNode(plan, next)
	case *physical.Batching:
		next, err := getRootChild("batching", plan.Children(root))
		if err != nil {
			return nil, err
		}
		return unwrapPhysicalNode(plan, next)
	}

	return root, nil
}

func getRootChild(parent string, children []physical.Node) (physical.Node, error) {
	switch len(children) {
	case 0:
		return nil, fmt.Errorf("%s node has no children", parent)
	case 1:
		return children[0], nil
	default:
		return nil, fmt.Errorf("%s node has multiple children", parent)
	}
}

// taskStatusName maps a terminal [TaskState] to the spec's status enum.
func taskStatusName(state TaskState) string {
	switch state {
	case TaskStateCompleted:
		return "success"
	case TaskStateFailed:
		return "fail"
	case TaskStateCancelled:
		return "cancel"
	default:
		return state.String()
	}
}

// cancellationPhaseName returns the cancellation phase when newState is
// [TaskStateCancelled], or nil otherwise (so the log field is omitted for
// non-cancellation outcomes).
//
//   - "pre_assignment" — cancellation happened before the task was assigned
//     to a worker (oldState was Created or Pending).
//   - "during_execution" — cancellation happened while a worker was running
//     the task (oldState was Running).
func cancellationPhaseName(oldState, newState TaskState) any {
	if newState != TaskStateCancelled {
		return nil
	}
	switch oldState {
	case TaskStateCreated, TaskStatePending:
		return "pre_assignment"
	case TaskStateRunning:
		return "during_execution"
	default:
		return nil
	}
}

// taskResultCacheOutcome derives the task result cache outcome for the task
// from its capture, returning "hit", "miss", or "n/a".
func taskResultCacheOutcome(capture *xcap.Capture) string {
	var (
		hits, _   = xcap.TryValue[int64](capture, xcap.TaskCacheHits)
		misses, _ = xcap.TryValue[int64](capture, xcap.TaskCacheMisses)
	)

	switch {
	case hits > 0:
		return "hit"
	case misses > 0:
		return "miss"
	default:
		return "n/a"
	}
}
