package downloads

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// TestTableManager_TriggerSyncRecordsManualMetric exercises the manual-sync wiring
// through the real syncTables: TriggerSync delegates to the syncManager, the
// injected work runs syncTables with the "manual" trigger, and the success is
// recorded under that label. (The guard/status lifecycle is covered in
// sync_manager_test.go.)
func TestTableManager_TriggerSyncRecordsManualMetric(t *testing.T) {
	tm, stop := buildTestTableManager(t, t.TempDir(), nil)
	defer stop()

	require.True(t, tm.TriggerSync())
	require.Eventually(t, func() bool {
		return !tm.SyncStatus().InProgress
	}, 5*time.Second, 10*time.Millisecond)

	require.Equal(t, float64(1), testutil.ToFloat64(
		tm.metrics.tablesSyncOperationTotal.WithLabelValues(statusSuccess, syncTriggerManual)))
}
