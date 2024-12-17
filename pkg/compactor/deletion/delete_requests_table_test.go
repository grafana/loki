package deletion

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/testutil"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/util"
)

func TestDeleteRequestsTable(t *testing.T) {
	// build test table
	tempDir := t.TempDir()

	workingDir := filepath.Join(tempDir, "working-dir")
	objectStorePath := filepath.Join(tempDir, "object-store")

	objectClient, err := local.NewFSObjectClient(local.FSConfig{
		Directory: objectStorePath,
	})
	require.NoError(t, err)
	indexClient, err := newDeleteRequestsTable(workingDir, storage.NewIndexStorageClient(objectClient, ""))
	require.NoError(t, err)

	// see if delete requests db was created
	testDeleteRequestsTable := indexClient.(*deleteRequestsTable)
	require.NotEmpty(t, testDeleteRequestsTable.dbPath)
	require.FileExists(t, testDeleteRequestsTable.dbPath)

	// add some records to the db
	batch := testDeleteRequestsTable.NewWriteBatch()
	testutil.AddRecordsToBatch(batch, DeleteRequestsTableName, 0, 10)
	require.NoError(t, testDeleteRequestsTable.BatchWrite(context.Background(), batch))

	// see if right records were written
	testutil.VerifySingleIndexFile(t, index.Query{}, testDeleteRequestsTable.db, local.IndexBucketName, 0, 10)

	// upload the file to the storage
	require.NoError(t, testDeleteRequestsTable.uploadFile())
	storageFilePath := filepath.Join(objectStorePath, DeleteRequestsTableName+"/"+DeleteRequestsTableName+".gz")
	require.FileExists(t, storageFilePath)

	// validate records in the storage db
	checkRecordsInStorage(t, storageFilePath, 0, 10)

	// add more records to the db
	testutil.AddRecordsToBatch(batch, DeleteRequestsTableName, 10, 10)
	require.NoError(t, testDeleteRequestsTable.BatchWrite(context.Background(), batch))

	// stop the table which should upload the db to storage
	testDeleteRequestsTable.Stop()

	// see if the storage db got the new records
	checkRecordsInStorage(t, storageFilePath, 0, 20)

	// remove local db
	require.NoError(t, os.Remove(testDeleteRequestsTable.dbPath))
	require.NoError(t, err)

	// re-create table to see if the db gets downloaded locally since it does not exist anymore
	indexClient, err = newDeleteRequestsTable(workingDir, storage.NewIndexStorageClient(objectClient, ""))
	require.NoError(t, err)
	defer indexClient.Stop()

	testDeleteRequestsTable = indexClient.(*deleteRequestsTable)
	require.NotEmpty(t, testDeleteRequestsTable.dbPath)

	// validate records in local db
	testutil.VerifySingleIndexFile(t, index.Query{}, testDeleteRequestsTable.db, local.IndexBucketName, 0, 20)
}

func checkRecordsInStorage(t *testing.T, storageFilePath string, start, numRecords int) {
	tempDir := t.TempDir()
	tempFilePath := filepath.Join(tempDir, DeleteRequestsTableName)
	testutil.DecompressFile(t, storageFilePath, tempFilePath)

	tempDB, err := util.SafeOpenBoltdbFile(tempFilePath)
	require.NoError(t, err)

	defer func() {
		require.NoError(t, tempDB.Close())
	}()

	boltdbIndexClient, err := local.NewBoltDBIndexClient(local.BoltDBConfig{Directory: tempDir})
	require.NoError(t, err)

	defer boltdbIndexClient.Stop()

	testutil.VerifySingleIndexFile(t, index.Query{}, tempDB, local.IndexBucketName, start, numRecords)
}
