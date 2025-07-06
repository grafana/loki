package compactor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compactor/deletion"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

func TestTableManager_RunCompaction(t *testing.T) {
	tempDir := t.TempDir()

	tablesPath := filepath.Join(tempDir, "index")
	commonDBsConfig := IndexesConfig{NumUnCompactedFiles: 5}
	perUserDBsConfig := PerUserIndexesConfig{}

	daySeconds := int64(24 * time.Hour / time.Second)
	tableNumEnd := time.Now().Unix() / daySeconds
	tableNumStart := tableNumEnd - 5

	periodConfigs := []config.PeriodConfig{
		{
			From:       config.DayTime{Time: model.Time(0)},
			IndexType:  "dummy",
			ObjectType: "fs_01",
			IndexTables: config.IndexPeriodicTableConfig{
				PathPrefix: "index/",
				PeriodicTableConfig: config.PeriodicTableConfig{
					Prefix: indexTablePrefix,
					Period: config.ObjectStorageIndexRequiredPeriod,
				}},
		},
	}

	for i := tableNumStart; i <= tableNumEnd; i++ {
		SetupTable(t, filepath.Join(tablesPath, fmt.Sprintf("%s%d", indexTablePrefix, i)), IndexesConfig{NumUnCompactedFiles: 5}, PerUserIndexesConfig{})
	}

	var (
		objectClients = map[config.DayTime]client.ObjectClient{}
		err           error
	)
	objectClients[periodConfigs[0].From], err = local.NewFSObjectClient(local.FSConfig{Directory: tempDir})
	require.NoError(t, err)

	compactor := setupTestCompactor(t, objectClients, periodConfigs, tempDir)
	err = compactor.tablesManager.runCompaction(context.Background(), false)
	require.NoError(t, err)

	for i := tableNumStart; i <= tableNumEnd; i++ {
		name := fmt.Sprintf("%s%d", indexTablePrefix, i)
		// verify that we have only 1 file left in storage after compaction.
		files, err := os.ReadDir(filepath.Join(tablesPath, name))
		require.NoError(t, err)
		require.Len(t, files, 1)
		require.True(t, strings.HasSuffix(files[0].Name(), ".gz"))

		verifyCompactedIndexTable(t, commonDBsConfig, perUserDBsConfig, filepath.Join(tablesPath, name))
	}
}

func TestTablesManager_TableLocking(t *testing.T) {
	commonDBsConfig := IndexesConfig{NumUnCompactedFiles: 5}
	perUserDBsConfig := PerUserIndexesConfig{}

	daySeconds := int64(24 * time.Hour / time.Second)
	tableNumEnd := time.Now().Unix() / daySeconds
	tableNumStart := tableNumEnd - 5

	setupCompactorAndIndex := func(tempDir string) *Compactor {
		tablesPath := filepath.Join(tempDir, "index")

		periodConfigs := []config.PeriodConfig{
			{
				From:       config.DayTime{Time: model.Time(0)},
				IndexType:  "dummy",
				ObjectType: "fs_01",
				IndexTables: config.IndexPeriodicTableConfig{
					PathPrefix: "index/",
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: indexTablePrefix,
						Period: config.ObjectStorageIndexRequiredPeriod,
					}},
			},
		}

		for i := tableNumStart; i <= tableNumEnd; i++ {
			SetupTable(t, filepath.Join(tablesPath, fmt.Sprintf("%s%d", indexTablePrefix, i)), IndexesConfig{NumUnCompactedFiles: 5}, PerUserIndexesConfig{})
		}

		var (
			objectClients = map[config.DayTime]client.ObjectClient{}
			err           error
		)
		objectClients[periodConfigs[0].From], err = local.NewFSObjectClient(local.FSConfig{Directory: tempDir})
		require.NoError(t, err)

		return setupTestCompactor(t, objectClients, periodConfigs, tempDir)
	}

	for _, tc := range []struct {
		name           string
		lockTable      string
		applyRetention bool

		retentionShouldTimeout bool
	}{
		{
			name: "no table locked - not applying retention",
		},
		{
			name:           "no table locked - applying retention",
			applyRetention: true,
		},
		{
			name:      "first table locked - not applying retention",
			lockTable: fmt.Sprintf("%s%d", indexTablePrefix, tableNumEnd),
		},
		{
			name:                   "first table locked - applying retention",
			lockTable:              fmt.Sprintf("%s%d", indexTablePrefix, tableNumEnd),
			applyRetention:         true,
			retentionShouldTimeout: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			tablesPath := filepath.Join(tempDir, "index")
			compactor := setupCompactorAndIndex(tempDir)

			// run the compaction twice, 2nd time without any table locking
			for n := 1; n <= 2; n++ {
				t.Run(fmt.Sprintf("%d", n), func(t *testing.T) {
					// lock table only for the first run
					if n == 1 && tc.lockTable != "" {
						locked, _ := compactor.tablesManager.tableLocker.lockTable(tc.lockTable)
						require.True(t, locked)

						defer compactor.tablesManager.tableLocker.unlockTable(tc.lockTable)
					}

					// set a timeout so that retention does not get blocked forever on acquiring table lock.
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					defer cancel()

					err := compactor.tablesManager.runCompaction(ctx, tc.applyRetention)
					// retention should not timeout after first run since we won't be locking the table
					if n == 1 && tc.retentionShouldTimeout {
						require.ErrorIs(t, err, context.DeadlineExceeded)
						require.Equal(t, float64(1), testutil.ToFloat64(compactor.metrics.applyRetentionOperationTotal.WithLabelValues(statusFailure)))
						require.Equal(t, float64(0), testutil.ToFloat64(compactor.metrics.compactTablesOperationTotal.WithLabelValues(statusFailure)))
						return
					}
					require.NoError(t, err)

					if n > 1 && tc.applyRetention && tc.retentionShouldTimeout {
						// this should be the first successful run if retention was expected to timeout out during first run
						require.Equal(t, float64(1), testutil.ToFloat64(compactor.metrics.applyRetentionOperationTotal.WithLabelValues(statusSuccess)))
					} else {
						// else it should have succeeded during all the n runs
						if tc.applyRetention {
							require.Equal(t, float64(n), testutil.ToFloat64(compactor.metrics.applyRetentionOperationTotal.WithLabelValues(statusSuccess)))
						} else {
							require.Equal(t, float64(n), testutil.ToFloat64(compactor.metrics.compactTablesOperationTotal.WithLabelValues(statusSuccess)))
						}
					}
					if tc.applyRetention {
						require.Equal(t, float64(0), testutil.ToFloat64(compactor.metrics.compactTablesOperationTotal.WithLabelValues(statusSuccess)))
					} else {
						require.Equal(t, float64(0), testutil.ToFloat64(compactor.metrics.applyRetentionOperationTotal.WithLabelValues(statusSuccess)))
					}

					// if the table was locked and compaction ran without retention then only locked table should have been skipped
					if tc.lockTable != "" {
						if tc.applyRetention {
							require.Equal(t, float64(0), testutil.ToFloat64(compactor.metrics.skippedCompactingLockedTables.WithLabelValues(tc.lockTable)))
						} else {
							// we only lock table during first run so second run should reset the skip count metric to 0
							skipCount := float64(0)
							if n == 1 {
								skipCount = 1
							}
							require.Equal(t, skipCount, testutil.ToFloat64(compactor.metrics.skippedCompactingLockedTables.WithLabelValues(tc.lockTable)))
						}
					}

					for tableNum := tableNumStart; tableNum <= tableNumEnd; tableNum++ {
						name := fmt.Sprintf("%s%d", indexTablePrefix, tableNum)
						files, err := os.ReadDir(filepath.Join(tablesPath, name))
						require.NoError(t, err)

						if n == 1 && name == tc.lockTable {
							// locked table should not be compacted during first run
							require.Len(t, files, 5)
						} else {
							require.Len(t, files, 1)
							require.True(t, strings.HasSuffix(files[0].Name(), ".gz"))

							verifyCompactedIndexTable(t, commonDBsConfig, perUserDBsConfig, filepath.Join(tablesPath, name))
						}
					}
				})
			}
		})
	}
}

func TestTablesManager_IterateTables(t *testing.T) {
	tempDir := t.TempDir()

	tablesPath := filepath.Join(tempDir, "index")
	commonDBsConfig := IndexesConfig{NumUnCompactedFiles: 5}
	perUserDBsConfig := PerUserIndexesConfig{}

	daySeconds := int64(24 * time.Hour / time.Second)
	tableNumEnd := time.Now().Unix() / daySeconds
	tableNumStart := tableNumEnd - 5

	periodConfigs := []config.PeriodConfig{
		{
			From:       config.DayTime{Time: model.Time(0)},
			IndexType:  "dummy",
			ObjectType: "fs_01",
			IndexTables: config.IndexPeriodicTableConfig{
				PathPrefix: "index/",
				PeriodicTableConfig: config.PeriodicTableConfig{
					Prefix: indexTablePrefix,
					Period: config.ObjectStorageIndexRequiredPeriod,
				}},
		},
	}

	var tablesBuilt []string
	for i := tableNumStart; i <= tableNumEnd; i++ {
		tableName := fmt.Sprintf("%s%d", indexTablePrefix, i)
		SetupTable(t, filepath.Join(tablesPath, tableName), IndexesConfig{NumUnCompactedFiles: 5}, PerUserIndexesConfig{})
		tablesBuilt = append(tablesBuilt, tableName)
	}

	var (
		objectClients = map[config.DayTime]client.ObjectClient{}
		err           error
	)
	objectClients[periodConfigs[0].From], err = local.NewFSObjectClient(local.FSConfig{Directory: tempDir})
	require.NoError(t, err)

	compactor := setupTestCompactor(t, objectClients, periodConfigs, tempDir)

	var tablesIterated []string
	err = compactor.tablesManager.IterateTables(context.Background(), func(tableName string, _ deletion.Table) error {
		// verify that table is locked while it is passed to the callback function
		require.True(t, compactor.tablesManager.tableLocker.isLocked(tableName))
		tablesIterated = append(tablesIterated, tableName)
		return nil
	})
	require.NoError(t, err)
	sort.Strings(tablesIterated)
	require.Equal(t, tablesBuilt, tablesIterated)

	for _, tableName := range tablesBuilt {
		// verify that table is unlocked
		require.False(t, compactor.tablesManager.tableLocker.isLocked(tableName))
		// verify that we have only 1 file left in storage after compaction.
		files, err := os.ReadDir(filepath.Join(tablesPath, tableName))
		require.NoError(t, err)
		require.Len(t, files, 1)
		require.True(t, strings.HasSuffix(files[0].Name(), ".gz"))

		verifyCompactedIndexTable(t, commonDBsConfig, perUserDBsConfig, filepath.Join(tablesPath, tableName))
	}

	tablesIterated = tablesIterated[:0]
	// ensure that the table is unlocked even when callback returns an error
	err = compactor.tablesManager.IterateTables(context.Background(), func(tableName string, _ deletion.Table) error {
		// verify that table is locked while it is passed to the callback function
		require.True(t, compactor.tablesManager.tableLocker.isLocked(tableName))
		tablesIterated = append(tablesIterated, tableName)
		return errors.New("some error")
	})
	// ensure that we stopped iterating tables and returned an error when callback gave an error
	require.Error(t, err)
	require.Len(t, tablesIterated, 1)

	// verify that all tables are still unlocked
	for _, tableName := range tablesBuilt {
		require.False(t, compactor.tablesManager.tableLocker.isLocked(tableName))
	}
}

func TestTablesManager_ApplyStorageUpdates(t *testing.T) {

	daySeconds := int64(24 * time.Hour / time.Second)
	tableNumEnd := time.Now().Unix() / daySeconds
	tableNumStart := tableNumEnd - 5

	periodConfigs := []config.PeriodConfig{
		{
			From:       config.DayTime{Time: model.Time(0)},
			IndexType:  "dummy",
			ObjectType: "fs_01",
			IndexTables: config.IndexPeriodicTableConfig{
				PathPrefix: "index/",
				PeriodicTableConfig: config.PeriodicTableConfig{
					Prefix: indexTablePrefix,
					Period: config.ObjectStorageIndexRequiredPeriod,
				}},
		},
	}

	var tablesToBuild []string
	for i := tableNumStart; i <= tableNumEnd; i++ {
		tablesToBuild = append(tablesToBuild, fmt.Sprintf("%s%d", indexTablePrefix, i))
	}

	for _, tc := range []struct {
		name      string
		updates   *dummyStorageUpdatesIterator
		expectErr bool
	}{
		{
			name:    "no updates to apply",
			updates: newDummyStorageUpdatesIterator(nil, nil),
		},
		{
			name: "applying updates on single table should succeed",
			updates: newDummyStorageUpdatesIterator([]indexDetails{
				{
					tableName: tablesToBuild[0],
					userID:    BuildUserID(0),
				},
			}, nil),
		},
		{
			name: "applying updates on multiple tables should succeed",
			updates: newDummyStorageUpdatesIterator([]indexDetails{
				{
					tableName: tablesToBuild[0],
					userID:    BuildUserID(0),
				},
				{
					tableName: tablesToBuild[1],
					userID:    BuildUserID(0),
				},
			}, nil),
		},
		{
			name: "applying multiple updates on same tables should succeed",
			updates: newDummyStorageUpdatesIterator([]indexDetails{
				{
					tableName: tablesToBuild[0],
					userID:    BuildUserID(0),
				},
				{
					tableName: tablesToBuild[0],
					userID:    BuildUserID(0),
				},
				{
					tableName: tablesToBuild[0],
					userID:    BuildUserID(1),
				},
			}, nil),
		},
		{
			name: "trying to apply updates on table not recognized by schema should throw an error",
			updates: newDummyStorageUpdatesIterator([]indexDetails{
				{
					tableName: "foo_100",
					userID:    "user1",
				},
			}, nil),
			expectErr: true,
		},
		{
			name: "applying updates on inexistent table recognized by schema should succeed",
			updates: newDummyStorageUpdatesIterator([]indexDetails{
				{
					tableName: fmt.Sprintf("%s%d", indexTablePrefix, tableNumStart-10),
					userID:    BuildUserID(0),
				},
			}, nil),
		},
		{
			name: "applying updates on inexistent user should succeed",
			updates: newDummyStorageUpdatesIterator([]indexDetails{
				{
					tableName: tablesToBuild[0],
					userID:    BuildUserID(10),
				},
			}, nil),
		},
		{
			name: "error from updates iterator should throw an error",
			updates: newDummyStorageUpdatesIterator([]indexDetails{
				{
					tableName: tablesToBuild[0],
					userID:    BuildUserID(0),
				},
			}, errors.New("some error")),
			expectErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			tablesPath := filepath.Join(tempDir, "index")

			commonDBsConfig := IndexesConfig{}
			perUserDBsConfig := PerUserIndexesConfig{
				IndexesConfig: IndexesConfig{
					NumUnCompactedFiles: 5,
				},
				NumUsers: 2,
			}

			for _, tableName := range tablesToBuild {
				SetupTable(t, filepath.Join(tablesPath, tableName), commonDBsConfig, perUserDBsConfig)
			}

			var (
				objectClients = map[config.DayTime]client.ObjectClient{}
				err           error
			)
			objectClients[periodConfigs[0].From], err = local.NewFSObjectClient(local.FSConfig{Directory: tempDir})
			require.NoError(t, err)

			compactor := setupTestCompactor(t, objectClients, periodConfigs, tempDir)
			tablesManager := compactor.tablesManager

			err = tablesManager.ApplyStorageUpdates(context.Background(), tc.updates)
			require.Equal(t, tc.expectErr, err != nil)

			tablesWithUpdates := map[string]struct{}{}
			for _, indexDetails := range tc.updates.iter {
				tablesWithUpdates[indexDetails.tableName] = struct{}{}
			}

			for _, tableName := range tablesToBuild {
				// verify that all table is unlocked irrespective of outcome of the apply updates operation
				require.False(t, compactor.tablesManager.tableLocker.isLocked(tableName))

				files, err := os.ReadDir(filepath.Join(tablesPath, tableName))
				require.NoError(t, err)

				// table which has updates should get compacted if there is no error expected from the apply updates operation
				if _, ok := tablesWithUpdates[tableName]; ok && !tc.expectErr {
					require.Len(t, files, 2)
					for _, file := range files {
						require.True(t, file.IsDir())
					}
				} else {
					require.Len(t, files, 5)
					for _, file := range files {
						require.False(t, file.IsDir())
					}
				}
			}
		})
	}
}

type indexDetails struct {
	userID, tableName string
}

type dummyStorageUpdatesIterator struct {
	iter []indexDetails
	curr int
	err  error
}

func newDummyStorageUpdatesIterator(iter []indexDetails, err error) *dummyStorageUpdatesIterator {
	return &dummyStorageUpdatesIterator{
		iter: iter,
		curr: -1,
		err:  err,
	}
}

func (d *dummyStorageUpdatesIterator) Next() bool {
	d.curr++
	return d.curr < len(d.iter)
}

func (d *dummyStorageUpdatesIterator) UserID() string {
	return d.iter[d.curr].userID
}

func (d *dummyStorageUpdatesIterator) TableName() string {
	return d.iter[d.curr].tableName
}

func (d *dummyStorageUpdatesIterator) Err() error {
	return d.err
}

func (d *dummyStorageUpdatesIterator) ForEachSeries(_ func(_ string, _ []string, _ []string, _ []deletion.Chunk) error) error {
	return nil
}
