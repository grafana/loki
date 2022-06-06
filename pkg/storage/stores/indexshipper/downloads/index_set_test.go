package downloads

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/stores/indexshipper/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/storage"
	util_log "github.com/grafana/loki/pkg/util/log"
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

	require.NoError(t, idxSet.Init(false))

	return idxSet.(*indexSet), idxSet.Close
}

func TestIndexSet_Init(t *testing.T) {
	tempDir := t.TempDir()
	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)
	var indexesSetup []string

	checkIndexSet := func() {
		indexSet, stopFunc := buildTestIndexSet(t, userID, tempDir)
		require.Len(t, indexSet.index, len(indexesSetup))
		verifyIndexForEach(t, indexesSetup, func(callbackFunc func(index.Index) error) error {
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
			verifyIndexForEach(t, indexesSetup, func(callbackFunc func(index.Index) error) error {
				return indexSet.ForEach(context.Background(), callbackFunc)
			})
		})
	}
}
