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
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/local"
	"github.com/grafana/loki/pkg/storage/stores/shipper/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper/testutil"
)

const (
	indexDirName          = "index"
	objectsStorageDirName = "objects"
	userID                = "user-id"
)

func buildTestClients(t *testing.T, path string) (*local.BoltIndexClient, StorageClient) {
	indexPath := filepath.Join(path, indexDirName)

	boltDBIndexClient, err := local.NewBoltDBIndexClient(local.BoltDBConfig{Directory: indexPath})
	require.NoError(t, err)

	objectStoragePath := filepath.Join(path, objectsStorageDirName)
	fsObjectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStoragePath})
	require.NoError(t, err)

	return boltDBIndexClient, storage.NewIndexStorageClient(fsObjectClient, "")
}

type stopFunc func()

func buildTestTable(t *testing.T, path string, makePerTenantBuckets bool) (*Table, *local.BoltIndexClient, stopFunc) {
	boltDBIndexClient, fsObjectClient := buildTestClients(t, path)
	indexPath := filepath.Join(path, indexDirName)

	table, err := NewTable(indexPath, "test", fsObjectClient, boltDBIndexClient, makePerTenantBuckets)
	require.NoError(t, err)

	return table, boltDBIndexClient, func() {
		table.Stop()
		boltDBIndexClient.Stop()
	}
}

func TestLoadTable(t *testing.T) {
	indexPath := t.TempDir()

	boltDBIndexClient, err := local.NewBoltDBIndexClient(local.BoltDBConfig{Directory: indexPath})
	require.NoError(t, err)

	defer func() {
		boltDBIndexClient.Stop()
	}()

	// setup some dbs with default bucket and per tenant bucket for a table at a path.
	tablePath := filepath.Join(indexPath, "test-table")
	testutil.SetupDBsAtPath(t, tablePath, map[string]testutil.DBConfig{
		"db1": {
			DBRecords: testutil.DBRecords{
				Start:      0,
				NumRecords: 10,
			},
		},
		"db2": {
			DBRecords: testutil.DBRecords{
				Start:      10,
				NumRecords: 10,
			},
		},
	}, nil)
	testutil.SetupDBsAtPath(t, tablePath, map[string]testutil.DBConfig{
		"db3": {
			DBRecords: testutil.DBRecords{
				Start:      20,
				NumRecords: 10,
			},
		},
		"db4": {
			DBRecords: testutil.DBRecords{
				Start:      30,
				NumRecords: 10,
			},
		},
	}, []byte(userID))

	// change a boltdb file to text file which would fail to open.
	invalidFilePath := filepath.Join(tablePath, "invalid")
	require.NoError(t, ioutil.WriteFile(invalidFilePath, []byte("invalid boltdb file"), 0666))

	// verify that changed boltdb file can't be opened.
	_, err = local.OpenBoltdbFile(invalidFilePath)
	require.Error(t, err)

	// try loading the table.
	table, err := LoadTable(tablePath, "test", nil, boltDBIndexClient, false, newMetrics(nil))
	require.NoError(t, err)
	require.NotNil(t, table)

	defer func() {
		table.Stop()
	}()

	// verify that we still have 5 files(4 valid, 1 invalid)
	filesInfo, err := ioutil.ReadDir(tablePath)
	require.NoError(t, err)
	require.Len(t, filesInfo, 5)

	require.NoError(t, table.Snapshot())

	// query the loaded table to see if it has right data.
	testutil.TestSingleTableQuery(t, userID, []chunk.IndexQuery{{}}, table, 0, 40)
}

func TestTable_Write(t *testing.T) {
	for _, withPerTenantBucket := range []bool{false, true} {
		t.Run(fmt.Sprintf("withPerTenantBucket=%v", withPerTenantBucket), func(t *testing.T) {
			tempDir := t.TempDir()

			table, boltIndexClient, stopFunc := buildTestTable(t, tempDir, withPerTenantBucket)
			defer stopFunc()

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
					require.NoError(t, table.write(user.InjectOrgID(context.Background(), userID), tc.writeTime, batch.(*local.BoltWriteBatch).Writes["test"]))

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
					testutil.TestSingleTableQuery(t, userID, []chunk.IndexQuery{{}}, table, 0, (i+1)*10)
					bucketToQuery := local.IndexBucketName
					if withPerTenantBucket {
						bucketToQuery = []byte(userID)
					}
					testutil.TestSingleDBQuery(t, chunk.IndexQuery{}, db, bucketToQuery, boltIndexClient, i*10, 10)
				})
			}
		})
	}
}

func TestTable_Upload(t *testing.T) {
	for _, withPerTenantBucket := range []bool{false, true} {
		t.Run(fmt.Sprintf("withPerTenantBucket=%v", withPerTenantBucket), func(t *testing.T) {
			tempDir := t.TempDir()

			table, boltIndexClient, stopFunc := buildTestTable(t, tempDir, withPerTenantBucket)
			defer stopFunc()

			now := time.Now()

			// write a batch for now
			batch := boltIndexClient.NewWriteBatch()
			testutil.AddRecordsToBatch(batch, "test", 0, 10)
			require.NoError(t, table.write(user.InjectOrgID(context.Background(), userID), now, batch.(*local.BoltWriteBatch).Writes["test"]))

			// upload the table
			require.NoError(t, table.Upload(context.Background(), true))
			require.Len(t, table.dbs, 1)

			// compare the local dbs for the table with the dbs in remote storage after upload to ensure they have same data
			objectStorageDir := filepath.Join(tempDir, objectsStorageDirName)
			compareTableWithStorage(t, table, objectStorageDir)

			// write a batch to another shard
			batch = boltIndexClient.NewWriteBatch()
			testutil.AddRecordsToBatch(batch, "test", 20, 10)
			require.NoError(t, table.write(user.InjectOrgID(context.Background(), userID), now.Add(ShardDBsByDuration), batch.(*local.BoltWriteBatch).Writes["test"]))

			// upload the dbs to storage
			require.NoError(t, table.Upload(context.Background(), true))
			require.Len(t, table.dbs, 2)

			// check local dbs with remote dbs to ensure they have same data
			compareTableWithStorage(t, table, objectStorageDir)
		})
	}
}

func compareTableWithStorage(t *testing.T, table *Table, storageDir string) {
	// use a temp dir for decompressing the files before comparison.
	tempDir := t.TempDir()

	for name, db := range table.dbs {
		fileName := table.buildFileName(name)

		// open compressed file from storage
		compressedFile, err := os.Open(filepath.Join(storageDir, table.name, fileName))
		require.NoError(t, err)

		// get a compressed reader
		compressedReader, err := gzip.NewReader(compressedFile)
		require.NoError(t, err)

		// create a temp file for writing decompressed file
		decompressedFilePath := filepath.Join(tempDir, filepath.Base(fileName))
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
	testDir := t.TempDir()

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
	testutil.AddRecordsToDB(t, outsideRetention, boltDBIndexClient, 0, 10, nil)
	testutil.AddRecordsToDB(t, inRetention, boltDBIndexClient, 10, 10, nil)
	testutil.AddRecordsToDB(t, notUploaded, boltDBIndexClient, 20, 10, nil)

	// load existing dbs
	table, err := LoadTable(indexPath, "test", storageClient, boltDBIndexClient, false, newMetrics(nil))
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
	indexPath := t.TempDir()

	// setup some dbs with a snapshot file.
	tablePath := testutil.SetupDBsAtPath(t, filepath.Join(indexPath, "test-table"), map[string]testutil.DBConfig{
		"db1": {
			DBRecords: testutil.DBRecords{
				Start:      0,
				NumRecords: 10,
			},
		},
		"db1" + tempFileSuffix: { // a snapshot file which should be ignored.
			DBRecords: testutil.DBRecords{
				Start:      0,
				NumRecords: 10,
			},
		},
		"db2": {
			DBRecords: testutil.DBRecords{
				Start:      10,
				NumRecords: 10,
			},
		},
	}, nil)

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
	tempDir := t.TempDir()

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

	dbs := map[string]testutil.DBConfig{}
	for _, dbName := range dbNames {
		dbs[fmt.Sprint(dbName)] = testutil.DBConfig{
			DBRecords: testutil.DBRecords{
				NumRecords: 10,
			},
		}
	}

	// setup some dbs for a table at a path.
	tableName := "test-table"
	tablePath := testutil.SetupDBsAtPath(t, filepath.Join(indexPath, tableName), dbs, nil)

	table, err := LoadTable(tablePath, "test", storageClient, boltDBIndexClient, false, newMetrics(nil))
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
		require.FileExists(t, filepath.Join(objectStorageDir, tableName, table.buildFileName(fmt.Sprint(expectedDB))))
	}

	// force upload of dbs
	require.NoError(t, table.Upload(context.Background(), true))
	expectedDBsToUpload = dbNames

	// verify that all the dbs are uploaded
	uploadedDBs, err = ioutil.ReadDir(filepath.Join(objectStorageDir, table.name))
	require.NoError(t, err)

	require.Len(t, uploadedDBs, len(expectedDBsToUpload))
	for _, expectedDB := range expectedDBsToUpload {
		require.FileExists(t, filepath.Join(objectStorageDir, tableName, table.buildFileName(fmt.Sprint(expectedDB))))
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
		require.NoFileExists(t, filepath.Join(objectStorageDir, tableName, table.buildFileName(fmt.Sprint(expectedDB))))
	}
}

func TestTable_MultiQueries(t *testing.T) {
	indexPath := t.TempDir()

	boltDBIndexClient, err := local.NewBoltDBIndexClient(local.BoltDBConfig{Directory: indexPath})
	require.NoError(t, err)

	defer func() {
		boltDBIndexClient.Stop()
	}()

	user1, user2 := "user1", "user2"

	// setup some dbs with default bucket and per tenant bucket for a table at a path.
	tablePath := filepath.Join(indexPath, "test-table")
	testutil.SetupDBsAtPath(t, tablePath, map[string]testutil.DBConfig{
		"db1": {
			DBRecords: testutil.DBRecords{
				NumRecords: 10,
			},
		},
		"db2": {
			DBRecords: testutil.DBRecords{
				Start:      10,
				NumRecords: 10,
			},
		},
	}, nil)
	testutil.SetupDBsAtPath(t, tablePath, map[string]testutil.DBConfig{
		"db3": {
			DBRecords: testutil.DBRecords{
				Start:      20,
				NumRecords: 10,
			},
		},
		"db4": {
			DBRecords: testutil.DBRecords{
				Start:      30,
				NumRecords: 10,
			},
		},
	}, []byte(user1))

	// try loading the table.
	table, err := LoadTable(tablePath, "test", nil, boltDBIndexClient, false, newMetrics(nil))
	require.NoError(t, err)
	require.NotNil(t, table)

	defer func() {
		table.Stop()
	}()

	require.NoError(t, table.Snapshot())

	// build queries each looking for specific value from all the dbs
	var queries []chunk.IndexQuery
	for i := 5; i < 35; i++ {
		queries = append(queries, chunk.IndexQuery{ValueEqual: []byte(strconv.Itoa(i))})
	}

	// querying data for user1 should return both data from common index and user1's index
	testutil.TestSingleTableQuery(t, user1, queries, table, 5, 30)

	// querying data for user2 should return only common index
	testutil.TestSingleTableQuery(t, user2, queries, table, 5, 15)
}
