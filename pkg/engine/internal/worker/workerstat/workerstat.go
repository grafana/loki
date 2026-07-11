// Package workerstat declares worker-recorded per-task observation
// statistics.
package workerstat

import "github.com/grafana/loki/v3/pkg/xcap"

// These stats live in their own package rather than in
// [pkg/engine/internal/worker] to avoid a cyclic dependency between workflow
// and the worker.

var (
	// TaskExecutionSetupDuration is the time (in nanoseconds) spent preparing a
	// task for execution before its pipeline is opened and drained.
	TaskExecutionSetupDuration = xcap.NewStatisticInt64("worker.task.execution.setup.duration", xcap.AggregationTypeSum)

	// TaskExecutionOpenDuration is the time (in nanoseconds) spent opening the
	// root pipeline before the worker began draining record batches from it.
	TaskExecutionOpenDuration = xcap.NewStatisticInt64("worker.task.execution.open.duration", xcap.AggregationTypeSum)

	// TaskExecutionReadDuration is the total time (in nanoseconds) the worker
	// spent in calls to Pipeline.Read while draining the root pipeline.
	TaskExecutionReadDuration = xcap.NewStatisticInt64("worker.task.execution.read.duration", xcap.AggregationTypeSum)

	// TaskExecutionSendDuration is the total time (in nanoseconds) the worker
	// spent forwarding record batches to external sinks while draining the root
	// pipeline.
	TaskExecutionSendDuration = xcap.NewStatisticInt64("worker.task.execution.send.duration", xcap.AggregationTypeSum)

	// TaskExecutionReadRecvDuration is the total time (in nanoseconds) spent
	// inside the per-node receive path that feeds downstream input batches into
	// non-leaf tasks. It is a sub-phase of [TaskExecutionReadDuration].
	TaskExecutionReadRecvDuration = xcap.NewStatisticInt64("worker.task.execution.read.recv.duration", xcap.AggregationTypeSum)

	TaskRecordsSent          = xcap.NewStatisticInt64("task.records.sent", xcap.AggregationTypeSum)
	TaskRowsSent             = xcap.NewStatisticInt64("task.rows.sent", xcap.AggregationTypeSum)
	TaskDrainRecordsReceived = xcap.NewStatisticInt64("task.drain.records.received", xcap.AggregationTypeSum)
	TaskWireBytes            = xcap.NewStatisticInt64("task.wire.bytes", xcap.AggregationTypeSum)
)
