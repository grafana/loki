package downloads

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper/testutil"
	util_log "github.com/grafana/loki/pkg/util/log"
)

const tableName = "test"

func buildTestIndexSet(t *testing.T, userID, path string) (*indexSet, stopFunc) {
	boltDBIndexClient, storageClient := buildTestClients(t, path)
	cachePath := filepath.Join(path, cacheDirName)

	baseIndexSet := storage.NewIndexSet(storageClient, userID != "")
	idxSet, err := NewIndexSet(tableName, userID, filepath.Join(cachePath, tableName, userID), baseIndexSet, boltDBIndexClient, util_log.Logger)
	require.NoError(t, err)

	require.NoError(t, idxSet.Init(false))

	return idxSet.(*indexSet), func() {
		idxSet.Close()
		boltDBIndexClient.Stop()
	}
}

func TestIndexSet_Init(t *testing.T) {
	tempDir := t.TempDir()
	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)
	testDBs := map[string]testutil.DBConfig{}

	checkIndexSet := func() {
		indexSet, stopFunc := buildTestIndexSet(t, userID, tempDir)
		require.Len(t, indexSet.dbs, len(testDBs))
		testutil.TestSingleTableQuery(t, userID, []index.Query{{}}, indexSet, 0, len(testDBs)*10)
		stopFunc()
	}

	// check index set without any local files and in storage
	checkIndexSet()

	// setup some dbs in object storage
	for i := 0; i < 10; i++ {
		testDBs[fmt.Sprint(i)] = testutil.DBConfig{
			CompressFile: i%2 == 0,
			DBRecords: testutil.DBRecords{
				Start:      i * 10,
				NumRecords: 10,
			},
		}
	}

	testutil.SetupDBsAtPath(t, filepath.Join(objectStoragePath, tableName, userID), testDBs, nil)

	// check index set twice; first run to have new files to download, second run to test with no changes in storage.
	for i := 0; i < 2; i++ {
		checkIndexSet()
	}

	// change a boltdb file to text file which would fail to open.
	indexSetPathPathInCache := filepath.Join(tempDir, cacheDirName, tableName, userID)
	require.NoError(t, ioutil.WriteFile(filepath.Join(indexSetPathPathInCache, "0"), []byte("invalid boltdb file"), 0o666))

	// check index set with a corrupt file which should get downloaded again from storage
	checkIndexSet()

	// delete a file from storage which should get removed from local as well
	indexSetPathPathInStorage := filepath.Join(objectStoragePath, tableName, userID)
	require.NoError(t, os.Remove(filepath.Join(indexSetPathPathInStorage, "9")))
	delete(testDBs, "9")

	checkIndexSet()
}

func TestIndexSet_doConcurrentDownload(t *testing.T) {
	tempDir := t.TempDir()
	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)

	for _, tc := range []int{0, 10, maxDownloadConcurrency, maxDownloadConcurrency * 2} {
		t.Run(fmt.Sprintf("%d dbs", tc), func(t *testing.T) {
			userID := fmt.Sprint(tc)
			testDBs := map[string]testutil.DBConfig{}

			for i := 0; i < tc; i++ {
				testDBs[fmt.Sprint(i)] = testutil.DBConfig{
					CompressFile: i%2 == 0,
					DBRecords: testutil.DBRecords{
						Start:      i * 10,
						NumRecords: 10,
					},
				}
			}

			testutil.SetupDBsAtPath(t, filepath.Join(objectStoragePath, tableName, userID), testDBs, nil)

			indexSet, stopFunc := buildTestIndexSet(t, userID, tempDir)
			defer func() {
				stopFunc()
			}()

			// ensure that we have `tc` number of files downloaded and opened.
			if tc > 0 {
				require.Len(t, indexSet.dbs, tc)
			}
			testutil.TestSingleTableQuery(t, userID, []index.Query{{}}, indexSet, 0, tc*10)
		})
	}
}
