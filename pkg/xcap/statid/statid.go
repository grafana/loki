// Package statid defines stable identifiers for wire-exported xcap statistics.
//
// IDs are part of the xcap wire protocol. Append new IDs only; never reorder
// or reuse existing IDs. To retire an ID, replace its declaration with _ and
// add its numeric value to reservedIDs below.
package statid

// ID uniquely identifies a statistic on the xcap wire protocol.
type ID uint32

const (
	// Invalid is not a valid wire statistic identifier.
	Invalid ID = iota

	// Data object statistics.
	DatasetPagesTotal
	DatasetPagesPruned
	ObjectBytesDownloaded
	DatasetPrimaryColumns
	DatasetSecondaryColumns
	DatasetPrimaryColumnPages
	DatasetSecondaryColumnPages
	DatasetMaxRows
	DatasetRowsAfterPruning
	DatasetPrimaryRowsRead
	DatasetSecondaryRowsRead
	DatasetPrimaryRowBytes
	DatasetSecondaryRowBytes
	DatasetPageDownloadTime
	DatasetPrimaryPagesDownloaded
	DatasetSecondaryPagesDownloaded
	DatasetPrimaryColumnBytes
	DatasetSecondaryColumnBytes
	DatasetPrimaryColumnUncompressedBytes
	DatasetSecondaryColumnUncompressedBytes
	DatasetReadCalls
	StreamPagesTotal
	StreamRelevantPages
	StreamRelevantRows
	StreamPageRuns

	// Metastore statistics.
	MetastoreTocTables
	MetastoreIndexObjects
	MetastoreSectionsResolved
	MetastorePointerSectionsOpened
	MetastorePointerSectionsProductive
	PostingsLabelColumnNameTotalPages
	PostingsLabelColumnNameRelevantPages
	PostingsBloomColumnNameTotalPages
	PostingsBloomColumnNameRelevantPages

	// Engine statistics.
	EngineLogicalPlanDuration
	EnginePhysicalPlanDuration
	EnginePrepareDuration
	EngineExecutionDuration
	EngineCloseDuration
	EnginePhysicalIndexQueryDuration
	EnginePhysicalOptimizeDuration
	EngineReadBatchDuration
	EngineProcessBatchDuration
	EngineResultRows
	EngineTimeToFirstBatch
	EngineResultSizeBytes

	// Executor statistics.
	PipelineRowsOut
	PipelineReadCalls
	PipelineReadDuration
	TaskCacheHits
	TaskCacheMisses
	TaskCacheBatches
	TaskCacheBytes
	DataObjScanCacheHits
	DataObjScanCacheMisses
	DataObjScanCacheBatches
	DataObjScanCacheBytes

	// Scheduler statistics.
	SchedulerPlannedTasks
	SchedulerQueuedTasks
	SchedulerAssignedTasks
	SchedulerExecutedTasks
	SchedulerCanceledPendingTasks
	SchedulerCanceledQueuedTasks
	SchedulerCanceledAssignedTasks
	SchedulerFailedTasks

	// Scheduler task statistics.
	SchedulerTaskStagingDuration
	SchedulerTaskQueueDuration
	SchedulerTaskExecutionDuration
	SchedulerTaskAssignmentRetries
	SchedulerTaskTotalDuration
	SchedulerTaskFinishTime

	// Worker task statistics.
	WorkerTaskExecutionSetupDuration
	WorkerTaskExecutionOpenDuration
	WorkerTaskExecutionReadDuration
	WorkerTaskExecutionSendDuration
	WorkerTaskExecutionReadRecvDuration
	WorkerTaskRecordsSent
	WorkerTaskRowsSent
	WorkerTaskDrainRecordsReceived

	// Workflow statistics.
	WorkflowPrunedTasks
	WorkflowPositiveCacheHits
	WorkflowNegativeCacheHits

	// Count is one greater than the largest valid ID.
	Count
)

// reservedIDs contains IDs for retired statistics. A retired ID remains part
// of the wire protocol so older workers can send it safely, but it must not be
// registered or decoded by newer processes.
//
// Keep entries in numeric order. Add a numeric ID rather than a statistic name:
// the name is deliberately removed from the const block when a statistic is
// retired.
var reservedIDs []ID

// IsReserved reports whether id was assigned to a retired statistic.
func IsReserved(id ID) bool {
	for _, reservedID := range reservedIDs {
		if id == reservedID {
			return true
		}
	}
	return false
}
