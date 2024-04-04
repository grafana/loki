package boltdb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
	shipperindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/testutil"
)

const (
	indexDirName = "index"
	userID       = "user-id"
)

type mockIndexShipper struct {
	addedIndexes map[string][]shipperindex.Index
}

func newMockIndexShipper() Shipper {
	return &mockIndexShipper{
		addedIndexes: make(map[string][]shipperindex.Index),
	}
}

func (m *mockIndexShipper) AddIndex(tableName, _ string, index shipperindex.Index) error {
	m.addedIndexes[tableName] = append(m.addedIndexes[tableName], index)
	return nil
}

func (m *mockIndexShipper) ForEach(_ context.Context, tableName, _ string, callback shipperindex.ForEachIndexCallback) error {
	for _, idx := range m.addedIndexes[tableName] {
		if err := callback(false, idx); err != nil {
			return err
		}
	}

	return nil
}

func (m *mockIndexShipper) hasIndex(tableName, indexName string) bool {
	for _, index := range m.addedIndexes[tableName] {
		if indexName == index.Name() {
			return true
		}
	}

	return false
}

type stopFunc func()

func buildTestTable(t *testing.T, path string, makePerTenantBuckets bool) (*Table, stopFunc) {
	mockIndexShipper := newMockIndexShipper()
	indexPath := filepath.Join(path, indexDirName)

	require.NoError(t, util.EnsureDirectory(indexPath))

	table, err := NewTable(indexPath, "test", mockIndexShipper, makePerTenantBuckets)
	require.NoError(t, err)

	return table, table.Stop
}

func TestLoadTable(t *testing.T) {
	indexPath := t.TempDir()

	boltDBIndexClient, err := local.NewBoltDBIndexClient(local.BoltDBConfig{Directory: indexPath})
	require.NoError(t, err)

	defer func() {
		boltDBIndexClient.Stop()
	}()

	// setup some dbs with default bucket and per tenant bucket for a table at a path.
	tablePath := filepath.Join(indexPath, "test-table")
	testutil.SetupDBsAtPath(t, tablePath, map[string]testutil.DBConfig{
		"db1": {
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
	}, nil)

	// change a boltdb file to text file which would fail to open.
	invalidFilePath := filepath.Join(tablePath, "invalid")
	require.NoError(t, os.WriteFile(invalidFilePath, []byte("invalid boltdb file"), 0o666))

	// verify that changed boltdb file can't be opened.
	_, err = local.OpenBoltdbFile(invalidFilePath)
	require.Error(t, err)

	// try loading the table.
	table, err := LoadTable(tablePath, "test", newMockIndexShipper(), false, newTableManagerMetrics(nil))
	require.NoError(t, err)
	require.NotNil(t, table)

	defer func() {
		table.Stop()
	}()

	// verify that we still have 3 files(2 valid, 1 invalid)
	dirEntries, err := os.ReadDir(tablePath)
	require.NoError(t, err)
	require.Len(t, dirEntries, 3)

	// query the loaded table to see if it has right data.
	require.NoError(t, table.Snapshot())
	testutil.VerifyIndexes(t, userID, []index.Query{{TableName: table.name}}, func(ctx context.Context, _ string, callback func(b *bbolt.DB) error) error {
		return table.ForEach(ctx, callback)
	}, 0, 20)
}

func TestTable_Write(t *testing.T) {
	for _, withPerTenantBucket := range []bool{false, true} {
		t.Run(fmt.Sprintf("withPerTenantBucket=%v", withPerTenantBucket), func(t *testing.T) {
			tempDir := t.TempDir()

			table, stopFunc := buildTestTable(t, tempDir, withPerTenantBucket)
			defer stopFunc()

			now := time.Now()

			// allow modifying last 5 shards
			table.modifyShardsSince = now.Add(-5 * ShardDBsByDuration).Unix()

			// a couple of times for which we want to do writes to make the table create different shards
			testCases := []struct {
				writeTime time.Time
				dbName    string // set only when it is supposed to be written to a different name than usual
			}{
				{
					writeTime: now,
				},
				{
					writeTime: now.Add(-(ShardDBsByDuration + 5*time.Minute)),
				},
				{
					writeTime: now.Add(-(ShardDBsByDuration*3 + 3*time.Minute)),
				},
				{
					writeTime: now.Add(-6 * ShardDBsByDuration), // write with time older than table.modifyShardsSince
					dbName:    fmt.Sprint(table.modifyShardsSince),
				},
			}

			numFiles := 0

			// performing writes and checking whether the index gets written to right shard
			for i, tc := range testCases {
				t.Run(fmt.Sprint(i), func(t *testing.T) {
					batch := local.NewWriteBatch()
					testutil.AddRecordsToBatch(batch, "test", i*10, 10)
					require.NoError(t, table.write(user.InjectOrgID(context.Background(), userID), tc.writeTime, batch.(*local.BoltWriteBatch).Writes["test"]))

					numFiles++
					require.Equal(t, numFiles, len(table.dbs))

					expectedDBName := tc.dbName
					if expectedDBName == "" {
						expectedDBName = fmt.Sprint(tc.writeTime.Truncate(ShardDBsByDuration).Unix())
					}
					db, ok := table.dbs[expectedDBName]
					require.True(t, ok)

					require.NoError(t, table.Snapshot())

					// test that the table has current + previous records
					testutil.VerifyIndexes(t, userID, []index.Query{{}},
						func(ctx context.Context, _ string, callback func(b *bbolt.DB) error) error {
							return table.ForEach(ctx, callback)
						},
						0, (i+1)*10)
					bucketToQuery := local.IndexBucketName
					if withPerTenantBucket {
						bucketToQuery = []byte(userID)
					}
					testutil.VerifySingleIndexFile(t, index.Query{}, db, bucketToQuery, i*10, 10)
				})
			}
		})
	}
}

func TestTable_HandoverIndexesToShipper(t *testing.T) {
	for _, withPerTenantBucket := range []bool{false, true} {
		t.Run(fmt.Sprintf("withPerTenantBucket=%v", withPerTenantBucket), func(t *testing.T) {
			tempDir := t.TempDir()

			table, stopFunc := buildTestTable(t, tempDir, withPerTenantBucket)
			defer stopFunc()

			now := time.Now()

			// write a batch for now
			batch := local.NewWriteBatch()
			testutil.AddRecordsToBatch(batch, table.name, 0, 10)
			require.NoError(t, table.write(user.InjectOrgID(context.Background(), userID), now, batch.(*local.BoltWriteBatch).Writes[table.name]))

			// handover indexes from the table
			require.NoError(t, table.HandoverIndexesToShipper(true))
			require.Len(t, table.dbs, 0)
			require.Len(t, table.dbSnapshots, 0)

			// check that shipper has the data we handed over
			indexShipper := table.indexShipper.(*mockIndexShipper)
			require.Len(t, indexShipper.addedIndexes[table.name], 1)

			testutil.VerifyIndexes(t, userID, []index.Query{{TableName: table.name}},
				func(ctx context.Context, _ string, callback func(b *bbolt.DB) error) error {
					return indexShipper.ForEach(ctx, table.name, "", func(_ bool, index shipperindex.Index) error {
						return callback(index.(*IndexFile).GetBoltDB())
					})
				},
				0, 10)

			// write a batch to another shard
			batch = local.NewWriteBatch()
			testutil.AddRecordsToBatch(batch, table.name, 10, 10)
			require.NoError(t, table.write(user.InjectOrgID(context.Background(), userID), now.Add(ShardDBsByDuration), batch.(*local.BoltWriteBatch).Writes[table.name]))

			// handover indexes from the table
			require.NoError(t, table.HandoverIndexesToShipper(true))
			require.Len(t, table.dbs, 0)
			require.Len(t, table.dbSnapshots, 0)

			// check that shipper got the new data we handed over
			require.Len(t, indexShipper.addedIndexes[table.name], 2)
			testutil.VerifyIndexes(t, userID, []index.Query{{TableName: table.name}},
				func(ctx context.Context, _ string, callback func(b *bbolt.DB) error) error {
					return indexShipper.ForEach(ctx, table.name, "", func(_ bool, index shipperindex.Index) error {
						return callback(index.(*IndexFile).GetBoltDB())
					})
				},
				0, 20)
		})
	}
}

func Test_LoadBoltDBsFromDir(t *testing.T) {
	indexPath := t.TempDir()

	// setup some dbs with a snapshot file.
	tablePath := testutil.SetupDBsAtPath(t, filepath.Join(indexPath, "test-table"), map[string]testutil.DBConfig{
		"db1": {
			DBRecords: testutil.DBRecords{
				Start:      0,
				NumRecords: 10,
			},
		},
		"db1" + TempFileSuffix: { // a snapshot file which should be ignored.
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
	}, nil)

	// create a boltdb file without bucket which should get removed
	db, err := local.OpenBoltdbFile(filepath.Join(tablePath, "no-bucket"))
	require.NoError(t, err)
	require.NoError(t, db.Close())

	// try loading the dbs
	dbs, err := loadBoltDBsFromDir(tablePath, newTableManagerMetrics(nil))
	require.NoError(t, err)

	// check that we have just 2 dbs
	require.Len(t, dbs, 2)
	require.NotNil(t, dbs["db1"])
	require.NotNil(t, dbs["db2"])

	// close all the open dbs
	for _, db := range dbs {
		require.NoError(t, db.Close())
	}

	dirEntries, err := os.ReadDir(tablePath)
	require.NoError(t, err)
	require.Len(t, dirEntries, 2)
}

func TestTable_ImmutableUploads(t *testing.T) {
	tempDir := t.TempDir()

	indexShipper := newMockIndexShipper()
	indexPath := filepath.Join(tempDir, indexDirName)

	// shardCutoff is calculated based on when shards are considered to not be active anymore and are safe to be
	// handed over to shipper for uploading.
	shardCutoff := getOldestActiveShardTime()

	// some dbs to setup
	dbNames := []int64{
		shardCutoff.Add(-ShardDBsByDuration).Unix(),    // inactive shard, should handover
		shardCutoff.Add(-1 * time.Minute).Unix(),       // 1 minute before shard cutoff, should handover
		time.Now().Truncate(ShardDBsByDuration).Unix(), // active shard, should not handover
	}

	dbs := map[string]testutil.DBConfig{}
	for _, dbName := range dbNames {
		dbs[fmt.Sprint(dbName)] = testutil.DBConfig{
			DBRecords: testutil.DBRecords{
				NumRecords: 10,
			},
		}
	}

	// setup some dbs for a table at a path.
	tableName := "test-table"
	tablePath := testutil.SetupDBsAtPath(t, filepath.Join(indexPath, tableName), dbs, nil)

	table, err := LoadTable(tablePath, "test", indexShipper, false, newTableManagerMetrics(nil))
	require.NoError(t, err)
	require.NotNil(t, table)

	defer func() {
		table.Stop()
	}()

	// db expected to be handed over without forcing it
	expectedDBsToHandedOver := []int64{dbNames[0], dbNames[1]}

	// handover dbs without forcing it which should not handover active shard or shard which has been active upto a minute back.
	require.NoError(t, table.HandoverIndexesToShipper(false))

	mockIndexShipper := table.indexShipper.(*mockIndexShipper)

	// verify that only expected dbs are handed over
	require.Len(t, mockIndexShipper.addedIndexes, 1)
	require.Len(t, mockIndexShipper.addedIndexes[table.name], len(expectedDBsToHandedOver))
	for _, expectedDB := range expectedDBsToHandedOver {
		require.True(t, mockIndexShipper.hasIndex(tableName, table.buildFileName(fmt.Sprint(expectedDB))))
	}

	// force handover of dbs
	require.NoError(t, table.HandoverIndexesToShipper(true))
	expectedDBsToHandedOver = dbNames

	// verify that all the dbs are handed over
	require.Len(t, mockIndexShipper.addedIndexes, 1)
	require.Len(t, mockIndexShipper.addedIndexes[table.name], len(expectedDBsToHandedOver))
	for _, expectedDB := range expectedDBsToHandedOver {
		require.True(t, mockIndexShipper.hasIndex(tableName, table.buildFileName(fmt.Sprint(expectedDB))))
	}

	// clear dbs handed over to shipper
	mockIndexShipper.addedIndexes = map[string][]shipperindex.Index{}

	// force handover of dbs
	require.NoError(t, table.HandoverIndexesToShipper(true))

	// make sure nothing was added to shipper again
	require.Len(t, mockIndexShipper.addedIndexes, 0)
}

func TestTable_MultiQueries(t *testing.T) {
	indexPath := t.TempDir()

	boltDBIndexClient, err := local.NewBoltDBIndexClient(local.BoltDBConfig{Directory: indexPath})
	require.NoError(t, err)

	defer func() {
		boltDBIndexClient.Stop()
	}()

	user1, user2 := "user1", "user2"

	// setup some dbs with default bucket and per tenant bucket for a table at a path.
	tablePath := filepath.Join(indexPath, "test-table")
	testutil.SetupDBsAtPath(t, tablePath, map[string]testutil.DBConfig{
		"db1": {
			DBRecords: testutil.DBRecords{
				NumRecords: 10,
			},
		},
		"db2": {
			DBRecords: testutil.DBRecords{
				Start:      10,
				NumRecords: 10,
			},
		},
	}, nil)
	testutil.SetupDBsAtPath(t, tablePath, map[string]testutil.DBConfig{
		"db3": {
			DBRecords: testutil.DBRecords{
				Start:      20,
				NumRecords: 10,
			},
		},
		"db4": {
			DBRecords: testutil.DBRecords{
				Start:      30,
				NumRecords: 10,
			},
		},
	}, []byte(user1))

	// try loading the table.
	table, err := LoadTable(tablePath, "test", newMockIndexShipper(), false, newTableManagerMetrics(nil))
	require.NoError(t, err)
	require.NotNil(t, table)
	defer func() {
		table.Stop()
	}()

	require.NoError(t, table.Snapshot())

	// build queries each looking for specific value from all the dbs
	var queries []index.Query
	for i := 5; i < 35; i++ {
		queries = append(queries, index.Query{TableName: table.name, ValueEqual: []byte(strconv.Itoa(i))})
	}

	// querying data for user1 should return both data from common index and user1's index
	testutil.VerifyIndexes(t, user1, queries,
		func(ctx context.Context, _ string, callback func(b *bbolt.DB) error) error {
			return table.ForEach(ctx, callback)
		},
		5, 30)

	// querying data for user2 should return only common index
	testutil.VerifyIndexes(t, user2, queries,
		func(ctx context.Context, _ string, callback func(b *bbolt.DB) error) error {
			return table.ForEach(ctx, callback)
		},
		5, 15)
}
