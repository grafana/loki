package downloads

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/util"
	"github.com/grafana/loki/pkg/storage/stores/shipper/testutil"
)

func buildTestTableManager(t *testing.T, path string) (*TableManager, stopFunc) {
	boltDBIndexClient, indexStorageClient := buildTestClients(t, path)
	cachePath := filepath.Join(path, cacheDirName)

	cfg := Config{
		CacheDir:     cachePath,
		SyncInterval: time.Hour,
		CacheTTL:     time.Hour,
	}
	tableManager, err := NewTableManager(cfg, boltDBIndexClient, indexStorageClient, nil)
	require.NoError(t, err)

	return tableManager, func() {
		tableManager.Stop()
		boltDBIndexClient.Stop()
	}
}

func TestTableManager_QueryPages(t *testing.T) {
	t.Run("QueryPages", func(t *testing.T) {
		tempDir := t.TempDir()
		objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)

		var queries []chunk.IndexQuery
		for i, name := range []string{"table1", "table2"} {
			testutil.SetupTable(t, filepath.Join(objectStoragePath, name), testutil.DBsConfig{
				NumUnCompactedDBs: 5,
				DBRecordsStart:    i * 1000,
			}, testutil.PerUserDBsConfig{
				DBsConfig: testutil.DBsConfig{
					NumUnCompactedDBs: 5,
					DBRecordsStart:    i*1000 + 500,
				},
				NumUsers: 1,
			})
			queries = append(queries, chunk.IndexQuery{TableName: name})
		}

		tableManager, stopFunc := buildTestTableManager(t, tempDir)
		defer stopFunc()

		testutil.TestMultiTableQuery(t, testutil.BuildUserID(0), queries, tableManager, 0, 2000)
	})

	t.Run("it doesn't deadlock when table create fails", func(t *testing.T) {
		tempDir := os.TempDir()

		// This file forces chunk_util.EnsureDirectory to fail. Any write error would cause this
		// deadlock
		f, err := os.CreateTemp(filepath.Join(tempDir, "cache"), "not-a-directory")
		require.NoError(t, err)
		badTable := filepath.Base(f.Name())

		tableManager, stopFunc := buildTestTableManager(t, tempDir)
		defer stopFunc()

		err = tableManager.query(context.Background(), badTable, nil, nil)
		require.Error(t, err)

		// This one deadlocks without the fix
		err = tableManager.query(context.Background(), badTable, nil, nil)
		require.Error(t, err)
	})
}

func TestTableManager_cleanupCache(t *testing.T) {
	tempDir := t.TempDir()

	tableManager, stopFunc := buildTestTableManager(t, tempDir)
	defer stopFunc()

	// one table that would expire and other one won't
	expiredTableName := "expired-table"
	nonExpiredTableName := "non-expired-table"

	// query for above 2 tables which should set them up in table manager
	err := tableManager.QueryPages(user.InjectOrgID(context.Background(), "fake"), []chunk.IndexQuery{
		{TableName: expiredTableName},
		{TableName: nonExpiredTableName},
	}, func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
		return true
	})

	require.NoError(t, err)
	// table manager should now have 2 tables.
	require.Len(t, tableManager.tables, 2)

	// call cleanupCache and verify that no tables are cleaned up because they are not yet expired.
	require.NoError(t, tableManager.cleanupCache())
	require.Len(t, tableManager.tables, 2)

	// change the last used at time of expiredTable to before the ttl.
	expiredTable, ok := tableManager.tables[expiredTableName]
	require.True(t, ok)
	for _, idxSet := range expiredTable.indexSets {
		idxSet.(*indexSet).lastUsedAt = time.Now().Add(-(tableManager.cfg.CacheTTL + time.Minute))
	}

	// call the cleanupCache and verify that we still have nonExpiredTable and expiredTable is gone.
	require.NoError(t, tableManager.cleanupCache())
	require.Len(t, tableManager.tables, 1)

	_, ok = tableManager.tables[expiredTableName]
	require.False(t, ok)

	_, ok = tableManager.tables[nonExpiredTableName]
	require.True(t, ok)
}

func TestTableManager_ensureQueryReadiness(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		queryReadyNumDaysCfg int
	}{
		{
			name: "0 queryReadyNumDaysCfg with 10 tables in storage",
		},
		{
			name:                 "5 queryReadyNumDaysCfg with 10 tables in storage",
			queryReadyNumDaysCfg: 5,
		},
		{
			name:                 "20 queryReadyNumDaysCfg with 10 tables in storage",
			queryReadyNumDaysCfg: 20,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()

			objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)

			tables := map[string]map[string]testutil.DBConfig{}
			activeTableNumber := getActiveTableNumber()
			for i := 0; i < 10; i++ {
				tables[fmt.Sprintf("table_%d", activeTableNumber-int64(i))] = map[string]testutil.DBConfig{
					"db": {
						CompressFile: i%2 == 0,
						DBRecords: testutil.DBRecords{
							Start:      i * 10,
							NumRecords: 10,
						},
					},
				}
			}

			for name, dbs := range tables {
				testutil.SetupDBsAtPath(t, filepath.Join(objectStoragePath, name), dbs, nil)
			}

			boltDBIndexClient, indexStorageClient := buildTestClients(t, tempDir)
			cachePath := filepath.Join(tempDir, cacheDirName)
			require.NoError(t, util.EnsureDirectory(cachePath))

			cfg := Config{
				CacheDir:          cachePath,
				SyncInterval:      time.Hour,
				CacheTTL:          time.Hour,
				QueryReadyNumDays: tc.queryReadyNumDaysCfg,
			}
			tableManager := &TableManager{
				cfg:                cfg,
				boltIndexClient:    boltDBIndexClient,
				indexStorageClient: indexStorageClient,
				tables:             make(map[string]*Table),
				metrics:            newMetrics(nil),
				ctx:                context.Background(),
				cancel:             func() {},
			}

			defer func() {
				tableManager.Stop()
				boltDBIndexClient.Stop()
			}()

			require.NoError(t, tableManager.ensureQueryReadiness())

			if tc.queryReadyNumDaysCfg == 0 {
				require.Len(t, tableManager.tables, 0)
			} else {
				require.Len(t, tableManager.tables, int(math.Min(float64(tc.queryReadyNumDaysCfg+1), 10)))
			}
		})
	}
}

func TestTableManager_tablesRequiredForQueryReadiness(t *testing.T) {
	numDailyTablesInStorage := 10
	var tablesInStorage []string
	// tables with daily table number
	activeDailyTableNumber := getActiveTableNumber()
	for i := 0; i < numDailyTablesInStorage; i++ {
		tablesInStorage = append(tablesInStorage, fmt.Sprintf("table_%d", activeDailyTableNumber-int64(i)))
	}

	// tables with weekly table number
	activeWeeklyTableNumber := time.Now().Unix() / int64((durationDay*7)/time.Second)
	for i := 0; i < 10; i++ {
		tablesInStorage = append(tablesInStorage, fmt.Sprintf("table_%d", activeWeeklyTableNumber-int64(i)))
	}

	// tables without a table number
	tablesInStorage = append(tablesInStorage, "foo", "bar")

	for i, tc := range []int{
		0, 5, 10, 20,
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			tableManager := &TableManager{
				cfg: Config{
					QueryReadyNumDays: tc,
				},
			}

			tablesNames, err := tableManager.tablesRequiredForQueryReadiness(tablesInStorage)
			require.NoError(t, err)

			numExpectedTables := 0
			if tc != 0 {
				numExpectedTables = int(math.Min(float64(tc+1), float64(numDailyTablesInStorage)))
			}

			for i := 0; i < numExpectedTables; i++ {
				require.Equal(t, fmt.Sprintf("table_%d", activeDailyTableNumber-int64(i)), tablesNames[i])
			}
		})
	}
}
