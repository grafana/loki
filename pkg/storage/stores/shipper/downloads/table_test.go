package downloads

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/local"
	"github.com/grafana/loki/pkg/storage/stores/shipper/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper/testutil"
)

const (
	cacheDirName          = "cache"
	objectsStorageDirName = "objects"
)

// storageClientWithFakeObjectsInList adds a fake object in the list call response which
// helps with testing the case where objects gets deleted in the middle of a Sync/Download operation due to compaction.
type storageClientWithFakeObjectsInList struct {
	StorageClient
}

func newStorageClientWithFakeObjectsInList(storageClient StorageClient) StorageClient {
	return storageClientWithFakeObjectsInList{storageClient}
}

func (o storageClientWithFakeObjectsInList) ListFiles(ctx context.Context, tableName string) ([]storage.IndexFile, error) {
	files, err := o.StorageClient.ListFiles(ctx, tableName)
	if err != nil {
		return nil, err
	}

	files = append(files, storage.IndexFile{
		Name:       "fake-object",
		ModifiedAt: time.Now(),
	})

	return files, nil
}

type stopFunc func()

func buildTestClients(t *testing.T, path string) (*local.BoltIndexClient, StorageClient) {
	cachePath := filepath.Join(path, cacheDirName)

	boltDBIndexClient, err := local.NewBoltDBIndexClient(local.BoltDBConfig{Directory: cachePath})
	require.NoError(t, err)

	objectStoragePath := filepath.Join(path, objectsStorageDirName)
	fsObjectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStoragePath})
	require.NoError(t, err)

	return boltDBIndexClient, storage.NewIndexStorageClient(fsObjectClient, "")
}

func buildTestTable(t *testing.T, tableName, path string) (*Table, *local.BoltIndexClient, stopFunc) {
	boltDBIndexClient, storageClient := buildTestClients(t, path)
	cachePath := filepath.Join(path, cacheDirName)

	table := NewTable(context.Background(), tableName, cachePath, storageClient, boltDBIndexClient, newMetrics(nil))

	// wait for either table to get ready or a timeout hits
	select {
	case <-table.ready:
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

func TestTable_MultiQueries(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "table-downloads-multi-queries")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

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

	testutil.SetupDBTablesAtPath(t, "test", objectStoragePath, testDBs, true)

	table, _, stopFunc := buildTestTable(t, "test", tempDir)
	defer func() {
		stopFunc()
	}()

	// build queries each looking for specific value from all the dbs
	var queries []chunk.IndexQuery
	for i := 5; i < 25; i++ {
		queries = append(queries, chunk.IndexQuery{ValueEqual: []byte(strconv.Itoa(i))})
	}

	// query the loaded table to see if it has right data.
	testutil.TestSingleTableQuery(t, queries, table, 5, 20)
}

func TestTable_Sync(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "table-sync")
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
	}

	// setup the table in storage with some records
	testutil.SetupDBTablesAtPath(t, tableName, objectStoragePath, testDBs, false)

	// create table instance
	table, boltdbClient, stopFunc := buildTestTable(t, "test", tempDir)
	defer func() {
		stopFunc()
	}()

	// replace the storage client with the one that adds fake objects in the list call
	table.storageClient = newStorageClientWithFakeObjectsInList(table.storageClient)

	// query table to see it has expected records setup
	testutil.TestSingleTableQuery(t, []chunk.IndexQuery{{}}, table, 0, 20)

	// add a sleep since we are updating a file and CI is sometimes too fast to create a difference in mtime of files
	time.Sleep(time.Second)

	// remove deleteDB and add the newDB
	require.NoError(t, os.Remove(filepath.Join(tablePathInStorage, deleteDB)))
	testutil.AddRecordsToDB(t, filepath.Join(tablePathInStorage, newDB), boltdbClient, 20, 10)

	// sync the table
	require.NoError(t, table.Sync(context.Background()))

	// query and verify table has expected records from new db and the records from deleted db are gone
	testutil.TestSingleTableQuery(t, []chunk.IndexQuery{{}}, table, 10, 20)

	// verify files in cache where dbs for the table are synced to double check.
	expectedFilesInDir := map[string]struct{}{
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

func TestTable_LastUsedAt(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "table-last-used-at")
	require.NoError(t, err)

	table, _, stopFunc := buildTestTable(t, "test", tempDir)
	defer func() {
		stopFunc()
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	// a newly built table should have last used at close to now.
	require.InDelta(t, time.Now().Unix(), table.LastUsedAt().Unix(), 1)

	// change the last used at to an hour before
	table.lastUsedAt = time.Now().Add(-time.Hour)
	require.InDelta(t, time.Now().Add(-time.Hour).Unix(), table.LastUsedAt().Unix(), 1)

	// query the table which should set the last used at to now.
	err = table.MultiQueries(context.Background(), []chunk.IndexQuery{{}}, func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
		return true
	})
	require.NoError(t, err)

	// check whether last used at got update to now.
	require.InDelta(t, time.Now().Unix(), table.LastUsedAt().Unix(), 1)
}

func TestTable_doParallelDownload(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "table-parallel-download")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)

	for _, tc := range []int{0, 10, downloadParallelism, downloadParallelism * 2} {
		t.Run(fmt.Sprintf("%d dbs", tc), func(t *testing.T) {
			testDBs := map[string]testutil.DBRecords{}

			for i := 0; i < tc; i++ {
				testDBs[fmt.Sprint(i)] = testutil.DBRecords{
					Start:      i * 10,
					NumRecords: 10,
				}
			}

			testutil.SetupDBTablesAtPath(t, fmt.Sprint(tc), objectStoragePath, testDBs, true)

			table, _, stopFunc := buildTestTable(t, fmt.Sprint(tc), tempDir)
			defer func() {
				stopFunc()
			}()

			// ensure that we have `tc` number of files downloaded and opened.
			require.Len(t, table.dbs, tc)
			testutil.TestSingleTableQuery(t, []chunk.IndexQuery{{}}, table, 0, tc*10)
		})
	}
}

func TestTable_DuplicateIndex(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "table-duplicate-index")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)

	testDBs := map[string]testutil.DBRecords{
		"db1": {
			Start:      0,
			NumRecords: 10,
		},
		"duplicate_db1": {
			Start:      0,
			NumRecords: 10,
		},
		"db2": {
			Start:      10,
			NumRecords: 10,
		},
		"partially_duplicate_db2": {
			Start:      10,
			NumRecords: 5,
		},
		"db3": {
			Start:      20,
			NumRecords: 10,
		},
	}

	testutil.SetupDBTablesAtPath(t, "test", objectStoragePath, testDBs, true)

	table, _, stopFunc := buildTestTable(t, "test", tempDir)
	defer func() {
		stopFunc()
	}()

	// build queries each looking for specific value from all the dbs
	var queries []chunk.IndexQuery
	for i := 5; i < 25; i++ {
		queries = append(queries, chunk.IndexQuery{ValueEqual: []byte(strconv.Itoa(i))})
	}

	// query the loaded table to see if it has right data.
	testutil.TestSingleTableQuery(t, queries, table, 5, 20)
}

func TestLoadTable(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "load-table")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)
	tableName := "test"

	dbs := make(map[string]testutil.DBRecords)
	for i := 0; i < 10; i++ {
		dbs[fmt.Sprint(i)] = testutil.DBRecords{
			Start:      i,
			NumRecords: 1,
		}
	}

	// setup the table in storage with some records
	testutil.SetupDBTablesAtPath(t, tableName, objectStoragePath, dbs, false)

	boltDBIndexClient, storageClient := buildTestClients(t, tempDir)
	cachePath := filepath.Join(tempDir, cacheDirName)

	storageClient = newStorageClientWithFakeObjectsInList(storageClient)

	// try loading the table.
	table, err := LoadTable(context.Background(), tableName, cachePath, storageClient, boltDBIndexClient, newMetrics(nil))
	require.NoError(t, err)
	require.NotNil(t, table)

	// query the loaded table to see if it has right data.
	testutil.TestSingleTableQuery(t, []chunk.IndexQuery{{}}, table, 0, 10)

	// close the table to test reloading of table with already having files in the cache dir.
	table.Close()

	// change a boltdb file to text file which would fail to open.
	tablePathInCache := filepath.Join(cachePath, tableName)
	require.NoError(t, ioutil.WriteFile(filepath.Join(tablePathInCache, "0"), []byte("invalid boltdb file"), 0666))

	// verify that changed boltdb file can't be opened.
	_, err = local.OpenBoltdbFile(filepath.Join(tablePathInCache, "0"))
	require.Error(t, err)

	// add some more files to the storage.
	dbs = make(map[string]testutil.DBRecords)
	for i := 10; i < 20; i++ {
		dbs[fmt.Sprint(i)] = testutil.DBRecords{
			Start:      i,
			NumRecords: 1,
		}
	}

	testutil.SetupDBTablesAtPath(t, tableName, objectStoragePath, dbs, false)

	// try loading the table, it should skip loading corrupt file and reload it from storage.
	table, err = LoadTable(context.Background(), tableName, cachePath, storageClient, boltDBIndexClient, newMetrics(nil))
	require.NoError(t, err)
	require.NotNil(t, table)

	defer table.Close()

	// query the loaded table to see if it has right data.
	testutil.TestSingleTableQuery(t, []chunk.IndexQuery{{}}, table, 0, 20)
}
