package compactor

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cortexproject/cortex/pkg/chunk/local"
	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/storage/stores/shipper/testutil"
)

const (
	objectsStorageDirName = "objects"
	workingDirName        = "working-dir"
)

func TestTable_Compaction(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "table-compaction")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	tableName := "test"
	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)
	tablePathInStorage := filepath.Join(objectStoragePath, tableName)
	tableWorkingDirectory := filepath.Join(tempDir, workingDirName, tableName)

	// setup some dbs
	numDBs := compactMinDBs * 2
	numRecordsPerDB := 100

	dbsToSetup := make(map[string]testutil.DBRecords)
	for i := 0; i < numDBs; i++ {
		dbsToSetup[fmt.Sprint(i)] = testutil.DBRecords{
			Start:      i * numRecordsPerDB,
			NumRecords: (i + 1) * numRecordsPerDB,
		}
	}

	testutil.SetupDBTablesAtPath(t, tableName, objectStoragePath, dbsToSetup, true)

	// setup exact same copy of dbs for comparison.
	testutil.SetupDBTablesAtPath(t, "test-copy", objectStoragePath, dbsToSetup, false)

	// do the compaction
	objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStoragePath})
	require.NoError(t, err)

	table, err := newTable(context.Background(), tableWorkingDirectory, objectClient)
	require.NoError(t, err)

	require.NoError(t, table.compact())

	// verify that we have only 1 file left in storage after compaction.
	files, err := ioutil.ReadDir(tablePathInStorage)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.True(t, strings.HasSuffix(files[0].Name(), ".gz"))

	// verify we have all the kvs in compacted db which were there in source dbs.
	compareCompactedDB(t, filepath.Join(tablePathInStorage, files[0].Name()), filepath.Join(objectStoragePath, "test-copy"))
}

func TestTable_CompactionFailure(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "table-compaction-failure")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	tableName := "test"
	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)
	tablePathInStorage := filepath.Join(objectStoragePath, tableName)
	tableWorkingDirectory := filepath.Join(tempDir, workingDirName, tableName)

	// setup some dbs
	numDBs := compactMinDBs * 2
	numRecordsPerDB := 100

	dbsToSetup := make(map[string]testutil.DBRecords)
	for i := 0; i < numDBs; i++ {
		dbsToSetup[fmt.Sprint(i)] = testutil.DBRecords{
			Start:      i * numRecordsPerDB,
			NumRecords: (i + 1) * numRecordsPerDB,
		}
	}

	testutil.SetupDBTablesAtPath(t, tableName, objectStoragePath, dbsToSetup, true)

	// put a non-boltdb file in the table which should cause the compaction to fail in the middle because it would fail to open that file with boltdb client.
	require.NoError(t, ioutil.WriteFile(filepath.Join(tablePathInStorage, "fail.txt"), []byte("fail the compaction"), 0666))

	// do the compaction
	objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStoragePath})
	require.NoError(t, err)

	table, err := newTable(context.Background(), tableWorkingDirectory, objectClient)
	require.NoError(t, err)

	// compaction should fail due to a non-boltdb file.
	require.Error(t, table.compact())

	// ensure that files in storage are intact.
	files, err := ioutil.ReadDir(tablePathInStorage)
	require.NoError(t, err)
	require.Len(t, files, numDBs+1)

	// ensure that we have cleanup the local working directory after failing the compaction.
	require.NoFileExists(t, tableWorkingDirectory)

	// remove the non-boltdb file and ensure that compaction succeeds now.
	require.NoError(t, os.Remove(filepath.Join(tablePathInStorage, "fail.txt")))

	table, err = newTable(context.Background(), tableWorkingDirectory, objectClient)
	require.NoError(t, err)
	require.NoError(t, table.compact())

	// ensure that we have cleanup the local working directory after successful compaction.
	require.NoFileExists(t, tableWorkingDirectory)
}

func compareCompactedDB(t *testing.T, compactedDBPath string, sourceDBsPath string) {
	tempDir, err := ioutil.TempDir("", "compare-compacted-db")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	decompressedFilePath := filepath.Join(tempDir, filepath.Base(compactedDBPath))
	testutil.DecompressFile(t, compactedDBPath, decompressedFilePath)

	compactedDB, err := local.OpenBoltdbFile(decompressedFilePath)
	require.NoError(t, err)

	defer func() {
		require.NoError(t, compactedDB.Close())
	}()

	sourceFiles, err := ioutil.ReadDir(sourceDBsPath)
	require.NoError(t, err)

	err = compactedDB.View(func(tx *bbolt.Tx) error {
		compactedBucket := tx.Bucket(bucketName)
		require.NotNil(t, compactedBucket)

		for _, file := range sourceFiles {
			srcDB, err := local.OpenBoltdbFile(filepath.Join(sourceDBsPath, file.Name()))
			require.NoError(t, err)

			err = srcDB.View(func(tx *bbolt.Tx) error {
				srcBucket := tx.Bucket(bucketName)
				require.NotNil(t, srcBucket)

				return srcBucket.ForEach(func(k, v []byte) error {
					val := compactedBucket.Get(k)
					require.NotNil(t, val)
					require.Equal(t, v, val)
					return nil
				})

			})
			require.NoError(t, err)

			require.NoError(t, srcDB.Close())
		}
		return nil
	})

	require.NoError(t, err)
}
