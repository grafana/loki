package statid

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestIDStability pins representative IDs across every ownership group. IDs
// are wire protocol values: inserting or reordering declarations must fail
// this test rather than silently changing their meaning.
func TestIDStability(t *testing.T) {
	require.Equal(t, ID(1), DatasetPagesTotal)
	require.Equal(t, ID(25), StreamPageRuns)
	require.Equal(t, ID(26), MetastoreTocTables)
	require.Equal(t, ID(34), PostingsBloomColumnNameRelevantPages)
	require.Equal(t, ID(35), EngineLogicalPlanDuration)
	require.Equal(t, ID(46), EngineResultSizeBytes)
	require.Equal(t, ID(47), PipelineRowsOut)
	require.Equal(t, ID(57), DataObjScanCacheBytes)
	require.Equal(t, ID(58), SchedulerPlannedTasks)
	require.Equal(t, ID(65), SchedulerFailedTasks)
	require.Equal(t, ID(66), SchedulerTaskStagingDuration)
	require.Equal(t, ID(71), SchedulerTaskFinishTime)
	require.Equal(t, ID(72), WorkerTaskExecutionSetupDuration)
	require.Equal(t, ID(79), WorkerTaskDrainRecordsReceived)
	require.Equal(t, ID(80), WorkflowPrunedTasks)
	require.Equal(t, ID(82), WorkflowNegativeCacheHits)
	require.Equal(t, ID(83), Count)
}

func TestIsReserved(t *testing.T) {
	original := reservedIDs
	reservedIDs = []ID{DatasetPagesPruned}
	t.Cleanup(func() {
		reservedIDs = original
	})

	require.True(t, IsReserved(DatasetPagesPruned))
	require.False(t, IsReserved(DatasetPagesTotal))
}
