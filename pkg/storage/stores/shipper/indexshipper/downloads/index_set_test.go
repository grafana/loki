package downloads

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

func buildTestIndexSet(t *testing.T, userID, path string) (*indexSet, stopFunc) {
	storageClient := buildTestStorageClient(t, path)
	cachePath := filepath.Join(path, cacheDirName)

	baseIndexSet := storage.NewIndexSet(storageClient, userID != "")
	idxSet, err := NewIndexSet(tableName, userID, filepath.Join(cachePath, tableName, userID), baseIndexSet,
		func(path string) (index.Index, error) {
			return openMockIndexFile(t, path), nil
		}, util_log.Logger)
	require.NoError(t, err)

	require.NoError(t, idxSet.Init(false, util_log.Logger))

	return idxSet.(*indexSet), idxSet.Close
}

func TestIndexSet_Init(t *testing.T) {
	tempDir := t.TempDir()
	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)
	var indexesSetup []string

	checkIndexSet := func() {
		indexSet, stopFunc := buildTestIndexSet(t, userID, tempDir)
		require.Len(t, indexSet.index, len(indexesSetup))
		verifyIndexForEach(t, indexesSetup, func(callbackFunc index.ForEachIndexCallback) error {
			return indexSet.ForEach(context.Background(), callbackFunc)
		})
		stopFunc()
	}

	// check index set without any local files and in storage
	checkIndexSet()

	// setup some indexes in object storage
	setupIndexesAtPath(t, userID, filepath.Join(objectStoragePath, tableName, userID), 0, 10)
	indexesSetup = buildListOfExpectedIndexes(userID, 0, 10)

	// check index set twice; first run to have new files to download, second run to test with no changes in storage.
	for i := 0; i < 2; i++ {
		checkIndexSet()
	}

	// delete a file from storage which should get removed from local as well
	indexSetPathPathInStorage := filepath.Join(objectStoragePath, tableName, userID)
	require.NoError(t, os.Remove(filepath.Join(indexSetPathPathInStorage, indexesSetup[0])))
	indexesSetup = indexesSetup[1:]

	checkIndexSet()
}

func TestIndexSet_doConcurrentDownload(t *testing.T) {
	tempDir := t.TempDir()
	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)

	for _, tc := range []int{0, 10, maxDownloadConcurrency, maxDownloadConcurrency * 2} {
		t.Run(fmt.Sprintf("%d indexes", tc), func(t *testing.T) {
			userID := fmt.Sprint(tc)
			setupIndexesAtPath(t, userID, filepath.Join(objectStoragePath, tableName, userID), 0, tc)
			indexesSetup := buildListOfExpectedIndexes(userID, 0, tc)

			indexSet, stopFunc := buildTestIndexSet(t, userID, tempDir)
			defer func() {
				stopFunc()
			}()

			// ensure that we have `tc` number of files downloaded and opened.
			if tc > 0 {
				require.Len(t, indexSet.index, tc)
			}
			verifyIndexForEach(t, indexesSetup, func(callbackFunc index.ForEachIndexCallback) error {
				return indexSet.ForEach(context.Background(), callbackFunc)
			})
		})
	}
}

func TestIndexSet_Sync(t *testing.T) {
	tempDir := t.TempDir()
	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)
	tablePathInStorage := filepath.Join(objectStoragePath, tableName)

	var indexesSetup []string

	indexSet, stopFunc := buildTestIndexSet(t, "", tempDir)
	defer stopFunc()

	checkIndexSet := func() {
		require.Len(t, indexSet.index, len(indexesSetup))
		verifyIndexForEach(t, indexesSetup, func(callbackFunc index.ForEachIndexCallback) error {
			return indexSet.ForEach(context.Background(), callbackFunc)
		})
	}

	// setup some indexes in object storage
	setupIndexesAtPath(t, "", tablePathInStorage, 0, 10)
	indexesSetup = buildListOfExpectedIndexes("", 0, 10)

	// sync and verify the indexSet
	indexSet.baseIndexSet.RefreshIndexTableCache(context.Background(), tableName)
	require.NoError(t, indexSet.Sync(context.Background()))

	// check index set twice; first run to have new files to download, second run to test with no changes in storage.
	for i := 0; i < 2; i++ {
		checkIndexSet()
	}

	// delete a file from storage which should get removed from local as well
	require.NoError(t, os.Remove(filepath.Join(tablePathInStorage, indexesSetup[0])))
	indexesSetup = indexesSetup[1:]

	// sync and verify the indexSet
	indexSet.baseIndexSet.RefreshIndexTableCache(context.Background(), tableName)
	require.NoError(t, indexSet.Sync(context.Background()))
	checkIndexSet()

	// let us simulate a compaction to test stale index list cache handling

	// first, let us add a new file and refresh the index list cache
	oneMoreDB := "one-more-db"
	require.NoError(t, os.WriteFile(filepath.Join(tablePathInStorage, oneMoreDB), []byte(oneMoreDB), 0755))
	indexSet.baseIndexSet.RefreshIndexTableCache(context.Background(), tableName)

	// now, without syncing the indexset, let us compact the index in storage
	compactedDBName := "compacted-db"
	require.NoError(t, os.RemoveAll(tablePathInStorage))
	require.NoError(t, util.EnsureDirectory(tablePathInStorage))
	require.NoError(t, os.WriteFile(filepath.Join(tablePathInStorage, compactedDBName), []byte(compactedDBName), 0755))
	indexesSetup = []string{compactedDBName}

	// verify that we are getting errIndexListCacheTooStale without refreshing the list cache
	require.ErrorIs(t, errIndexListCacheTooStale, indexSet.sync(context.Background(), true, false))

	// let us run a sync which should detect the stale index list cache and sync the table after refreshing the cache
	require.NoError(t, indexSet.Sync(context.Background()))

	// verify that table has got only compacted db
	checkIndexSet()
}

func TestIndexSet_ForEach_ErrorPropagation(t *testing.T) {
	initErr := errors.New("simulated init failure")

	tests := []struct {
		name         string
		filesInStore int
		callInit     bool
		injectErr    error
		wantErr      error
	}{
		{
			// Init failed and cleanup successfully emptied t.index — the
			// original bug shape: parked readers unblock to len(t.index) == 0.
			name:         "empty index with init error propagates",
			filesInStore: 0,
			callInit:     false, // bypass Init to simulate the "post-defer, error set" state
			injectErr:    initErr,
			wantErr:      initErr,
		},
		{
			// Init failed, cleanupDB failed (df.Close returned an error) so
			// t.index still holds surviving orphan entries. Without the fix,
			// ForEach would iterate them and silently return partial data.
			name:         "populated index with init error propagates (orphan cleanup)",
			filesInStore: 3,
			callInit:     true, // Init populates t.index with 3 files
			injectErr:    initErr,
			wantErr:      initErr,
		},
		{
			// Legitimate empty state — no files anywhere, Init succeeded,
			// t.err stays nil. Regression guard against over-eager propagation.
			name:         "empty index without error returns nil",
			filesInStore: 0,
			callInit:     true,
			injectErr:    nil,
			wantErr:      nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()

			if tc.filesInStore > 0 {
				setupIndexesAtPath(t, userID, filepath.Join(tempDir, objectsStorageDirName, tableName, userID), 0, tc.filesInStore)
			}

			storageClient := buildTestStorageClient(t, tempDir)
			cachePath := filepath.Join(tempDir, cacheDirName)
			baseIndexSet := storage.NewIndexSet(storageClient, userID != "")

			idxSet, err := NewIndexSet(tableName, userID, filepath.Join(cachePath, tableName, userID), baseIndexSet,
				func(path string) (index.Index, error) {
					return openMockIndexFile(t, path), nil
				}, util_log.Logger)
			require.NoError(t, err)
			defer idxSet.Close()

			is := idxSet.(*indexSet)

			if tc.callInit {
				require.NoError(t, idxSet.Init(false, util_log.Logger))
			} else {
				// Bypass Init to simulate the exact post-defer state where
				// the readiness gate is open but t.err is set and t.index is
				// empty. Init would normally handle this via its defer at
				// index_set.go:114 (markReady).
				is.indexMtx.markReady()
			}

			if tc.injectErr != nil {
				is.err = tc.injectErr
			}

			require.Len(t, is.index, tc.filesInStore, "pre-condition: t.index size")

			// Both entry points must behave identically.
			for _, forEach := range []struct {
				name string
				fn   func(ctx context.Context, cb index.ForEachIndexCallback) error
			}{
				{"ForEach", idxSet.ForEach},
				{"ForEachConcurrent", idxSet.ForEachConcurrent},
			} {
				t.Run(forEach.name, func(t *testing.T) {
					var invocations atomic.Int32
					err := forEach.fn(context.Background(), func(_ bool, _ index.Index) error {
						invocations.Add(1)
						return nil
					})

					if tc.wantErr != nil {
						require.ErrorIs(t, err, tc.wantErr, "must propagate t.err")
						require.Equal(t, int32(0), invocations.Load(), "callback must not fire when the indexSet is broken")
					} else {
						require.NoError(t, err)
						require.Equal(t, int32(tc.filesInStore), invocations.Load(), "callback must fire once per index file")
					}
				})
			}
		})
	}
}
