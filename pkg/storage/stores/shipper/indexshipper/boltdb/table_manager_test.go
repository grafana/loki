package boltdb

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
	shipperindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/testutil"
)

const indexTablePeriod = 24 * time.Hour

func buildTestTableManager(t *testing.T, testDir string) (*TableManager, stopFunc) {
	defer func() {
		require.NoError(t, os.RemoveAll(testDir))
	}()

	mockIndexShipper := newMockIndexShipper()
	indexPath := filepath.Join(testDir, indexDirName)

	cfg := Config{
		Uploader: "test-table-manager",
		IndexDir: indexPath,
	}

	tableRange := config.TableRange{
		End: math.MaxInt64,
		PeriodConfig: &config.PeriodConfig{IndexTables: config.IndexPeriodicTableConfig{
			PeriodicTableConfig: config.PeriodicTableConfig{
				Prefix: "index_",
				Period: indexTablePeriod,
			}}},
	}
	tm, err := NewTableManager(cfg, mockIndexShipper, tableRange, nil, log.NewNopLogger())
	require.NoError(t, err)

	return tm, tm.Stop
}

func TestLoadTables(t *testing.T) {
	testDir := t.TempDir()

	mockIndexShipper := newMockIndexShipper()
	indexPath := filepath.Join(testDir, indexDirName)
	require.NoError(t, util.EnsureDirectory(indexPath))

	testutil.AddRecordsToDB(t, filepath.Join(indexPath, "table0"), 0, 10, nil)
	// add a legacy db which is outside of table specific folder
	testutil.AddRecordsToDB(t, filepath.Join(indexPath, "table1"), 0, 10, nil)

	// table2 with 2 dbs
	testutil.SetupDBsAtPath(t, filepath.Join(indexPath, "table2"), map[string]testutil.DBConfig{
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

	// table3 with 2 dbs
	testutil.SetupDBsAtPath(t, filepath.Join(indexPath, "table3"), map[string]testutil.DBConfig{
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

	// table4 with 2 dbs
	testutil.SetupDBsAtPath(t, filepath.Join(indexPath, "table4"), map[string]testutil.DBConfig{
		"db1": {
			DBRecords: testutil.DBRecords{
				Start:      50,
				NumRecords: 10,
			},
		},
		"db2": {
			DBRecords: testutil.DBRecords{
				Start:      60,
				NumRecords: 10,
			},
		},
	}, nil)

	expectedTables := map[string]struct {
		start, numRecords int
	}{
		"table1": {start: 0, numRecords: 10},
		"table2": {start: 10, numRecords: 20},
		"table3": {start: 30, numRecords: 20},
	}

	cfg := Config{
		Uploader: "test-table-manager",
		IndexDir: indexPath,
	}

	tableRange := config.TableRange{
		Start: 1,
		End:   3,
		PeriodConfig: &config.PeriodConfig{IndexTables: config.IndexPeriodicTableConfig{
			PeriodicTableConfig: config.PeriodicTableConfig{
				Prefix: "table",
				Period: indexTablePeriod,
			}}},
	}
	tm, err := NewTableManager(cfg, mockIndexShipper, tableRange, nil, log.NewNopLogger())
	require.NoError(t, err)
	defer tm.Stop()

	require.Len(t, tm.tables, len(expectedTables))

	stat, err := os.Stat(filepath.Join(indexPath, "table1", "table1"))
	require.NoError(t, err)
	require.True(t, !stat.IsDir())

	for tableName, expectedIndex := range expectedTables {
		// loaded tables should not have any index files, it should have handed them over to index shipper
		testutil.VerifyIndexes(t, userID, []index.Query{{TableName: tableName}},
			func(ctx context.Context, _ string, callback func(_ *bbolt.DB) error) error {
				return tm.tables[tableName].ForEach(ctx, callback)
			},
			0, 0)

		// see if index shipper has the index files
		testutil.VerifyIndexes(t, userID, []index.Query{{TableName: tableName}},
			func(ctx context.Context, table string, callback func(b *bbolt.DB) error) error {
				return tm.indexShipper.ForEach(ctx, table, userID, func(_ bool, index shipperindex.Index) error {
					return callback(index.(*IndexFile).GetBoltDB())
				})
			},
			expectedIndex.start, expectedIndex.numRecords)
	}
}

func TestTableManager_BatchWrite(t *testing.T) {
	testDir := t.TempDir()

	tm, stopFunc := buildTestTableManager(t, testDir)
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

	writeBatch := local.NewWriteBatch()
	for tableName, records := range tc {
		testutil.AddRecordsToBatch(writeBatch, tableName, records.start, records.numRecords)
	}

	require.NoError(t, tm.BatchWrite(context.Background(), writeBatch))

	require.Len(t, tm.tables, len(tc))

	for tableName, expectedIndex := range tc {
		require.NoError(t, tm.tables[tableName].Snapshot())
		testutil.VerifyIndexes(t, userID, []index.Query{{TableName: tableName}},
			func(_ context.Context, _ string, callback func(_ *bbolt.DB) error) error {
				return tm.tables[tableName].ForEach(context.Background(), callback)
			},
			expectedIndex.start, expectedIndex.numRecords)
	}
}

func TestTableManager_ForEach(t *testing.T) {
	testDir := t.TempDir()

	tm, stopFunc := buildTestTableManager(t, testDir)
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

	var queries []index.Query
	writeBatch := local.NewWriteBatch()
	for tableName, records := range tc {
		testutil.AddRecordsToBatch(writeBatch, tableName, records.start, records.numRecords)
		queries = append(queries, index.Query{TableName: tableName})
	}

	queries = append(queries, index.Query{TableName: "non-existent"})

	require.NoError(t, tm.BatchWrite(context.Background(), writeBatch))

	for _, table := range tm.tables {
		require.NoError(t, table.Snapshot())
	}

	testutil.VerifyIndexes(t, userID, queries,
		func(ctx context.Context, table string, callback func(b *bbolt.DB) error) error {
			return tm.ForEach(ctx, table, callback)
		},
		0, 30)

}
