package downloads

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/local"
	"github.com/grafana/loki/pkg/storage/stores/shipper/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper/testutil"
	util_log "github.com/grafana/loki/pkg/util/log"
)

const (
	cacheDirName          = "cache"
	objectsStorageDirName = "objects"
	userID                = "user-id"
)

// storageClientWithFakeObjectsInList adds a fake object in the list call response which
// helps with testing the case where objects gets deleted in the middle of a Sync/Download operation due to compaction.
type storageClientWithFakeObjectsInList struct {
	storage.Client
}

func newStorageClientWithFakeObjectsInList(storageClient storage.Client) storage.Client {
	return storageClientWithFakeObjectsInList{storageClient}
}

func (o storageClientWithFakeObjectsInList) ListFiles(ctx context.Context, tableName string) ([]storage.IndexFile, []string, error) {
	files, userIDs, err := o.Client.ListFiles(ctx, tableName)
	if err != nil {
		return nil, nil, err
	}

	files = append(files, storage.IndexFile{
		Name:       "fake-object",
		ModifiedAt: time.Now(),
	})

	return files, userIDs, nil
}

type stopFunc func()

func buildTestClients(t *testing.T, path string) (*local.BoltIndexClient, storage.Client) {
	cachePath := filepath.Join(path, cacheDirName)

	boltDBIndexClient, err := local.NewBoltDBIndexClient(local.BoltDBConfig{Directory: cachePath})
	require.NoError(t, err)

	objectStoragePath := filepath.Join(path, objectsStorageDirName)
	fsObjectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStoragePath})
	require.NoError(t, err)

	return boltDBIndexClient, storage.NewIndexStorageClient(fsObjectClient, "")
}

func buildTestTable(t *testing.T, path string) (*table, *local.BoltIndexClient, stopFunc) {
	boltDBIndexClient, storageClient := buildTestClients(t, path)
	cachePath := filepath.Join(path, cacheDirName)

	table := NewTable(tableName, cachePath, storageClient, boltDBIndexClient, newMetrics(nil)).(*table)
	_, usersWithIndex, err := table.storageClient.ListFiles(context.Background(), tableName)
	require.NoError(t, err)
	require.NoError(t, table.EnsureQueryReadiness(context.Background(), usersWithIndex))

	return table, boltDBIndexClient, func() {
		table.Close()
		boltDBIndexClient.Stop()
	}
}

type mockIndexSet struct {
	IndexSet
	queriesDone []chunk.IndexQuery
	failQueries bool
	lastUsedAt  time.Time
}

func (m *mockIndexSet) MultiQueries(_ context.Context, queries []chunk.IndexQuery, _ chunk.QueryPagesCallback) error {
	m.queriesDone = append(m.queriesDone, queries...)
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

func TestTable_MultiQueries(t *testing.T) {
	usersToSetup := []string{"user1", "user2"}
	for name, tc := range map[string]struct {
		withError       bool
		queryWithUserID string
	}{
		"without error": {
			queryWithUserID: usersToSetup[0],
		},
		"with error": {
			withError:       true,
			queryWithUserID: usersToSetup[0],
		},
		"query with user2": {
			queryWithUserID: usersToSetup[1],
		},
	} {
		t.Run(name, func(t *testing.T) {
			table := table{
				indexSets: map[string]IndexSet{},
				logger:    util_log.Logger,
			}

			table.indexSets[""] = &mockIndexSet{}
			for _, userID := range usersToSetup {
				table.indexSets[userID] = &mockIndexSet{failQueries: tc.withError}
			}

			var testQueries []chunk.IndexQuery
			for i := 0; i < 5; i++ {
				testQueries = append(testQueries, chunk.IndexQuery{
					TableName:        "test-table",
					HashValue:        fmt.Sprint(i),
					RangeValuePrefix: []byte(fmt.Sprintf("range-value-prefix-%d", i)),
					RangeValueStart:  []byte(fmt.Sprintf("range-value-start-%d", i)),
					ValueEqual:       []byte(fmt.Sprintf("value-equal-%d", i)),
				})
			}

			err := table.MultiQueries(user.InjectOrgID(context.Background(), tc.queryWithUserID), testQueries, func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
				return true
			})
			if tc.withError {
				require.Error(t, err)
				require.Len(t, table.indexSets, len(usersToSetup))
				ensureIndexSetExistsInTable(t, &table, "")
				for _, userID := range usersToSetup {
					if userID != tc.queryWithUserID {
						ensureIndexSetExistsInTable(t, &table, userID)
					}
				}
			} else {
				require.NoError(t, err)
				require.Len(t, table.indexSets, len(usersToSetup)+1)
				// ensure that only common and user specific index sets are queried
				for userID, indexSet := range table.indexSets {
					if userID == "" || userID == tc.queryWithUserID {
						require.EqualValues(t, testQueries, indexSet.(*mockIndexSet).queriesDone)
					} else {
						require.Len(t, indexSet.(*mockIndexSet).queriesDone, 0)
					}
				}
			}
		})
	}
}

func TestTable_MultiQueries_Response(t *testing.T) {
	tempDir := t.TempDir()

	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)

	testutil.SetupTable(t, filepath.Join(objectStoragePath, tableName), testutil.DBsConfig{
		DBRecordsStart:    0,
		NumUnCompactedDBs: 5,
	}, testutil.PerUserDBsConfig{
		DBsConfig: testutil.DBsConfig{
			DBRecordsStart:    500,
			NumUnCompactedDBs: 5,
			NumCompactedDBs:   1,
		},
		NumUsers: 1,
	})

	table, _, stopFunc := buildTestTable(t, tempDir)
	defer func() {
		stopFunc()
	}()

	// build queries each looking for specific value from all the dbs
	var queries []chunk.IndexQuery
	for i := 0; i < 1000; i++ {
		queries = append(queries, chunk.IndexQuery{ValueEqual: []byte(strconv.Itoa(i))})
	}

	// run the queries concurrently
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// query for user 0 which has per user index setup which should return both user and common index.
			testutil.TestSingleTableQuery(t, testutil.BuildUserID(0), queries, table, 0, 1000)

			// query for user 1 which does not have per user index setup which should return only common index.
			testutil.TestSingleTableQuery(t, testutil.BuildUserID(1), queries, table, 0, 500)
		}()
	}

	wg.Wait()
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
	testutil.SetupTable(t, filepath.Join(objectStoragePath, tableName), testutil.DBsConfig{
		DBRecordsStart:    0,
		NumUnCompactedDBs: 1,
	}, testutil.PerUserDBsConfig{
		DBsConfig: testutil.DBsConfig{
			DBRecordsStart:  100,
			NumCompactedDBs: 1,
		},
		NumUsers: 2,
	})

	boltDBIndexClient, storageClient := buildTestClients(t, tempDir)
	defer boltDBIndexClient.Stop()

	for _, tc := range []struct {
		name                       string
		usersToDoQueryReadinessFor []string
	}{
		{
			name: "only common index to be query ready",
		},
		{
			name:                       "one of the users to be query ready",
			usersToDoQueryReadinessFor: []string{testutil.BuildUserID(0)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cachePath := t.TempDir()
			table := NewTable(tableName, cachePath, storageClient, boltDBIndexClient, newMetrics(nil)).(*table)
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

	testDBs := map[string]testutil.DBConfig{
		deleteDB: {
			DBRecords: testutil.DBRecords{
				Start:      0,
				NumRecords: 10,
			},
		},
		noUpdatesDB: {
			DBRecords: testutil.DBRecords{
				Start:      10,
				NumRecords: 10,
			},
		},
	}

	// setup the table in storage with some records
	testutil.SetupDBsAtPath(t, filepath.Join(objectStoragePath, tableName), testDBs, nil)

	// create table instance
	table, boltdbClient, stopFunc := buildTestTable(t, tempDir)
	defer func() {
		stopFunc()
	}()

	// replace the storage client with the one that adds fake objects in the list call
	table.storageClient = newStorageClientWithFakeObjectsInList(table.storageClient)

	// query table to see it has expected records setup
	testutil.TestSingleTableQuery(t, userID, []chunk.IndexQuery{{}}, table, 0, 20)

	// add a sleep since we are updating a file and CI is sometimes too fast to create a difference in mtime of files
	time.Sleep(time.Second)

	// remove deleteDB and add the newDB
	require.NoError(t, os.Remove(filepath.Join(tablePathInStorage, deleteDB)))
	testutil.AddRecordsToDB(t, filepath.Join(tablePathInStorage, newDB), boltdbClient, 20, 10, nil)

	// sync the table
	require.NoError(t, table.Sync(context.Background()))

	// query and verify table has expected records from new db and the records from deleted db are gone
	testutil.TestSingleTableQuery(t, userID, []chunk.IndexQuery{{}}, table, 10, 20)

	// verify files in cache where dbs for the table are synced to double check.
	expectedFilesInDir := map[string]struct{}{
		noUpdatesDB: {},
		newDB:       {},
	}
	filesInfo, err := ioutil.ReadDir(tablePathInStorage)
	require.NoError(t, err)
	require.Len(t, table.indexSets[""].(*indexSet).dbs, len(expectedFilesInDir))

	for _, fileInfo := range filesInfo {
		require.False(t, fileInfo.IsDir())
		_, ok := expectedFilesInDir[fileInfo.Name()]
		require.True(t, ok)
	}
}

func TestTable_QueryResponse(t *testing.T) {
	tempDir := t.TempDir()

	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)
	tablePathInStorage := filepath.Join(objectStoragePath, tableName)

	commonDBs := map[string]testutil.DBConfig{
		"db1": {
			CompressFile: true,
			DBRecords: testutil.DBRecords{
				Start:      0,
				NumRecords: 10,
			},
		},
		"duplicate_db1": {
			CompressFile: true,
			DBRecords: testutil.DBRecords{
				Start:      0,
				NumRecords: 10,
			},
		},
		"db2": {
			DBRecords: testutil.DBRecords{
				Start:      10,
				NumRecords: 10,
			},
		},
		"partially_duplicate_db2": {
			CompressFile: true,
			DBRecords: testutil.DBRecords{
				Start:      10,
				NumRecords: 5,
			},
		},
		"db3": {
			DBRecords: testutil.DBRecords{
				Start:      20,
				NumRecords: 10,
			},
		},
	}

	userDBs := map[string]testutil.DBConfig{
		"overlaps_with_common_dbs": {
			CompressFile: true,
			DBRecords: testutil.DBRecords{
				Start:      10,
				NumRecords: 30,
			},
		},
		"same_db_again": {
			DBRecords: testutil.DBRecords{
				Start:      10,
				NumRecords: 20,
			},
		},
		"additional_records": {
			CompressFile: true,
			DBRecords: testutil.DBRecords{
				Start:      30,
				NumRecords: 10,
			},
		},
	}

	testutil.SetupDBsAtPath(t, tablePathInStorage, commonDBs, nil)
	testutil.SetupDBsAtPath(t, filepath.Join(tablePathInStorage, userID), userDBs, nil)

	table, _, stopFunc := buildTestTable(t, tempDir)
	defer func() {
		stopFunc()
	}()

	// build queries each looking for specific value from all the dbs
	var queries []chunk.IndexQuery
	for i := 5; i < 35; i++ {
		queries = append(queries, chunk.IndexQuery{ValueEqual: []byte(strconv.Itoa(i))})
	}

	// Query the table with user id which has user specific index as well.
	// Response should include records from both user and common index.
	testutil.TestSingleTableQuery(t, userID, queries, table, 5, 30)

	// Query the table with different user id which does not have user specific index.
	// Response should include records only from common index.
	testutil.TestSingleTableQuery(t, "fake", queries, table, 5, 25)
}

func TestLoadTable(t *testing.T) {
	tempDir := t.TempDir()

	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)
	tablePathInStorage := filepath.Join(objectStoragePath, tableName)

	commonDBs := make(map[string]testutil.DBConfig)
	userDBs := make(map[string]testutil.DBConfig)
	for i := 0; i < 10; i++ {
		commonDBs[fmt.Sprint(i)] = testutil.DBConfig{
			DBRecords: testutil.DBRecords{
				Start:      i,
				NumRecords: 1,
			},
		}
		userDBs[fmt.Sprint(i+10)] = testutil.DBConfig{
			DBRecords: testutil.DBRecords{
				Start:      i + 10,
				NumRecords: 1,
			},
		}
	}

	// setup the table in storage with some records
	testutil.SetupDBsAtPath(t, tablePathInStorage, commonDBs, nil)
	testutil.SetupDBsAtPath(t, filepath.Join(tablePathInStorage, userID), userDBs, nil)

	boltDBIndexClient, storageClient := buildTestClients(t, tempDir)
	tablePathInCache := filepath.Join(tempDir, cacheDirName, tableName)

	storageClient = newStorageClientWithFakeObjectsInList(storageClient)

	// try loading the table.
	table, err := LoadTable(tableName, tablePathInCache, storageClient, boltDBIndexClient, newMetrics(nil))
	require.NoError(t, err)
	require.NotNil(t, table)

	// query the loaded table to see if it has right data.
	testutil.TestSingleTableQuery(t, userID, []chunk.IndexQuery{{}}, table, 0, 20)

	// close the table to test reloading of table with already having files in the cache dir.
	table.Close()

	// change a boltdb file to text file which would fail to open.
	require.NoError(t, ioutil.WriteFile(filepath.Join(tablePathInCache, "0"), []byte("invalid boltdb file"), 0666))
	require.NoError(t, ioutil.WriteFile(filepath.Join(tablePathInCache, userID, "10"), []byte("invalid boltdb file"), 0666))

	// verify that changed boltdb file can't be opened.
	_, err = local.OpenBoltdbFile(filepath.Join(tablePathInCache, "0"))
	require.Error(t, err)

	// add some more files to the storage.
	commonDBs = make(map[string]testutil.DBConfig)
	userDBs = make(map[string]testutil.DBConfig)
	for i := 20; i < 30; i++ {
		commonDBs[fmt.Sprint(i)] = testutil.DBConfig{
			DBRecords: testutil.DBRecords{
				Start:      i,
				NumRecords: 1,
			},
		}
		userDBs[fmt.Sprint(i+10)] = testutil.DBConfig{
			DBRecords: testutil.DBRecords{
				Start:      i + 10,
				NumRecords: 1,
			},
		}
	}

	testutil.SetupDBsAtPath(t, tablePathInStorage, commonDBs, nil)
	testutil.SetupDBsAtPath(t, filepath.Join(tablePathInStorage, userID), userDBs, nil)

	// try loading the table, it should skip loading corrupt file and reload it from storage.
	table, err = LoadTable(tableName, tablePathInCache, storageClient, boltDBIndexClient, newMetrics(nil))
	require.NoError(t, err)
	require.NotNil(t, table)

	defer table.Close()

	// query the loaded table to see if it has right data.
	testutil.TestSingleTableQuery(t, userID, []chunk.IndexQuery{{}}, table, 0, 40)
}

func ensureIndexSetExistsInTable(t *testing.T, table *table, indexSetName string) {
	_, ok := table.indexSets[indexSetName]
	require.True(t, ok)
}
