package downloads

import (
	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	"github.com/grafana/loki/pkg/storage/stores/shipper/testutil"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"path/filepath"
	"testing"
	"time"
)

func buildTestTableManager(t *testing.T, path string) (*TableManager, *local.BoltIndexClient, stopFunc) {
	boltDBIndexClient, fsObjectClient := buildTestClients(t, path)
	cachePath := filepath.Join(path, cacheDirName)

	cfg := Config{
		CacheDir:     cachePath,
		SyncInterval: time.Hour,
		CacheTTL:     time.Hour,
	}
	tableManager, err := NewTableManager(cfg, boltDBIndexClient, fsObjectClient,nil)
	require.NoError(t, err)

	return tableManager, boltDBIndexClient, func() {
		tableManager.Stop()
		boltDBIndexClient.Stop()
	}
}

func TestTableManager_QueryPages(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "table-manager-query-pages")
	require.NoError(t, err)

	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)

	tables := map[string]map[string]testutil.DBRecords{
		"table1": {
			"db1": {
				Start: 0,
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
				Start: 30,
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
		testutil.SetupDBTablesAtPath(t, name, objectStoragePath, dbs)
	}

	tableManager, _, stopFunc := buildTestTableManager(t, tempDir)
	defer stopFunc()

	testutil.TestMultiTableQuery(t, queries, tableManager, 0, 60)
}
