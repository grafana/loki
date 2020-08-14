package uploads

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	"github.com/grafana/loki/pkg/storage/stores/shipper/testutil"
	"github.com/stretchr/testify/require"
)

func buildTestTableManager(t *testing.T, testDir string) (*TableManager, *local.BoltIndexClient, stopFunc) {
	defer func() {
		require.NoError(t, os.RemoveAll(testDir))
	}()

	boltDBIndexClient, storageClient := buildTestClients(t, testDir)
	indexPath := filepath.Join(testDir, indexDirName)

	cfg := Config{
		Uploader:       "test-table-manager",
		IndexDir:       indexPath,
		UploadInterval: time.Hour,
	}
	tm, err := NewTableManager(cfg, boltDBIndexClient, storageClient, nil)
	require.NoError(t, err)

	return tm, boltDBIndexClient, func() {
		tm.Stop()
		boltDBIndexClient.Stop()
	}
}

func TestLoadTables(t *testing.T) {
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

	// add a legacy db which is outside of table specific folder
	testutil.AddRecordsToDB(t, filepath.Join(indexPath, "table0"), boltDBIndexClient, 0, 10)

	// table1 with 2 dbs
	testutil.SetupDBTablesAtPath(t, "table1", indexPath, map[string]testutil.DBRecords{
		"db1": testutil.DBRecords{
			Start:      10,
			NumRecords: 10,
		},
		"db2": testutil.DBRecords{
			Start:      20,
			NumRecords: 10,
		},
	}, false)

	// table2 with 2 dbs
	testutil.SetupDBTablesAtPath(t, "table2", indexPath, map[string]testutil.DBRecords{
		"db1": testutil.DBRecords{
			Start:      30,
			NumRecords: 10,
		},
		"db2": testutil.DBRecords{
			Start:      40,
			NumRecords: 10,
		},
	}, false)

	expectedTables := map[string]struct {
		start, numRecords int
	}{
		"table0": {start: 0, numRecords: 10},
		"table1": {start: 10, numRecords: 20},
		"table2": {start: 30, numRecords: 20},
	}

	cfg := Config{
		Uploader:       "test-table-manager",
		IndexDir:       indexPath,
		UploadInterval: time.Hour,
	}

	tm, err := NewTableManager(cfg, boltDBIndexClient, storageClient, nil)
	require.NoError(t, err)
	require.Len(t, tm.tables, len(expectedTables))

	stat, err := os.Stat(filepath.Join(indexPath, "table0", "table0"))
	require.NoError(t, err)
	require.True(t, !stat.IsDir())

	for tableName, expectedIndex := range expectedTables {
		testutil.TestSingleQuery(t, chunk.IndexQuery{TableName: tableName}, tm.tables[tableName], expectedIndex.start, expectedIndex.numRecords)
	}
}

func TestTableManager_BatchWrite(t *testing.T) {
	testDir, err := ioutil.TempDir("", "batch-write")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(testDir))
	}()

	tm, boltIndexClient, stopFunc := buildTestTableManager(t, testDir)
	defer func() {
		stopFunc()
	}()

	tc := map[string]struct {
		start, numRecords int
	}{
		"table0": {start: 0, numRecords: 10},
		"table1": {start: 10, numRecords: 10},
		"table2": {start: 20, numRecords: 10},
	}

	writeBatch := boltIndexClient.NewWriteBatch()
	for tableName, records := range tc {
		testutil.AddRecordsToBatch(writeBatch, tableName, records.start, records.numRecords)
	}

	require.NoError(t, tm.BatchWrite(context.Background(), writeBatch))

	require.NoError(t, err)
	require.Len(t, tm.tables, len(tc))

	for tableName, expectedIndex := range tc {
		testutil.TestSingleQuery(t, chunk.IndexQuery{TableName: tableName}, tm.tables[tableName], expectedIndex.start, expectedIndex.numRecords)
	}
}

func TestTableManager_QueryPages(t *testing.T) {
	testDir, err := ioutil.TempDir("", "query-pages")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(testDir))
	}()

	tm, boltIndexClient, stopFunc := buildTestTableManager(t, testDir)
	defer func() {
		stopFunc()
	}()

	tc := map[string]struct {
		start, numRecords int
	}{
		"table0": {start: 0, numRecords: 10},
		"table1": {start: 10, numRecords: 10},
		"table2": {start: 20, numRecords: 10},
	}

	var queries []chunk.IndexQuery
	writeBatch := boltIndexClient.NewWriteBatch()
	for tableName, records := range tc {
		testutil.AddRecordsToBatch(writeBatch, tableName, records.start, records.numRecords)
		queries = append(queries, chunk.IndexQuery{TableName: tableName})
	}

	queries = append(queries, chunk.IndexQuery{TableName: "non-existent"})

	require.NoError(t, tm.BatchWrite(context.Background(), writeBatch))

	testutil.TestMultiTableQuery(t, queries, tm, 0, 30)
}
