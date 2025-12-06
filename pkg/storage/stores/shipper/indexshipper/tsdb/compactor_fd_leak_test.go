package tsdb

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

// TestSetupBuilder_FileDescriptorLeak tests that setupBuilder properly closes
// files immediately after use, preventing file descriptor leaks when processing
// many source files.
//
// This test detects the leak by monitoring file descriptors DURING the execution
// of setupBuilder, not after it returns. The bug causes files to accumulate during
// the loop because defer statements keep them open until the function returns.
func TestSetupBuilder_FileDescriptorLeak(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("File descriptor monitoring requires Linux /proc filesystem")
	}

	now := model.Now()
	periodConfig := config.PeriodConfig{
		IndexTables: config.IndexPeriodicTableConfig{
			PeriodicTableConfig: config.PeriodicTableConfig{Period: config.ObjectStorageIndexRequiredPeriod}},
		Schema: "v12",
	}
	indexBkts := IndexBuckets(now, now, []config.TableRange{periodConfig.GetIndexTableNumberRange(config.DayTime{Time: now})})
	tableName := indexBkts[0]

	tempDir := t.TempDir()
	objectStoragePath := filepath.Join(tempDir, "objects")
	tablePathInStorage := filepath.Join(objectStoragePath, tableName.Prefix)
	tableWorkingDirectory := filepath.Join(tempDir, "working-dir", tableName.Prefix)

	require.NoError(t, util.EnsureDirectory(objectStoragePath))
	require.NoError(t, util.EnsureDirectory(tablePathInStorage))
	require.NoError(t, util.EnsureDirectory(tableWorkingDirectory))

	// Create many per-tenant index files to simulate the scenario where
	// many files need to be processed
	// We need enough files to make the leak visible
	numFiles := 50 // Use enough files to detect the leak
	indexFormat, err := periodConfig.TSDBFormat()
	require.NoError(t, err)

	// Create per-tenant index files in the user's directory
	userTablePath := filepath.Join(tablePathInStorage, "user1")
	require.NoError(t, util.EnsureDirectory(userTablePath))

	lbls := mustParseLabels(`{foo="bar"}`)
	for i := 0; i < numFiles; i++ {
		streams := []stream{
			buildStream(lbls, buildChunkMetas(int64(i*1000), int64(i*1000+100)), ""),
		}
		setupPerTenantIndex(t, indexFormat, streams, userTablePath, time.Unix(int64(i), 0))
	}

	// Build the client and index set
	objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStoragePath})
	require.NoError(t, err)

	idxSet, err := newMockIndexSet("user1", tableName.Prefix, filepath.Join(tableWorkingDirectory, "user1"), objectClient)
	require.NoError(t, err)

	// Get initial file descriptor count
	initialFDCount := getFDCount(t)

	// Channel to signal when setupBuilder starts and ends
	started := make(chan struct{})
	done := make(chan struct{})
	maxFDCount := 0
	var maxFDCountMutex sync.Mutex

	// Monitor file descriptors in a separate goroutine
	// We need to monitor DURING the execution to catch the leak
	monitorDone := make(chan struct{})
	fdSamples := make([]int, 0, 1000)
	go func() {
		defer close(monitorDone)
		<-started
		for {
			select {
			case <-done:
				return
			default:
				count := getFDCount(t)
				fdSamples = append(fdSamples, count)
				maxFDCountMutex.Lock()
				if count > maxFDCount {
					maxFDCount = count
				}
				maxFDCountMutex.Unlock()
				time.Sleep(5 * time.Millisecond) // Check every 5ms for better detection
			}
		}
	}()

	// Call setupBuilder in a goroutine so we can monitor during execution
	ctx := context.Background()
	var builder *Builder
	var builderErr error
	go func() {
		close(started)
		builder, builderErr = setupBuilder(ctx, indexFormat, "user1", idxSet, []Index{})
		close(done)
	}()

	// Wait for completion
	<-done
	<-monitorDone

	require.NoError(t, builderErr)
	require.NotNil(t, builder)

	// Get final file descriptor count
	finalFDCount := getFDCount(t)

	maxFDCountMutex.Lock()
	peakFDCount := maxFDCount
	maxFDCountMutex.Unlock()

	// Calculate the increase in file descriptors
	// With the bug: file descriptors accumulate during the loop (peak will be high)
	// With the fix: file descriptors are closed immediately (peak should be low)
	fdIncrease := peakFDCount - initialFDCount

	// Analyze FD samples to see if there's a pattern of accumulation
	var avgFDCount int
	if len(fdSamples) > 0 {
		sum := 0
		for _, sample := range fdSamples {
			sum += sample
		}
		avgFDCount = sum / len(fdSamples)
	}

	t.Logf("File descriptor stats: initial=%d, peak=%d, avg=%d, final=%d, increase=%d, numFiles=%d, samples=%d",
		initialFDCount, peakFDCount, avgFDCount, finalFDCount, fdIncrease, numFiles, len(fdSamples))

	// The key test: with the bug, file descriptors accumulate during the loop.
	// Each file opened adds ~1-2 FDs (one for the file, possibly one for mmap).
	// With 30 files, we'd expect to see at least 20+ FDs accumulated if the bug exists.
	// With the fix, files are closed immediately, so peak should be much lower.

	// Calculate expected accumulation: each file might use 1-2 FDs
	// With the bug, we'd see accumulation proportional to numFiles
	// With the fix, we should see a constant overhead (maybe 5-10 FDs)
	expectedLeakFDs := numFiles * 1 // At least 1 FD per file if leak exists
	maxAllowedIncrease := 20        // Allow some overhead, but not proportional to numFiles

	if fdIncrease >= expectedLeakFDs/2 {
		// If we see accumulation proportional to numFiles, it's a leak
		t.Errorf("File descriptor leak detected! Peak increase: %d (expected leak: ~%d, allowed: %d). "+
			"This indicates files are not being closed immediately during processing. "+
			"With the fix, file descriptors should be closed right after use, not deferred until function return. "+
			"FD samples show accumulation pattern.",
			fdIncrease, expectedLeakFDs, maxAllowedIncrease)
	}

	// Verify the builder was populated correctly
	require.NotNil(t, builder.streams)
	require.Greater(t, len(builder.streams), 0, "Builder should have streams")
}

// getFDCount returns the current number of open file descriptors
func getFDCount(t *testing.T) int {
	if runtime.GOOS != "linux" {
		return 0
	}
	fds, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatalf("Failed to read /proc/self/fd: %v", err)
	}
	return len(fds)
}

// TestSetupBuilder_ManyFiles verifies that setupBuilder can handle processing
// many files without running into resource exhaustion issues.
func TestSetupBuilder_ManyFiles(t *testing.T) {
	now := model.Now()
	periodConfig := config.PeriodConfig{
		IndexTables: config.IndexPeriodicTableConfig{
			PeriodicTableConfig: config.PeriodicTableConfig{Period: config.ObjectStorageIndexRequiredPeriod}},
		Schema: "v12",
	}
	indexBkts := IndexBuckets(now, now, []config.TableRange{periodConfig.GetIndexTableNumberRange(config.DayTime{Time: now})})
	tableName := indexBkts[0]

	tempDir := t.TempDir()
	objectStoragePath := filepath.Join(tempDir, "objects")
	tablePathInStorage := filepath.Join(objectStoragePath, tableName.Prefix)
	tableWorkingDirectory := filepath.Join(tempDir, "working-dir", tableName.Prefix)

	require.NoError(t, util.EnsureDirectory(objectStoragePath))
	require.NoError(t, util.EnsureDirectory(tablePathInStorage))
	require.NoError(t, util.EnsureDirectory(tableWorkingDirectory))

	// Create a large number of files
	numFiles := 100
	indexFormat, err := periodConfig.TSDBFormat()
	require.NoError(t, err)

	// Create per-tenant index files in the user's directory
	userTablePath := filepath.Join(tablePathInStorage, "user1")
	require.NoError(t, util.EnsureDirectory(userTablePath))

	lbls := mustParseLabels(`{foo="bar"}`)
	for i := 0; i < numFiles; i++ {
		streams := []stream{
			buildStream(lbls, buildChunkMetas(int64(i*1000), int64(i*1000+100)), ""),
		}
		setupPerTenantIndex(t, indexFormat, streams, userTablePath, time.Unix(int64(i), 0))
	}

	objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStoragePath})
	require.NoError(t, err)

	idxSet, err := newMockIndexSet("user1", tableName.Prefix, filepath.Join(tableWorkingDirectory, "user1"), objectClient)
	require.NoError(t, err)

	// This should complete without errors even with many files
	// because files are closed immediately after processing
	ctx := context.Background()
	builder, err := setupBuilder(ctx, indexFormat, "user1", idxSet, []Index{})
	require.NoError(t, err)
	require.NotNil(t, builder)

	// Verify builder has the expected data
	builder.FinalizeChunks()
	require.Greater(t, len(builder.streams), 0)
}
