package downloads

import (
	"context"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
	tempDir, err := ioutil.TempDir("", "table-manager-query-pages")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)

	tables := map[string]map[string]testutil.DBRecords{
		"table1": {
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
		},
		"table2": {
			"db1": {
				Start:      30,
				NumRecords: 10,
			},
			"db2": {
				Start:      40,
				NumRecords: 10,
			},
			"db3": {
				Start:      50,
				NumRecords: 10,
			},
		},
	}

	var queries []chunk.IndexQuery
	for name, dbs := range tables {
		queries = append(queries, chunk.IndexQuery{TableName: name})
		testutil.SetupDBTablesAtPath(t, name, objectStoragePath, dbs, true)
	}

	tableManager, stopFunc := buildTestTableManager(t, tempDir)
	defer stopFunc()

	testutil.TestMultiTableQuery(t, queries, tableManager, 0, 60)
}

func TestTableManager_cleanupCache(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "table-manager-cleanup-cache")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	tableManager, stopFunc := buildTestTableManager(t, tempDir)
	defer stopFunc()

	// one table that would expire and other one won't
	expiredTableName := "expired-table"
	nonExpiredTableName := "non-expired-table"

	// query for above 2 tables which should set them up in table manager
	err = tableManager.QueryPages(context.Background(), []chunk.IndexQuery{
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
	expiredTable.lastUsedAt = time.Now().Add(-(tableManager.cfg.CacheTTL + time.Minute))

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
			tempDir, err := ioutil.TempDir("", "table-manager-ensure-query-readiness")
			require.NoError(t, err)

			defer func() {
				require.NoError(t, os.RemoveAll(tempDir))
			}()

			objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)

			tables := map[string]map[string]testutil.DBRecords{}
			activeTableNumber := getActiveTableNumber()
			for i := 0; i < 10; i++ {
				tables[fmt.Sprintf("table_%d", activeTableNumber-int64(i))] = map[string]testutil.DBRecords{
					"db": {
						Start:      i * 10,
						NumRecords: 10,
					},
				}
			}

			for name, dbs := range tables {
				testutil.SetupDBTablesAtPath(t, name, objectStoragePath, dbs, true)
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
