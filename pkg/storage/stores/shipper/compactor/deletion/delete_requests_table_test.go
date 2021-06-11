package deletion

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/local"

	"github.com/grafana/loki/pkg/storage/stores/shipper/testutil"
	"github.com/grafana/loki/pkg/storage/stores/shipper/util"
)

func TestDeleteRequestsTable(t *testing.T) {
	// build test table
	tempDir, err := ioutil.TempDir("", "test-delete-requests-table")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	workingDir := filepath.Join(tempDir, "working-dir")
	objectStorePath := filepath.Join(tempDir, "object-store")

	objectClient, err := local.NewFSObjectClient(local.FSConfig{
		Directory: objectStorePath,
	})
	require.NoError(t, err)
	indexClient, err := newDeleteRequestsTable(workingDir, objectClient)
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
	testutil.TestSingleDBQuery(t, chunk.IndexQuery{}, testDeleteRequestsTable.db, testDeleteRequestsTable.boltdbIndexClient, 0, 10)

	// upload the file to the storage
	require.NoError(t, testDeleteRequestsTable.uploadFile())
	storageFilePath := filepath.Join(objectStorePath, objectPathInStorage)
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
	indexClient, err = newDeleteRequestsTable(workingDir, objectClient)
	require.NoError(t, err)
	defer indexClient.Stop()

	testDeleteRequestsTable = indexClient.(*deleteRequestsTable)
	require.NotEmpty(t, testDeleteRequestsTable.dbPath)

	// validate records in local db
	testutil.TestSingleDBQuery(t, chunk.IndexQuery{}, testDeleteRequestsTable.db, testDeleteRequestsTable.boltdbIndexClient, 0, 20)
}

func checkRecordsInStorage(t *testing.T, storageFilePath string, start, numRecords int) {
	tempDir, err := ioutil.TempDir("", "compare-delete-requests-db")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()
	tempFilePath := filepath.Join(tempDir, DeleteRequestsTableName)
	require.NoError(t, err)
	testutil.DecompressFile(t, storageFilePath, tempFilePath)

	tempDB, err := util.SafeOpenBoltdbFile(tempFilePath)
	require.NoError(t, err)

	defer func() {
		require.NoError(t, tempDB.Close())
	}()

	boltdbIndexClient, err := local.NewBoltDBIndexClient(local.BoltDBConfig{Directory: tempDir})
	require.NoError(t, err)

	defer boltdbIndexClient.Stop()

	testutil.TestSingleDBQuery(t, chunk.IndexQuery{}, tempDB, boltdbIndexClient, start, numRecords)
}
