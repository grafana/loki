package downloads

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/v3/pkg/validation"
)

const (
	objectsStorageDirName = "objects"
	cacheDirName          = "cache"
	indexTablePrefix      = "table_"
	indexTablePeriod      = 24 * time.Hour
)

func buildTestStorageClient(t *testing.T, path string) storage.Client {
	objectStoragePath := filepath.Join(path, objectsStorageDirName)
	fsObjectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStoragePath})
	require.NoError(t, err)

	return storage.NewIndexStorageClient(fsObjectClient, "")
}

type stopFunc func()

func buildTestTableManager(t *testing.T, path string, tableRangeToHandle *config.TableRange) (*tableManager, stopFunc) {
	indexStorageClient := buildTestStorageClient(t, path)
	cachePath := filepath.Join(path, cacheDirName)

	cfg := Config{
		CacheDir:     cachePath,
		SyncInterval: time.Hour,
		CacheTTL:     time.Hour,
		Limits:       &mockLimits{},
	}

	if tableRangeToHandle == nil {
		tableRangeToHandle = &config.TableRange{
			Start: 0,
			End:   math.MaxInt64,
			PeriodConfig: &config.PeriodConfig{
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: indexTablePrefix,
						Period: indexTablePeriod,
					}},
			},
		}
	}
	tblManager, err := NewTableManager(cfg, func(s string) (index.Index, error) {
		return openMockIndexFile(t, s), nil
	}, indexStorageClient, nil, *tableRangeToHandle, nil, log.NewNopLogger())
	require.NoError(t, err)

	return tblManager.(*tableManager), func() {
		tblManager.Stop()
	}
}

func TestTableManager_ForEach(t *testing.T) {
	tempDir := t.TempDir()
	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)

	tables := []string{"table1", "table2"}
	users := []string{"", "user1"}
	for _, tableName := range tables {
		for _, userID := range users {
			setupIndexesAtPath(t, userID, filepath.Join(objectStoragePath, tableName, userID), 1, 5)
		}
	}

	tableManager, stopFunc := buildTestTableManager(t, tempDir, nil)
	defer stopFunc()

	for _, tableName := range tables {
		for i, userID := range []string{"user1", "common-index-user"} {
			expectedIndexes := buildListOfExpectedIndexes("", 1, 5)
			if i == 0 {
				expectedIndexes = append(expectedIndexes, buildListOfExpectedIndexes(userID, 1, 5)...)
			}
			verifyIndexForEach(t, expectedIndexes, func(callbackFunc index.ForEachIndexCallback) error {
				return tableManager.ForEach(context.Background(), tableName, userID, callbackFunc)
			})
		}
	}
}

func TestTableManager_cleanupCache(t *testing.T) {
	tempDir := t.TempDir()

	tableManager, stopFunc := buildTestTableManager(t, tempDir, nil)
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
	mockIndexStorageClient := &mockIndexStorageClient{
		userIndexesInTables: map[string][]string{},
	}

	cfg := Config{
		SyncInterval: time.Hour,
		CacheTTL:     time.Hour,
	}

	tableManager := &tableManager{
		cfg:                cfg,
		indexStorageClient: mockIndexStorageClient,
		tables:             make(map[string]Table),
		tableRangeToHandle: config.TableRange{
			Start: 0, End: math.MaxInt64, PeriodConfig: &config.PeriodConfig{},
		},
		ctx:    context.Background(),
		cancel: func() {},
		logger: log.NewNopLogger(),
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
		tableRangeToHandle   *config.TableRange

		expectedQueryReadinessDoneForUsers map[string][]string
	}{
		// includes whole table range
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
		// includes limited table range
		{
			name:                 "common index: 20 days",
			queryReadyNumDaysCfg: 20,
			tableRangeToHandle: &config.TableRange{
				End:   buildTableNumber(5),
				Start: buildTableNumber(9),
				PeriodConfig: &config.PeriodConfig{
					IndexTables: config.IndexPeriodicTableConfig{
						PeriodicTableConfig: config.PeriodicTableConfig{
							Prefix: indexTablePrefix,
							Period: indexTablePeriod,
						}},
				},
			},
			expectedQueryReadinessDoneForUsers: map[string][]string{
				buildTableName(5): {},
				buildTableName(6): {},
				buildTableName(7): {},
				buildTableName(8): {},
				buildTableName(9): {},
			},
		},
		{
			name: "common index: 5 days, user index default: 2 days",
			queryReadinessLimits: mockLimits{
				queryReadyIndexNumDaysDefault: 2,
			},
			queryReadyNumDaysCfg: 5,
			tableRangeToHandle: &config.TableRange{
				End:   buildTableNumber(2),
				Start: buildTableNumber(4),
				PeriodConfig: &config.PeriodConfig{
					IndexTables: config.IndexPeriodicTableConfig{
						PeriodicTableConfig: config.PeriodicTableConfig{
							Prefix: indexTablePrefix,
							Period: indexTablePeriod,
						}},
				},
			},
			expectedQueryReadinessDoneForUsers: map[string][]string{
				buildTableName(2): {"user1", "user2"},
				buildTableName(3): {},
				buildTableName(4): {},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc := tc // just to make the linter happy
			resetTables()
			tableManager.cfg.QueryReadyNumDays = tc.queryReadyNumDaysCfg
			tableManager.cfg.Limits = &tc.queryReadinessLimits
			if tc.tableRangeToHandle == nil {
				tableManager.tableRangeToHandle = config.TableRange{
					Start: 0, End: math.MaxInt64, PeriodConfig: &config.PeriodConfig{
						IndexTables: config.IndexPeriodicTableConfig{
							PeriodicTableConfig: config.PeriodicTableConfig{
								Prefix: indexTablePrefix,
								Period: indexTablePeriod,
							}},
					},
				}
			} else {
				tableManager.tableRangeToHandle = *tc.tableRangeToHandle
			}
			require.NoError(t, tableManager.ensureQueryReadiness(context.Background()))

			for name, table := range tableManager.tables {
				require.Equal(t, tc.expectedQueryReadinessDoneForUsers[name], table.(*mockTable).queryReadinessDoneForUsers, "table: %s", name)
			}
		})
	}
}

func TestTableManager_loadTables(t *testing.T) {
	tempDir := t.TempDir()
	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)
	cachePath := filepath.Join(tempDir, cacheDirName)

	var tables []string
	for i := 0; i < 10; i++ {
		tables = append(tables, buildTableName(i))
	}
	users := []string{"", "user1"}
	for _, tableName := range tables {
		for _, userID := range users {
			setupIndexesAtPath(t, userID, filepath.Join(objectStoragePath, tableName, userID), 1, 5)
			setupIndexesAtPath(t, userID, filepath.Join(cachePath, tableName, userID), 1, 5)
		}
	}

	verifyTables := func(tableManager *tableManager, tables []string) {
		for _, tableName := range tables {
			for i, userID := range []string{"user1", "common-index-user"} {
				expectedIndexes := buildListOfExpectedIndexes("", 1, 5)
				if i == 0 {
					expectedIndexes = append(expectedIndexes, buildListOfExpectedIndexes(userID, 1, 5)...)
				}
				verifyIndexForEach(t, expectedIndexes, func(callbackFunc index.ForEachIndexCallback) error {
					return tableManager.ForEach(context.Background(), tableName, userID, callbackFunc)
				})
			}
		}
	}

	tableManager, stopFunc := buildTestTableManager(t, tempDir, nil)
	require.Equal(t, len(tables), len(tableManager.tables))
	verifyTables(tableManager, tables)

	stopFunc()

	tableManager, stopFunc = buildTestTableManager(t, tempDir, &config.TableRange{
		End:   buildTableNumber(4),
		Start: buildTableNumber(8),
		PeriodConfig: &config.PeriodConfig{
			IndexTables: config.IndexPeriodicTableConfig{
				PeriodicTableConfig: config.PeriodicTableConfig{
					Prefix: indexTablePrefix,
					Period: indexTablePeriod,
				}},
		},
	},
	)
	defer stopFunc()
	require.Equal(t, 5, len(tableManager.tables))

	tables = []string{
		buildTableName(4),
		buildTableName(5),
		buildTableName(6),
		buildTableName(7),
		buildTableName(8),
	}
	verifyTables(tableManager, tables)
}

type mockLimits struct {
	queryReadyIndexNumDaysDefault int
	queryReadyIndexNumDaysByUser  map[string]int
	volumeMaxSeries               int
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

func (m *mockLimits) VolumeMaxSeries(_ string) int {
	return m.volumeMaxSeries
}

type mockTable struct {
	tableExpired               bool
	queryReadinessDoneForUsers []string
}

func (m *mockTable) ForEach(_ context.Context, _ string, _ index.ForEachIndexCallback) error {
	return nil
}
func (m *mockTable) ForEachConcurrent(_ context.Context, _ string, _ index.ForEachIndexCallback) error {
	return nil
}

func (m *mockTable) Close() {}

func (m *mockTable) DropUnusedIndex(_ time.Duration, _ time.Time) (bool, error) {
	return m.tableExpired, nil
}

func (m *mockTable) Sync(_ context.Context) error {
	return nil
}

func (m *mockTable) EnsureQueryReadiness(_ context.Context, userIDs []string) error {
	m.queryReadinessDoneForUsers = userIDs
	return nil
}

type mockIndexStorageClient struct {
	storage.Client
	tablesInStorage     []string
	userIndexesInTables map[string][]string
}

func (m *mockIndexStorageClient) ListTables(_ context.Context) ([]string, error) {
	return m.tablesInStorage, nil
}

func (m *mockIndexStorageClient) ListFiles(_ context.Context, tableName string, _ bool) ([]storage.IndexFile, []string, error) {
	return []storage.IndexFile{}, m.userIndexesInTables[tableName], nil
}

func (m *mockIndexStorageClient) RefreshIndexTableNamesCache(_ context.Context) {}

func buildTableNumber(idx int) int64 {
	return getActiveTableNumber() - int64(idx)
}

func buildTableName(idx int) string {
	return fmt.Sprintf("%s%d", indexTablePrefix, buildTableNumber(idx))
}
