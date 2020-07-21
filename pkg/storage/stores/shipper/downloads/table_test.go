package downloads

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/stores/shipper/testutil"
)

const (
	cacheDirName          = "cache"
	objectsStorageDirName = "objects"
)

type stopFunc func()

func buildTestClients(t *testing.T, path string) (*local.BoltIndexClient, *local.FSObjectClient) {
	cachePath := filepath.Join(path, cacheDirName)

	boltDBIndexClient, err := local.NewBoltDBIndexClient(local.BoltDBConfig{Directory: cachePath})
	require.NoError(t, err)

	objectStoragePath := filepath.Join(path, objectsStorageDirName)
	fsObjectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStoragePath})
	require.NoError(t, err)

	return boltDBIndexClient, fsObjectClient
}

func buildTestTable(t *testing.T, path string) (*Table, *local.BoltIndexClient, stopFunc) {
	boltDBIndexClient, fsObjectClient := buildTestClients(t, path)
	cachePath := filepath.Join(path, cacheDirName)

	table := NewTable("test", cachePath, fsObjectClient, boltDBIndexClient, newMetrics(nil))

	// wait for either table to get ready or a timeout hits
	select {
	case <-table.IsReady():
	case <-time.Tick(2 * time.Second):
		t.Fatal("failed to initialize table in time")
	}

	// there should be no error in initialization of the table
	require.NoError(t, table.Err())

	return table, boltDBIndexClient, func() {
		table.Close()
		boltDBIndexClient.Stop()
	}
}

func TestTable_Query(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "table-writes")
	require.NoError(t, err)

	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)

	testDBs := map[string]testutil.DBRecords{
		"db1": {
			Start:      0,
			NumRecords: 10,
		},
		"db2": {
			Start:      10,
			NumRecords: 10,
		},
		"db3": {
			Start:      20,
			NumRecords: 10,
		},
	}

	testutil.SetupDBTablesAtPath(t, "test", objectStoragePath, testDBs)

	table, _, stopFunc := buildTestTable(t, tempDir)
	defer func() {
		stopFunc()
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	testutil.TestSingleQuery(t, chunk.IndexQuery{}, table, 0, 30)
}

func TestTable_Sync(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "table-writes")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)
	tableName := "test"
	tablePathInStorage := filepath.Join(objectStoragePath, tableName)

	// list of dbs to create except newDB that would be added later as part of updates
	deleteDB := "delete"
	noUpdatesDB := "no-updates"
	updateDB := "update"
	newDB := "new"

	testDBs := map[string]testutil.DBRecords{
		deleteDB: {
			Start:      0,
			NumRecords: 10,
		},
		noUpdatesDB: {
			Start:      10,
			NumRecords: 10,
		},
		updateDB: {
			Start:      20,
			NumRecords: 10,
		},
	}

	// setup the table in storage with some records
	testutil.SetupDBTablesAtPath(t, tableName, objectStoragePath, testDBs)

	// create table instance
	table, boltdbClient, stopFunc := buildTestTable(t, tempDir)
	defer func() {
		stopFunc()
	}()

	// query table to see it has expected records setup
	testutil.TestSingleQuery(t, chunk.IndexQuery{}, table, 0, 30)

	// add a sleep since we are updating a file and CI is sometimes too fast to create a difference in mtime of files
	time.Sleep(time.Second)

	// remove deleteDB, update updateDB and add the newDB
	require.NoError(t, os.Remove(filepath.Join(tablePathInStorage, deleteDB)))
	testutil.AddRecordsToDB(t, filepath.Join(tablePathInStorage, updateDB), boltdbClient, 30, 10)
	testutil.AddRecordsToDB(t, filepath.Join(tablePathInStorage, newDB), boltdbClient, 40, 10)

	// sync the table
	require.NoError(t, table.Sync(context.Background()))

	// query and verify table has expected records from new and updated db and the records from deleted db are gone
	testutil.TestSingleQuery(t, chunk.IndexQuery{}, table, 10, 40)

	// verify files in cache where dbs for the table are synced to double check.
	expectedFilesInDir := map[string]struct{}{
		updateDB:    {},
		noUpdatesDB: {},
		newDB:       {},
	}
	filesInfo, err := ioutil.ReadDir(tablePathInStorage)
	require.NoError(t, err)
	require.Len(t, table.dbs, len(expectedFilesInDir))

	for _, fileInfo := range filesInfo {
		require.False(t, fileInfo.IsDir())
		_, ok := expectedFilesInDir[fileInfo.Name()]
		require.True(t, ok)
	}
}
