package uploads

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/gzip"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/local"
	"github.com/grafana/loki/pkg/storage/stores/shipper/testutil"
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
	}, false)

	// change a boltdb file to text file which would fail to open.
	invalidFilePath := filepath.Join(tablePath, "invalid")
	require.NoError(t, ioutil.WriteFile(invalidFilePath, []byte("invalid boltdb file"), 0666))

	// verify that changed boltdb file can't be opened.
	_, err = local.OpenBoltdbFile(invalidFilePath)
	require.Error(t, err)

	// try loading the table.
	table, err := LoadTable(tablePath, "test", nil, boltDBIndexClient, newMetrics(nil))
	require.NoError(t, err)
	require.NotNil(t, table)

	defer func() {
		table.Stop()
	}()

	// verify that we still have 3 files(2 valid, 1 invalid)
	filesInfo, err := ioutil.ReadDir(tablePath)
	require.NoError(t, err)
	require.Len(t, filesInfo, 3)

	require.NoError(t, table.Snapshot())

	// query the loaded table to see if it has right data.
	testutil.TestSingleTableQuery(t, []chunk.IndexQuery{{}}, table, 0, 20)
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

	// allow modifying last 5 shards
	table.modifyShardsSince = now.Add(-5 * ShardDBsByDuration).Unix()

	// a couple of times for which we want to do writes to make the table create different shards
	testCases := []struct {
		writeTime time.Time
		dbName    string // set only when it is supposed to be written to a different name than usual
	}{
		{
			writeTime: now,
		},
		{
			writeTime: now.Add(-(ShardDBsByDuration + 5*time.Minute)),
		},
		{
			writeTime: now.Add(-(ShardDBsByDuration*3 + 3*time.Minute)),
		},
		{
			writeTime: now.Add(-6 * ShardDBsByDuration), // write with time older than table.modifyShardsSince
			dbName:    fmt.Sprint(table.modifyShardsSince),
		},
	}

	numFiles := 0

	// performing writes and checking whether the index gets written to right shard
	for i, tc := range testCases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			batch := boltIndexClient.NewWriteBatch()
			testutil.AddRecordsToBatch(batch, "test", i*10, 10)
			require.NoError(t, table.write(context.Background(), tc.writeTime, batch.(*local.BoltWriteBatch).Writes["test"]))

			numFiles++
			require.Equal(t, numFiles, len(table.dbs))

			expectedDBName := tc.dbName
			if expectedDBName == "" {
				expectedDBName = fmt.Sprint(tc.writeTime.Truncate(ShardDBsByDuration).Unix())
			}
			db, ok := table.dbs[expectedDBName]
			require.True(t, ok)

			require.NoError(t, table.Snapshot())

			// test that the table has current + previous records
			testutil.TestSingleTableQuery(t, []chunk.IndexQuery{{}}, table, 0, (i+1)*10)
			testutil.TestSingleDBQuery(t, chunk.IndexQuery{}, db, boltIndexClient, i*10, 10)
		})
	}
}

func TestTable_Upload(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "upload")
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
	require.NoError(t, table.Upload(context.Background(), true))
	require.Len(t, table.dbs, 1)

	// compare the local dbs for the table with the dbs in remote storage after upload to ensure they have same data
	objectStorageDir := filepath.Join(tempDir, objectsStorageDirName)
	compareTableWithStorage(t, table, objectStorageDir)

	// write a batch to another shard
	batch = boltIndexClient.NewWriteBatch()
	testutil.AddRecordsToBatch(batch, "test", 20, 10)
	require.NoError(t, table.write(context.Background(), now.Add(ShardDBsByDuration), batch.(*local.BoltWriteBatch).Writes["test"]))

	// upload the dbs to storage
	require.NoError(t, table.Upload(context.Background(), true))
	require.Len(t, table.dbs, 2)

	// check local dbs with remote dbs to ensure they have same data
	compareTableWithStorage(t, table, objectStorageDir)
}

func compareTableWithStorage(t *testing.T, table *Table, storageDir string) {
	// use a temp dir for decompressing the files before comparison.
	tempDir, err := ioutil.TempDir("", "compare-table")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	for name, db := range table.dbs {
		objectKey := table.buildObjectKey(name)

		// open compressed file from storage
		compressedFile, err := os.Open(filepath.Join(storageDir, objectKey))
		require.NoError(t, err)

		// get a compressed reader
		compressedReader, err := gzip.NewReader(compressedFile)
		require.NoError(t, err)

		// create a temp file for writing decompressed file
		decompressedFilePath := filepath.Join(tempDir, filepath.Base(objectKey))
		decompressedFile, err := os.Create(decompressedFilePath)
		require.NoError(t, err)

		// do the decompression
		_, err = io.Copy(decompressedFile, compressedReader)
		require.NoError(t, err)

		// close the references
		require.NoError(t, compressedFile.Close())
		require.NoError(t, decompressedFile.Close())

		storageDB, err := local.OpenBoltdbFile(decompressedFilePath)
		require.NoError(t, err)

		testutil.CompareDBs(t, db, storageDB)
		require.NoError(t, storageDB.Close())
		require.NoError(t, os.Remove(decompressedFilePath))
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

	dbRetainPeriod := time.Hour

	// dbs for various scenarios to test
	outsideRetention := filepath.Join(indexPath, "outside-retention")
	inRetention := filepath.Join(indexPath, "in-retention")
	notUploaded := filepath.Join(indexPath, "not-uploaded")

	// build all the test dbs except for notUploaded
	testutil.AddRecordsToDB(t, outsideRetention, boltDBIndexClient, 0, 10)
	testutil.AddRecordsToDB(t, inRetention, boltDBIndexClient, 10, 10)
	testutil.AddRecordsToDB(t, notUploaded, boltDBIndexClient, 20, 10)

	// load existing dbs
	table, err := LoadTable(indexPath, "test", storageClient, boltDBIndexClient, newMetrics(nil))
	require.NoError(t, err)
	require.Len(t, table.dbs, 3)

	defer func() {
		table.Stop()
	}()

	require.NoError(t, table.Snapshot())

	// no cleanup without upload
	require.Len(t, table.dbs, 3)
	require.Len(t, table.dbSnapshots, 3)

	// upload outsideRetention and inRetention dbs
	require.NoError(t, table.uploadDB(context.Background(), filepath.Base(outsideRetention), table.dbs[filepath.Base(outsideRetention)]))
	require.NoError(t, table.uploadDB(context.Background(), filepath.Base(inRetention), table.dbs[filepath.Base(inRetention)]))

	// change the upload time of outsideRetention to before dbRetainPeriod
	table.dbUploadTime[filepath.Base(outsideRetention)] = time.Now().Add(-dbRetainPeriod).Add(-time.Minute)

	// cleanup the dbs
	require.NoError(t, table.Cleanup(dbRetainPeriod))

	// there must be 2 dbs now, it should have cleaned up only outsideRetention
	require.Len(t, table.dbs, 2)
	require.Len(t, table.dbSnapshots, 2)

	expectedDBs := []string{
		inRetention,
		fmt.Sprint(inRetention, snapshotFileSuffix),
		notUploaded,
		fmt.Sprint(notUploaded, snapshotFileSuffix),
	}

	// verify open dbs with the table and actual db files in the index directory
	filesInfo, err := ioutil.ReadDir(indexPath)
	require.NoError(t, err)
	require.Len(t, filesInfo, len(expectedDBs))

	for _, expectedDB := range expectedDBs {
		if strings.HasSuffix(expectedDB, snapshotFileSuffix) {
			_, ok := table.dbSnapshots[strings.TrimSuffix(filepath.Base(expectedDB), snapshotFileSuffix)]
			require.True(t, ok)
		} else {
			_, ok := table.dbs[filepath.Base(expectedDB)]
			require.True(t, ok)
		}

		_, err := os.Stat(expectedDB)
		require.NoError(t, err)
	}
}

func Test_LoadBoltDBsFromDir(t *testing.T) {
	indexPath, err := ioutil.TempDir("", "load-dbs-from-dir")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(indexPath))
	}()

	// setup some dbs with a snapshot file.
	tablePath := testutil.SetupDBTablesAtPath(t, "test-table", indexPath, map[string]testutil.DBRecords{
		"db1": {
			Start:      0,
			NumRecords: 10,
		},
		"db1" + tempFileSuffix: { // a snapshot file which should be ignored.
			Start:      0,
			NumRecords: 10,
		},
		"db2": {
			Start:      10,
			NumRecords: 10,
		},
	}, false)

	// create a boltdb file without bucket which should get removed
	db, err := local.OpenBoltdbFile(filepath.Join(tablePath, "no-bucket"))
	require.NoError(t, err)
	require.NoError(t, db.Close())

	// try loading the dbs
	dbs, err := loadBoltDBsFromDir(tablePath, newMetrics(nil))
	require.NoError(t, err)

	// check that we have just 2 dbs
	require.Len(t, dbs, 2)
	require.NotNil(t, dbs["db1"])
	require.NotNil(t, dbs["db2"])

	// close all the open dbs
	for _, boltdb := range dbs {
		require.NoError(t, boltdb.Close())
	}

	filesInfo, err := ioutil.ReadDir(tablePath)
	require.NoError(t, err)
	require.Len(t, filesInfo, 2)
}

func TestTable_ImmutableUploads(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "immutable-uploads")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	boltDBIndexClient, storageClient := buildTestClients(t, tempDir)
	indexPath := filepath.Join(tempDir, indexDirName)

	defer func() {
		boltDBIndexClient.Stop()
	}()

	// shardCutoff is calculated based on when shards are considered to not be active anymore and are safe to be uploaded.
	shardCutoff := getOldestActiveShardTime()

	// some dbs to setup
	dbNames := []int64{
		shardCutoff.Add(-ShardDBsByDuration).Unix(),    // inactive shard, should upload
		shardCutoff.Add(-1 * time.Minute).Unix(),       // 1 minute before shard cutoff, should upload
		time.Now().Truncate(ShardDBsByDuration).Unix(), // active shard, should not upload
	}

	dbs := map[string]testutil.DBRecords{}
	for _, dbName := range dbNames {
		dbs[fmt.Sprint(dbName)] = testutil.DBRecords{
			NumRecords: 10,
		}
	}

	// setup some dbs for a table at a path.
	tablePath := testutil.SetupDBTablesAtPath(t, "test-table", indexPath, dbs, false)

	table, err := LoadTable(tablePath, "test", storageClient, boltDBIndexClient, newMetrics(nil))
	require.NoError(t, err)
	require.NotNil(t, table)

	defer func() {
		table.Stop()
	}()

	// db expected to be uploaded without forcing the upload
	expectedDBsToUpload := []int64{dbNames[0], dbNames[1]}

	// upload dbs without forcing the upload which should not upload active shard or shard which has been active upto a minute back.
	require.NoError(t, table.Upload(context.Background(), false))

	// verify that only expected dbs are uploaded
	objectStorageDir := filepath.Join(tempDir, objectsStorageDirName)
	uploadedDBs, err := ioutil.ReadDir(filepath.Join(objectStorageDir, table.name))
	require.NoError(t, err)

	require.Len(t, uploadedDBs, len(expectedDBsToUpload))
	for _, expectedDB := range expectedDBsToUpload {
		require.FileExists(t, filepath.Join(objectStorageDir, table.buildObjectKey(fmt.Sprint(expectedDB))))
	}

	// force upload of dbs
	require.NoError(t, table.Upload(context.Background(), true))
	expectedDBsToUpload = dbNames

	// verify that all the dbs are uploaded
	uploadedDBs, err = ioutil.ReadDir(filepath.Join(objectStorageDir, table.name))
	require.NoError(t, err)

	require.Len(t, uploadedDBs, len(expectedDBsToUpload))
	for _, expectedDB := range expectedDBsToUpload {
		require.FileExists(t, filepath.Join(objectStorageDir, table.buildObjectKey(fmt.Sprint(expectedDB))))
	}

	// delete everything uploaded
	dir, err := ioutil.ReadDir(filepath.Join(objectStorageDir, table.name))
	require.NoError(t, err)
	for _, d := range dir {
		require.NoError(t, os.RemoveAll(filepath.Join(objectStorageDir, table.name, d.Name())))
	}

	// force upload of dbs
	require.NoError(t, table.Upload(context.Background(), true))

	// make sure nothing was re-uploaded
	for _, expectedDB := range expectedDBsToUpload {
		require.NoFileExists(t, filepath.Join(objectStorageDir, table.buildObjectKey(fmt.Sprint(expectedDB))))
	}
}

func TestTable_MultiQueries(t *testing.T) {
	indexPath, err := ioutil.TempDir("", "table-multi-queries")
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
	}, false)

	// try loading the table.
	table, err := LoadTable(tablePath, "test", nil, boltDBIndexClient, newMetrics(nil))
	require.NoError(t, err)
	require.NotNil(t, table)

	defer func() {
		table.Stop()
	}()

	require.NoError(t, table.Snapshot())

	// build queries each looking for specific value from all the dbs
	var queries []chunk.IndexQuery
	for i := 5; i < 15; i++ {
		queries = append(queries, chunk.IndexQuery{ValueEqual: []byte(strconv.Itoa(i))})
	}

	// query the loaded table to see if it has right data.
	testutil.TestSingleTableQuery(t, queries, table, 5, 10)
}
