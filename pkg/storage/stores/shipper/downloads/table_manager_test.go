package downloads

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper/testutil"
	"github.com/grafana/loki/pkg/validation"
)

func buildTestTableManager(t *testing.T, path string) (*TableManager, stopFunc) {
	boltDBIndexClient, indexStorageClient := buildTestClients(t, path)
	cachePath := filepath.Join(path, cacheDirName)

	cfg := Config{
		CacheDir:     cachePath,
		SyncInterval: time.Hour,
		CacheTTL:     time.Hour,
		Limits:       &mockLimits{},
	}
	tableManager, err := NewTableManager(cfg, boltDBIndexClient, indexStorageClient, nil, nil)
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

		var queries []index.Query
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
			queries = append(queries, index.Query{TableName: name})
		}

		tableManager, stopFunc := buildTestTableManager(t, tempDir)
		defer stopFunc()

		testutil.TestMultiTableQuery(t, testutil.BuildUserID(0), queries, tableManager, 0, 2000)
	})

	t.Run("it doesn't deadlock when table create fails", func(t *testing.T) {
		tempDir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(tempDir, "cache"), 0o777))

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

	tableManager.tables[expiredTableName] = &mockTable{}
	tableManager.tables[nonExpiredTableName] = &mockTable{}

	// call cleanupCache and verify that no tables are cleaned up because they are not yet expired.
	require.NoError(t, tableManager.cleanupCache())
	require.Len(t, tableManager.tables, 2)

	// set the flag for expiredTable to expire.
	tableManager.tables[expiredTableName].(*mockTable).tableExpired = true

	// call the cleanupCache and verify that we still have nonExpiredTable and expiredTable is gone.
	require.NoError(t, tableManager.cleanupCache())
	require.Len(t, tableManager.tables, 1)

	_, ok := tableManager.tables[expiredTableName]
	require.False(t, ok)

	_, ok = tableManager.tables[nonExpiredTableName]
	require.True(t, ok)
}

func TestTableManager_ensureQueryReadiness(t *testing.T) {
	activeTableNumber := getActiveTableNumber()
	mockIndexStorageClient := &mockIndexStorageClient{
		userIndexesInTables: map[string][]string{},
	}

	cfg := Config{
		SyncInterval: time.Hour,
		CacheTTL:     time.Hour,
	}

	tableManager := &TableManager{
		cfg:                cfg,
		indexStorageClient: mockIndexStorageClient,
		tables:             make(map[string]Table),
		metrics:            newMetrics(nil),
		ctx:                context.Background(),
		cancel:             func() {},
	}

	buildTableName := func(idx int) string {
		return fmt.Sprintf("table_%d", activeTableNumber-int64(idx))
	}

	// setup 10 tables with 5 latest tables having user index for user1 and user2
	for i := 0; i < 10; i++ {
		tableName := buildTableName(i)
		tableManager.tables[tableName] = &mockTable{}
		mockIndexStorageClient.tablesInStorage = append(mockIndexStorageClient.tablesInStorage, tableName)
		if i < 5 {
			mockIndexStorageClient.userIndexesInTables[tableName] = []string{"user1", "user2"}
		}
	}

	// function for resetting state of mockTables
	resetTables := func() {
		for _, table := range tableManager.tables {
			table.(*mockTable).queryReadinessDoneForUsers = nil
		}
	}

	for _, tc := range []struct {
		name                 string
		queryReadyNumDaysCfg int
		queryReadinessLimits mockLimits

		expectedQueryReadinessDoneForUsers map[string][]string
	}{
		{
			name:                 "no query readiness configured",
			queryReadinessLimits: mockLimits{},
		},
		{
			name:                 "common index: 5 days",
			queryReadyNumDaysCfg: 5,
			expectedQueryReadinessDoneForUsers: map[string][]string{
				buildTableName(0): {},
				buildTableName(1): {},
				buildTableName(2): {},
				buildTableName(3): {},
				buildTableName(4): {},
				buildTableName(5): {}, // NOTE: we include an extra table since we are counting days back from current point in time
			},
		},
		{
			name:                 "common index: 20 days",
			queryReadyNumDaysCfg: 20,
			expectedQueryReadinessDoneForUsers: map[string][]string{
				buildTableName(0): {},
				buildTableName(1): {},
				buildTableName(2): {},
				buildTableName(3): {},
				buildTableName(4): {},
				buildTableName(5): {},
				buildTableName(6): {},
				buildTableName(7): {},
				buildTableName(8): {},
				buildTableName(9): {},
			},
		},
		{
			name: "user index default: 2 days",
			queryReadinessLimits: mockLimits{
				queryReadyIndexNumDaysDefault: 2,
			},
			expectedQueryReadinessDoneForUsers: map[string][]string{
				buildTableName(0): {"user1", "user2"},
				buildTableName(1): {"user1", "user2"},
				buildTableName(2): {"user1", "user2"},
			},
		},
		{
			name: "common index: 5 days, user index default: 2 days",
			queryReadinessLimits: mockLimits{
				queryReadyIndexNumDaysDefault: 2,
			},
			queryReadyNumDaysCfg: 5,
			expectedQueryReadinessDoneForUsers: map[string][]string{
				buildTableName(0): {"user1", "user2"},
				buildTableName(1): {"user1", "user2"},
				buildTableName(2): {"user1", "user2"},
				buildTableName(3): {},
				buildTableName(4): {},
				buildTableName(5): {},
			},
		},
		{
			name: "user1: 2 days",
			queryReadinessLimits: mockLimits{
				queryReadyIndexNumDaysByUser: map[string]int{"user1": 2},
			},
			expectedQueryReadinessDoneForUsers: map[string][]string{
				buildTableName(0): {"user1"},
				buildTableName(1): {"user1"},
				buildTableName(2): {"user1"},
			},
		},
		{
			name: "user1: 2 days, user2: 20 days",
			queryReadinessLimits: mockLimits{
				queryReadyIndexNumDaysByUser: map[string]int{"user1": 2, "user2": 20},
			},
			expectedQueryReadinessDoneForUsers: map[string][]string{
				buildTableName(0): {"user1", "user2"},
				buildTableName(1): {"user1", "user2"},
				buildTableName(2): {"user1", "user2"},
				buildTableName(3): {"user2"},
				buildTableName(4): {"user2"},
			},
		},
		{
			name: "user index default: 3 days, user1: 2 days",
			queryReadinessLimits: mockLimits{
				queryReadyIndexNumDaysDefault: 3,
				queryReadyIndexNumDaysByUser:  map[string]int{"user1": 2},
			},
			expectedQueryReadinessDoneForUsers: map[string][]string{
				buildTableName(0): {"user1", "user2"},
				buildTableName(1): {"user1", "user2"},
				buildTableName(2): {"user1", "user2"},
				buildTableName(3): {"user2"},
			},
		},
	} {
		tcCopy := tc
		t.Run(tc.name, func(t *testing.T) {
			resetTables()
			tableManager.cfg.QueryReadyNumDays = tc.queryReadyNumDaysCfg
			tableManager.cfg.Limits = &tcCopy.queryReadinessLimits
			require.NoError(t, tableManager.ensureQueryReadiness(context.Background()))

			for name, table := range tableManager.tables {
				require.Equal(t, tc.expectedQueryReadinessDoneForUsers[name], table.(*mockTable).queryReadinessDoneForUsers, "table: %s", name)
			}
		})
	}
}

type mockLimits struct {
	queryReadyIndexNumDaysDefault int
	queryReadyIndexNumDaysByUser  map[string]int
}

func (m *mockLimits) AllByUserID() map[string]*validation.Limits {
	allByUserID := map[string]*validation.Limits{}
	for userID := range m.queryReadyIndexNumDaysByUser {
		allByUserID[userID] = &validation.Limits{
			QueryReadyIndexNumDays: m.queryReadyIndexNumDaysByUser[userID],
		}
	}

	return allByUserID
}

func (m *mockLimits) DefaultLimits() *validation.Limits {
	return &validation.Limits{
		QueryReadyIndexNumDays: m.queryReadyIndexNumDaysDefault,
	}
}

type mockTable struct {
	tableExpired               bool
	queryReadinessDoneForUsers []string
}

func (m *mockTable) Close() {}

func (m *mockTable) MultiQueries(ctx context.Context, queries []index.Query, callback index.QueryPagesCallback) error {
	return nil
}

func (m *mockTable) DropUnusedIndex(ttl time.Duration, now time.Time) (bool, error) {
	return m.tableExpired, nil
}

func (m *mockTable) Sync(ctx context.Context) error {
	return nil
}

func (m *mockTable) EnsureQueryReadiness(ctx context.Context, userIDs []string) error {
	m.queryReadinessDoneForUsers = userIDs
	return nil
}

type mockIndexStorageClient struct {
	storage.Client
	tablesInStorage     []string
	userIndexesInTables map[string][]string
}

func (m *mockIndexStorageClient) ListTables(ctx context.Context) ([]string, error) {
	return m.tablesInStorage, nil
}

func (m *mockIndexStorageClient) ListFiles(ctx context.Context, tableName string, bypassCache bool) ([]storage.IndexFile, []string, error) {
	return []storage.IndexFile{}, m.userIndexesInTables[tableName], nil
}
