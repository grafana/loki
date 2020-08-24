package downloads

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

func buildTestTableManager(t *testing.T, path string) (*TableManager, *local.BoltIndexClient, stopFunc) {
	boltDBIndexClient, fsObjectClient := buildTestClients(t, path)
	cachePath := filepath.Join(path, cacheDirName)

	cfg := Config{
		CacheDir:     cachePath,
		SyncInterval: time.Hour,
		CacheTTL:     time.Hour,
	}
	tableManager, err := NewTableManager(cfg, boltDBIndexClient, fsObjectClient, nil)
	require.NoError(t, err)

	return tableManager, boltDBIndexClient, func() {
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

	tableManager, _, stopFunc := buildTestTableManager(t, tempDir)
	defer stopFunc()

	testutil.TestMultiTableQuery(t, queries, tableManager, 0, 60)
}

func TestTableManager_cleanupCache(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "table-manager-cleanup-cache")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	tableManager, _, stopFunc := buildTestTableManager(t, tempDir)
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
