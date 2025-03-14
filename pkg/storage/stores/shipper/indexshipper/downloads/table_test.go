package downloads

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	userID    = "user-id"
	tableName = "test"
)

// storageClientWithFakeObjectsInList adds a fake object in the list call response which
// helps with testing the case where objects gets deleted in the middle of a Sync/Download operation due to compaction.
type storageClientWithFakeObjectsInList struct {
	storage.Client
}

func newStorageClientWithFakeObjectsInList(storageClient storage.Client) storage.Client {
	return storageClientWithFakeObjectsInList{storageClient}
}

func (o storageClientWithFakeObjectsInList) ListFiles(ctx context.Context, tableName string, _ bool) ([]storage.IndexFile, []string, error) {
	files, userIDs, err := o.Client.ListFiles(ctx, tableName, true)
	if err != nil {
		return nil, nil, err
	}

	files = append(files, storage.IndexFile{
		Name:       "fake-object",
		ModifiedAt: time.Now(),
	})

	return files, userIDs, nil
}

func (o storageClientWithFakeObjectsInList) ListUserFiles(ctx context.Context, tableName, userID string, _ bool) ([]storage.IndexFile, error) {
	files, err := o.Client.ListUserFiles(ctx, tableName, userID, true)
	if err != nil {
		return nil, err
	}

	files = append(files, storage.IndexFile{
		Name:       "fake-object",
		ModifiedAt: time.Now(),
	})

	return files, nil
}

func buildTestTable(t *testing.T, path string) (*table, stopFunc) {
	storageClient := buildTestStorageClient(t, path)
	cachePath := filepath.Join(path, cacheDirName)

	table := NewTable(tableName, cachePath, storageClient, func(path string) (index.Index, error) {
		return openMockIndexFile(t, path), nil
	}, newMetrics(nil)).(*table)
	_, usersWithIndex, err := table.storageClient.ListFiles(context.Background(), tableName, false)
	require.NoError(t, err)
	require.NoError(t, table.EnsureQueryReadiness(context.Background(), usersWithIndex))

	return table, table.Close
}

type mockIndexSet struct {
	IndexSet
	indexes     []index.Index
	failQueries bool
	lastUsedAt  time.Time
}

func (m *mockIndexSet) ForEach(_ context.Context, callback index.ForEachIndexCallback) error {
	for _, idx := range m.indexes {
		if err := callback(false, idx); err != nil {
			return err
		}
	}

	return nil
}

func (m *mockIndexSet) Err() error {
	var err error
	if m.failQueries {
		err = errors.New("fail queries")
	}
	return err
}

func (m *mockIndexSet) DropAllDBs() error {
	return nil
}

func (m *mockIndexSet) LastUsedAt() time.Time {
	return m.lastUsedAt
}

func (m *mockIndexSet) UpdateLastUsedAt() {
	m.lastUsedAt = time.Now()
}

func TestTable_ForEach(t *testing.T) {
	usersToSetup := []string{"user1", "user2"}
	for name, tc := range map[string]struct {
		withError  bool
		withUserID string
	}{
		"without error": {
			withUserID: usersToSetup[0],
		},
		"with error": {
			withError:  true,
			withUserID: usersToSetup[0],
		},
		"query with user2": {
			withUserID: usersToSetup[1],
		},
	} {
		t.Run(name, func(t *testing.T) {
			table := table{
				indexSets: map[string]IndexSet{},
				logger:    util_log.Logger,
			}

			table.indexSets[""] = &mockIndexSet{}
			for _, userID := range usersToSetup {
				var testIndexes []index.Index
				for _, indexPath := range setupIndexesAtPath(t, userID, t.TempDir(), 0, 5) {
					testIndexes = append(testIndexes, openMockIndexFile(t, indexPath))
				}
				table.indexSets[userID] = &mockIndexSet{
					failQueries: tc.withError,
					indexes:     testIndexes,
				}
			}

			var indexesFound []index.Index

			err := table.ForEach(context.Background(), tc.withUserID, func(_ bool, idx index.Index) error {
				indexesFound = append(indexesFound, idx)
				return nil
			})
			if tc.withError {
				require.Error(t, err)
				require.Len(t, table.indexSets, len(usersToSetup))
				ensureIndexSetExistsInTable(t, &table, "")
				for _, userID := range usersToSetup {
					if userID != tc.withUserID {
						ensureIndexSetExistsInTable(t, &table, userID)
					}
				}
			} else {
				require.NoError(t, err)
				require.Len(t, table.indexSets, len(usersToSetup)+1)
				require.Equal(t, table.indexSets[tc.withUserID].(*mockIndexSet).indexes, indexesFound)
			}
		})
	}
}

func TestTable_DropUnusedIndex(t *testing.T) {
	ttl := 24 * time.Hour
	now := time.Now()
	notExpiredIndexUserID := "not-expired-user-based-index"
	expiredIndexUserID := "expired-user-based-index"

	// initialize some indexSets with indexSet for expiredIndexUserID being expired
	indexSets := map[string]IndexSet{
		"":                    &mockIndexSet{lastUsedAt: time.Now()},
		notExpiredIndexUserID: &mockIndexSet{lastUsedAt: time.Now().Add(-time.Hour)},
		expiredIndexUserID:    &mockIndexSet{lastUsedAt: now.Add(-25 * time.Hour)},
	}

	table := table{
		indexSets: indexSets,
		logger:    util_log.Logger,
	}

	// ensure that we only find expiredIndexUserID to be dropped
	require.Equal(t, []string{expiredIndexUserID}, table.findExpiredIndexSets(ttl, now))

	// dropping unused indexSets should drop only index set for expiredIndexUserID
	allIndexSetsDropped, err := table.DropUnusedIndex(ttl, now)
	require.NoError(t, err)
	require.False(t, allIndexSetsDropped)

	// verify that we only dropped index set for expiredIndexUserID
	require.Len(t, table.indexSets, 2)
	ensureIndexSetExistsInTable(t, &table, "")
	ensureIndexSetExistsInTable(t, &table, notExpiredIndexUserID)

	// change the lastUsedAt for common index set to expire it
	indexSets[""].(*mockIndexSet).lastUsedAt = now.Add(-25 * time.Hour)

	// common index set should not get dropped since we still have notExpiredIndexUserID which is not expired
	require.Equal(t, []string(nil), table.findExpiredIndexSets(ttl, now))
	allIndexSetsDropped, err = table.DropUnusedIndex(ttl, now)
	require.NoError(t, err)
	require.False(t, allIndexSetsDropped)

	// none of the index set should be dropped
	require.Len(t, table.indexSets, 2)
	ensureIndexSetExistsInTable(t, &table, "")
	ensureIndexSetExistsInTable(t, &table, notExpiredIndexUserID)

	// change the lastUsedAt for all indexSets so that all of them get dropped
	for _, indexSets := range table.indexSets {
		indexSets.(*mockIndexSet).lastUsedAt = now.Add(-25 * time.Hour)
	}

	// ensure that we get userID of common index set at the end
	require.Equal(t, []string{notExpiredIndexUserID, ""}, table.findExpiredIndexSets(ttl, now))

	allIndexSetsDropped, err = table.DropUnusedIndex(ttl, now)
	require.NoError(t, err)
	require.True(t, allIndexSetsDropped)
}

func TestTable_EnsureQueryReadiness(t *testing.T) {
	tempDir := t.TempDir()
	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)

	// setup table in storage with 1 common db and 2 users with a db each
	tablePath := filepath.Join(objectStoragePath, tableName)
	setupIndexesAtPath(t, "", tablePath, 0, 5)
	usersToSetup := []string{"user1", "user2"}
	for _, userID := range usersToSetup {
		setupIndexesAtPath(t, userID, tablePath, 0, 5)
	}

	storageClient := buildTestStorageClient(t, tempDir)

	for _, tc := range []struct {
		name                       string
		usersToDoQueryReadinessFor []string
	}{
		{
			name: "only common index to be query ready",
		},
		{
			name:                       "one of the users to be query ready",
			usersToDoQueryReadinessFor: []string{"user-1"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cachePath := t.TempDir()
			table := NewTable(tableName, cachePath, storageClient, func(path string) (index.Index, error) {
				return openMockIndexFile(t, path), nil
			}, newMetrics(nil)).(*table)
			defer func() {
				table.Close()
			}()

			// EnsureQueryReadiness should update the last used at time of common index set
			require.NoError(t, table.EnsureQueryReadiness(context.Background(), tc.usersToDoQueryReadinessFor))
			require.Len(t, table.indexSets, len(tc.usersToDoQueryReadinessFor)+1)
			for _, userID := range append(tc.usersToDoQueryReadinessFor, "") {
				ensureIndexSetExistsInTable(t, table, userID)
				require.InDelta(t, time.Now().Unix(), table.indexSets[userID].(*indexSet).lastUsedAt.Unix(), 5)
			}

			// change the last used at to verify that it gets updated when we do the query readiness again
			for _, idxSet := range table.indexSets {
				idxSet.(*indexSet).lastUsedAt = time.Now().Add(-time.Hour)
			}

			// Running it multiple times should not have an impact other than updating last used at time
			for i := 0; i < 2; i++ {
				require.NoError(t, table.EnsureQueryReadiness(context.Background(), tc.usersToDoQueryReadinessFor))
				require.Len(t, table.indexSets, len(tc.usersToDoQueryReadinessFor)+1)
				for _, userID := range append(tc.usersToDoQueryReadinessFor, "") {
					ensureIndexSetExistsInTable(t, table, userID)
					require.InDelta(t, time.Now().Unix(), table.indexSets[userID].(*indexSet).lastUsedAt.Unix(), 5)
				}
			}
		})
	}
}

func TestTable_Sync(t *testing.T) {
	tempDir := t.TempDir()

	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)
	tablePathInStorage := filepath.Join(objectStoragePath, tableName)

	// list of dbs to create except newDB that would be added later as part of updates
	deleteDB := "delete"
	noUpdatesDB := "no-updates"
	newDB := "new"

	require.NoError(t, os.MkdirAll(tablePathInStorage, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tablePathInStorage, deleteDB), []byte(deleteDB), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tablePathInStorage, noUpdatesDB), []byte(noUpdatesDB), 0755))

	// create table instance
	table, stopFunc := buildTestTable(t, tempDir)
	defer stopFunc()

	// replace the storage client with the one that adds fake objects in the list call
	table.storageClient = newStorageClientWithFakeObjectsInList(table.storageClient)

	// check that table has expected indexes setup
	var indexesFound []string
	err := table.ForEach(context.Background(), userID, func(_ bool, idx index.Index) error {
		indexesFound = append(indexesFound, idx.Name())
		return nil
	})
	require.NoError(t, err)
	sort.Strings(indexesFound)
	require.Equal(t, []string{deleteDB, noUpdatesDB}, indexesFound)

	// add a sleep since we are updating a file and CI is sometimes too fast to create a difference in mtime of files
	time.Sleep(time.Second)

	// remove deleteDB and add the newDB
	require.NoError(t, os.Remove(filepath.Join(tablePathInStorage, deleteDB)))
	require.NoError(t, os.WriteFile(filepath.Join(tablePathInStorage, newDB), []byte(newDB), 0755))

	// sync the table
	table.storageClient.RefreshIndexTableCache(context.Background(), table.name)
	require.NoError(t, table.Sync(context.Background()))

	// check that table got the new index and dropped the deleted index
	indexesFound = []string{}
	err = table.ForEach(context.Background(), userID, func(_ bool, idx index.Index) error {
		indexesFound = append(indexesFound, idx.Name())
		return nil
	})
	require.NoError(t, err)
	sort.Strings(indexesFound)
	require.Equal(t, []string{newDB, noUpdatesDB}, indexesFound)

	// verify files in cache where dbs for the table are synced to double check.
	expectedFilesInDir := map[string]struct{}{
		noUpdatesDB: {},
		newDB:       {},
	}
	dirEntries, err := os.ReadDir(tablePathInStorage)
	require.NoError(t, err)
	require.Len(t, table.indexSets[""].(*indexSet).index, len(expectedFilesInDir))

	for _, entry := range dirEntries {
		require.False(t, entry.IsDir())
		_, ok := expectedFilesInDir[entry.Name()]
		require.True(t, ok)
	}

	// let us simulate a compaction to test stale index list cache handling

	// first, let us add a new file and refresh the index list cache
	oneMoreDB := "one-more-db"
	require.NoError(t, os.WriteFile(filepath.Join(tablePathInStorage, oneMoreDB), []byte(oneMoreDB), 0755))
	table.storageClient.RefreshIndexTableCache(context.Background(), table.name)

	// now, without syncing the table, let us compact the index in storage
	compactedDBName := "compacted-db"
	require.NoError(t, os.WriteFile(filepath.Join(tablePathInStorage, compactedDBName), []byte(compactedDBName), 0755))
	require.NoError(t, os.Remove(filepath.Join(tablePathInStorage, noUpdatesDB)))
	require.NoError(t, os.Remove(filepath.Join(tablePathInStorage, newDB)))
	require.NoError(t, os.Remove(filepath.Join(tablePathInStorage, oneMoreDB)))

	// let us run a sync which should detect the stale index list cache and sync the table after refreshing the cache
	require.NoError(t, table.Sync(context.Background()))

	// verify that table has got only compacted db
	indexesFound = []string{}
	err = table.ForEach(context.Background(), userID, func(_ bool, idx index.Index) error {
		indexesFound = append(indexesFound, idx.Name())
		return nil
	})
	require.NoError(t, err)
	sort.Strings(indexesFound)
	require.Equal(t, []string{compactedDBName}, indexesFound)
}

func TestLoadTable(t *testing.T) {
	tempDir := t.TempDir()

	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)
	tablePathInStorage := filepath.Join(objectStoragePath, tableName)

	// setup the table in storage with some records
	setupIndexesAtPath(t, "", tablePathInStorage, 0, 5)
	setupIndexesAtPath(t, userID, filepath.Join(tablePathInStorage, userID), 0, 5)

	storageClient := buildTestStorageClient(t, tempDir)
	tablePathInCache := filepath.Join(tempDir, cacheDirName, tableName)

	storageClient = newStorageClientWithFakeObjectsInList(storageClient)

	// try loading the table.
	table, err := LoadTable(tableName, tablePathInCache, storageClient, func(path string) (index.Index, error) {
		return openMockIndexFile(t, path), nil
	}, newMetrics(nil))
	require.NoError(t, err)
	require.NotNil(t, table)

	// check the loaded table to see it has right index files.
	expectedIndexes := append(buildListOfExpectedIndexes(userID, 0, 5), buildListOfExpectedIndexes("", 0, 5)...)
	verifyIndexForEach(t, expectedIndexes, func(callbackFunc index.ForEachIndexCallback) error {
		return table.ForEach(context.Background(), userID, callbackFunc)
	})

	// close the table to test reloading of table with already having files in the cache dir.
	table.Close()

	// add some more files to the storage.
	setupIndexesAtPath(t, "", tablePathInStorage, 5, 10)
	setupIndexesAtPath(t, userID, filepath.Join(tablePathInStorage, userID), 5, 10)

	// try loading the table, it should skip loading corrupt file and reload it from storage.
	table, err = LoadTable(tableName, tablePathInCache, storageClient, func(path string) (index.Index, error) {
		return openMockIndexFile(t, path), nil
	}, newMetrics(nil))
	require.NoError(t, err)
	require.NotNil(t, table)

	defer table.Close()

	expectedIndexes = append(buildListOfExpectedIndexes(userID, 0, 10), buildListOfExpectedIndexes("", 0, 10)...)
	verifyIndexForEach(t, expectedIndexes, func(callbackFunc index.ForEachIndexCallback) error {
		return table.ForEach(context.Background(), userID, callbackFunc)
	})
}

func buildListOfExpectedIndexes(userID string, start, end int) []string {
	var expectedIndexes []string
	for ; start < end; start++ {
		expectedIndexes = append(expectedIndexes, buildIndexFilename(userID, start))
	}

	return expectedIndexes
}

func ensureIndexSetExistsInTable(t *testing.T, table *table, indexSetName string) {
	_, ok := table.indexSets[indexSetName]
	require.True(t, ok)
}

func verifyIndexForEach(t *testing.T, expectedIndexes []string, forEachFunc func(callbackFunc index.ForEachIndexCallback) error) {
	var indexesFound []string
	err := forEachFunc(func(_ bool, idx index.Index) error {
		// get the reader for the index.
		readSeeker, err := idx.Reader()
		require.NoError(t, err)

		// seek it to 0
		_, err = readSeeker.Seek(0, 0)
		require.NoError(t, err)

		// read the contents of the index.
		buf, err := io.ReadAll(readSeeker)
		require.NoError(t, err)

		// see if it matches the name of the file
		require.Equal(t, idx.Name(), string(buf))

		indexesFound = append(indexesFound, idx.Name())
		return nil
	})
	require.NoError(t, err)

	sort.Strings(indexesFound)
	sort.Strings(expectedIndexes)
	require.Equal(t, expectedIndexes, indexesFound)
}
