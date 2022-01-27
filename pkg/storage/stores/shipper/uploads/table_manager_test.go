package uploads

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/local"
	"github.com/grafana/loki/pkg/storage/stores/shipper/testutil"
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
	testDir := t.TempDir()

	boltDBIndexClient, storageClient := buildTestClients(t, testDir)
	indexPath := filepath.Join(testDir, indexDirName)

	defer func() {
		boltDBIndexClient.Stop()
	}()

	// add a legacy db which is outside of table specific folder
	testutil.AddRecordsToDB(t, filepath.Join(indexPath, "table0"), boltDBIndexClient, 0, 10, nil)

	// table1 with 2 dbs
	testutil.SetupDBsAtPath(t, filepath.Join(indexPath, "table1"), map[string]testutil.DBConfig{
		"db1": {
			DBRecords: testutil.DBRecords{
				Start:      10,
				NumRecords: 10,
			},
		},
		"db2": {
			DBRecords: testutil.DBRecords{
				Start:      20,
				NumRecords: 10,
			},
		},
	}, nil)

	// table2 with 2 dbs
	testutil.SetupDBsAtPath(t, filepath.Join(indexPath, "table2"), map[string]testutil.DBConfig{
		"db1": {
			DBRecords: testutil.DBRecords{
				Start:      30,
				NumRecords: 10,
			},
		},
		"db2": {
			DBRecords: testutil.DBRecords{
				Start:      40,
				NumRecords: 10,
			},
		},
	}, nil)

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
	defer tm.Stop()

	require.Len(t, tm.tables, len(expectedTables))

	stat, err := os.Stat(filepath.Join(indexPath, "table0", "table0"))
	require.NoError(t, err)
	require.True(t, !stat.IsDir())

	for tableName, expectedIndex := range expectedTables {
		testutil.TestSingleTableQuery(t, userID, []chunk.IndexQuery{{TableName: tableName}}, tm.tables[tableName], expectedIndex.start, expectedIndex.numRecords)
	}
}

func TestTableManager_BatchWrite(t *testing.T) {
	testDir := t.TempDir()

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

	require.Len(t, tm.tables, len(tc))

	for tableName, expectedIndex := range tc {
		require.NoError(t, tm.tables[tableName].Snapshot())
		testutil.TestSingleTableQuery(t, userID, []chunk.IndexQuery{{TableName: tableName}}, tm.tables[tableName], expectedIndex.start, expectedIndex.numRecords)
	}
}

func TestTableManager_QueryPages(t *testing.T) {
	testDir := t.TempDir()

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

	for _, table := range tm.tables {
		require.NoError(t, table.Snapshot())
	}

	testutil.TestMultiTableQuery(t, userID, queries, tm, 0, 30)
}
