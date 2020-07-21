package uploads

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/storage/stores/shipper/testutil"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	"github.com/stretchr/testify/require"
)

const (
	indexDirName          = "index"
	objectsStorageDirName = "objects"
)

func buildTestClients(t *testing.T, path string) (*local.BoltIndexClient, *local.FSObjectClient) {
	indexPath := filepath.Join(path, indexDirName)

	boltDBIndexClient, err := local.NewBoltDBIndexClient(local.BoltDBConfig{Directory: indexPath})
	require.NoError(t, err)

	objectStoragePath := filepath.Join(path, objectsStorageDirName)
	fsObjectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStoragePath})
	require.NoError(t, err)

	return boltDBIndexClient, fsObjectClient
}

type stopFunc func()

func buildTestTable(t *testing.T, path string) (*Table, *local.BoltIndexClient, stopFunc) {
	boltDBIndexClient, fsObjectClient := buildTestClients(t, path)
	indexPath := filepath.Join(path, indexDirName)

	table, err := NewTable(indexPath, "test", fsObjectClient, boltDBIndexClient)
	require.NoError(t, err)

	return table, boltDBIndexClient, func() {
		table.Stop()
		boltDBIndexClient.Stop()
	}
}

func TestLoadTable(t *testing.T) {
	indexPath, err := ioutil.TempDir("", "table-load")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(indexPath))
	}()

	boltDBIndexClient, err := local.NewBoltDBIndexClient(local.BoltDBConfig{Directory: indexPath})
	require.NoError(t, err)

	defer func() {
		boltDBIndexClient.Stop()
	}()

	// setup some dbs for a table at a path.
	tablePath := testutil.SetupDBTablesAtPath(t, "test-table", indexPath, map[string]testutil.DBRecords{
		"db1": {
			Start:      0,
			NumRecords: 10,
		},
		"db2": {
			Start:      10,
			NumRecords: 10,
		},
	})

	// try loading the table.
	table, err := LoadTable(tablePath, "test", nil, boltDBIndexClient)
	require.NoError(t, err)
	require.NotNil(t, table)

	defer func() {
		table.Stop()
	}()

	// query the loaded table to see if it has right data.
	testutil.TestSingleQuery(t, chunk.IndexQuery{}, table, 0, 20)
}

func TestTable_Write(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "table-writes")
	require.NoError(t, err)

	table, boltIndexClient, stopFunc := buildTestTable(t, tempDir)

	defer func() {
		stopFunc()
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	now := time.Now()
	// a couple of times for which we want to do writes to make the table create different shards
	writeTimes := []time.Time{now, now.Add(-(shardDBsByDuration + 5*time.Minute)), now.Add(-(shardDBsByDuration*3 + 3*time.Minute))}

	numFiles := 0

	// performing writes and checking whether the index gets written to right shard
	for i, tm := range writeTimes {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			batch := boltIndexClient.NewWriteBatch()
			testutil.AddRecordsToBatch(batch, "test", i*10, 10)
			require.NoError(t, table.write(context.Background(), tm, batch.(*local.BoltWriteBatch).Writes["test"]))

			numFiles++
			require.Equal(t, numFiles, len(table.dbs))

			expectedDBName := fmt.Sprint(tm.Truncate(shardDBsByDuration).Unix())
			db, ok := table.dbs[expectedDBName]
			require.True(t, ok)

			// test that the table has current + previous records
			testutil.TestSingleQuery(t, chunk.IndexQuery{}, table, 0, (i+1)*10)
			testutil.TestSingleDBQuery(t, chunk.IndexQuery{}, db, boltIndexClient, i*10, 10)
		})
	}
}

func TestTable_Upload(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "table-writes")
	require.NoError(t, err)

	table, boltIndexClient, stopFunc := buildTestTable(t, tempDir)
	require.NoError(t, err)

	defer func() {
		stopFunc()
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	now := time.Now()

	// write a batch for now
	batch := boltIndexClient.NewWriteBatch()
	testutil.AddRecordsToBatch(batch, "test", 0, 10)
	require.NoError(t, table.write(context.Background(), now, batch.(*local.BoltWriteBatch).Writes["test"]))

	// upload the table
	require.NoError(t, table.Upload(context.Background()))
	require.Len(t, table.dbs, 1)

	// compare the local dbs for the table with the dbs in remote storage after upload to ensure they have same data
	objectStorageDir := filepath.Join(tempDir, objectsStorageDirName)
	compareTableWithStorage(t, table, objectStorageDir)

	// add a sleep since we are updating a file and CI is sometimes too fast to create a difference in mtime of files
	time.Sleep(time.Second)

	// write another batch to same shard
	batch = boltIndexClient.NewWriteBatch()
	testutil.AddRecordsToBatch(batch, "test", 10, 10)
	require.NoError(t, table.write(context.Background(), now, batch.(*local.BoltWriteBatch).Writes["test"]))

	// write a batch to another shard
	batch = boltIndexClient.NewWriteBatch()
	testutil.AddRecordsToBatch(batch, "test", 20, 10)
	require.NoError(t, table.write(context.Background(), now.Add(shardDBsByDuration), batch.(*local.BoltWriteBatch).Writes["test"]))

	// upload the dbs to storage
	require.NoError(t, table.Upload(context.Background()))
	require.Len(t, table.dbs, 2)

	// check local dbs with remote dbs to ensure they have same data
	compareTableWithStorage(t, table, objectStorageDir)
}

func compareTableWithStorage(t *testing.T, table *Table, storageDir string) {
	for name, db := range table.dbs {
		objectKey := table.buildObjectKey(name)
		storageDB, err := local.OpenBoltdbFile(filepath.Join(storageDir, objectKey))
		require.NoError(t, err)

		testutil.CompareDBs(t, db, storageDB)
		require.NoError(t, storageDB.Close())
	}
}

func TestTable_Cleanup(t *testing.T) {
	testDir, err := ioutil.TempDir("", "cleanup")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(testDir))
	}()

	boltDBIndexClient, storageClient := buildTestClients(t, testDir)
	indexPath := filepath.Join(testDir, indexDirName)

	defer func() {
		boltDBIndexClient.Stop()
	}()

	// dbs for various scenarios to test
	outsideRetention := filepath.Join(indexPath, "outside-retention")
	outsideRetentionButModified := filepath.Join(indexPath, "outside-retention-but-mod")
	outsideRetentionButNeverUploaded := filepath.Join(indexPath, "outside-retention-but-no-uploaded")
	inRention := filepath.Join(indexPath, "in-retention")

	// build all the test dbs except for outsideRetentionButNeverUploaded
	testutil.AddRecordsToDB(t, outsideRetention, boltDBIndexClient, 0, 10)
	testutil.AddRecordsToDB(t, outsideRetentionButModified, boltDBIndexClient, 10, 10)
	testutil.AddRecordsToDB(t, inRention, boltDBIndexClient, 20, 10)

	// change the mtimes of dbs that should be outside retention
	require.NoError(t, os.Chtimes(outsideRetention, time.Now().Add(-2*dbRetainPeriod), time.Now().Add(-2*dbRetainPeriod)))
	require.NoError(t, os.Chtimes(outsideRetentionButModified, time.Now().Add(-2*dbRetainPeriod), time.Now().Add(-2*dbRetainPeriod)))

	// load existing dbs
	table, err := LoadTable(indexPath, "test", storageClient, boltDBIndexClient)
	require.NoError(t, err)
	require.Len(t, table.dbs, 3)

	defer func() {
		table.Stop()
	}()

	// upload all the existing dbs
	require.NoError(t, table.Upload(context.Background()))
	require.Len(t, table.uploadedDBsMtime, 3)

	// change the mtime of outsideRetentionButModified db after the upload
	require.NoError(t, os.Chtimes(outsideRetentionButModified, time.Now().Add(-dbRetainPeriod), time.Now().Add(-dbRetainPeriod)))

	// build and add the outsideRetentionButNeverUploaded db
	testutil.AddRecordsToDB(t, outsideRetentionButNeverUploaded, boltDBIndexClient, 30, 10)
	require.NoError(t, os.Chtimes(outsideRetentionButNeverUploaded, time.Now().Add(-2*dbRetainPeriod), time.Now().Add(-2*dbRetainPeriod)))
	_, err = table.getOrAddDB(filepath.Base(outsideRetentionButNeverUploaded))
	require.NoError(t, err)

	// there must be 4 dbs now in the table
	require.Len(t, table.dbs, 4)

	// cleanup the dbs
	require.NoError(t, table.Cleanup())

	// there must be 3 dbs now, it should have cleaned up only outsideRetention
	require.Len(t, table.dbs, 3)

	expectedDBs := []string{
		outsideRetentionButModified,
		outsideRetentionButNeverUploaded,
		inRention,
	}

	// verify open dbs with the table and actual db files in the index directory
	filesInfo, err := ioutil.ReadDir(indexPath)
	require.NoError(t, err)
	require.Len(t, filesInfo, len(expectedDBs))

	for _, expectedDB := range expectedDBs {
		_, ok := table.dbs[filepath.Base(expectedDB)]
		require.True(t, ok)

		_, err := os.Stat(expectedDB)
		require.NoError(t, err)
	}
}
