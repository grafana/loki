package testutil

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/grafana/dskit/tenant"
	"github.com/grafana/dskit/user"
	"github.com/klauspost/compress/gzip"
	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	chunk_util "github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
)

func AddRecordsToDB(t testing.TB, path string, start, numRecords int, bucketName []byte) {
	t.Helper()
	db, err := local.OpenBoltdbFile(path)
	require.NoError(t, err)

	batch := local.NewWriteBatch()
	AddRecordsToBatch(batch, "test", start, numRecords)

	if len(bucketName) == 0 {
		bucketName = local.IndexBucketName
	}

	require.NoError(t, local.WriteToDB(context.Background(), db, bucketName, batch.(*local.BoltWriteBatch).Writes["test"]))

	require.NoError(t, db.Sync())
	require.NoError(t, db.Close())
}

func AddRecordsToBatch(batch index.WriteBatch, tableName string, start, numRecords int) {
	for i := 0; i < numRecords; i++ {
		rec := []byte(strconv.Itoa(start + i))
		batch.Add(tableName, "", rec, rec)
	}
}

// nolint
func queryIndexes(t *testing.T, ctx context.Context, queries []index.Query, indexIteratorFunc IndexIteratorFunc, callback index.QueryPagesCallback) {
	userID, err := tenant.TenantID(ctx)
	require.NoError(t, err)

	for _, query := range queries {
		err := indexIteratorFunc(ctx, query.TableName, func(boltdb *bbolt.DB) error {
			return queryBoltDB(ctx, boltdb, []byte(userID), []index.Query{query}, callback)
		})
		require.NoError(t, err)
	}
}

type IndexIteratorFunc func(ctx context.Context, table string, callback func(boltdb *bbolt.DB) error) error

func VerifyIndexes(t *testing.T, userID string, queries []index.Query, indexIteratorFunc IndexIteratorFunc, start, numRecords int) {
	t.Helper()
	minValue := start
	maxValue := start + numRecords
	fetchedRecords := make(map[string]string)

	queryIndexes(t, user.InjectOrgID(context.Background(), userID), queries, indexIteratorFunc, makeTestCallback(t, minValue, maxValue, fetchedRecords))
	require.Len(t, fetchedRecords, numRecords)
}

type SingleDBQuerier interface {
	QueryDB(ctx context.Context, db *bbolt.DB, bucketName []byte, query index.Query, callback index.QueryPagesCallback) error
}

func VerifySingleIndexFile(t *testing.T, query index.Query, db *bbolt.DB, bucketName []byte, start, numRecords int) {
	t.Helper()
	minValue := start
	maxValue := start + numRecords
	fetchedRecords := make(map[string]string)

	err := db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketName)
		require.NotNil(t, b)
		return local.QueryWithCursor(context.Background(), b.Cursor(), query, makeTestCallback(t, minValue, maxValue, fetchedRecords))
	})

	require.NoError(t, err)
	require.Len(t, fetchedRecords, numRecords)
}

func makeTestCallback(t *testing.T, minValue, maxValue int, records map[string]string) index.QueryPagesCallback {
	t.Helper()
	recordsMtx := sync.Mutex{}
	return func(_ index.Query, batch index.ReadBatchResult) (shouldContinue bool) {
		itr := batch.Iterator()
		for itr.Next() {
			require.Equal(t, itr.RangeValue(), itr.Value())
			rec, err := strconv.Atoi(string(itr.Value()))

			require.NoError(t, err)
			require.GreaterOrEqual(t, rec, minValue)
			require.LessOrEqual(t, rec, maxValue)

			recordsMtx.Lock()
			records[string(itr.RangeValue())] = string(itr.Value())
			recordsMtx.Unlock()
		}
		return true
	}
}

// ToDo(Sandeep): refactor to remove DBConfig and use DBRecords directly
type DBConfig struct {
	DBRecords
}

type DBRecords struct {
	Start, NumRecords int
}

func SetupDBsAtPath(t *testing.T, path string, dbs map[string]DBConfig, bucketName []byte) string {
	t.Helper()
	boltIndexClient, err := local.NewBoltDBIndexClient(local.BoltDBConfig{Directory: path})
	require.NoError(t, err)

	defer boltIndexClient.Stop()

	require.NoError(t, chunk_util.EnsureDirectory(path))

	for name, dbConfig := range dbs {
		AddRecordsToDB(t, filepath.Join(path, name), dbConfig.Start, dbConfig.NumRecords, bucketName)
	}

	return path
}

func DecompressFile(t *testing.T, src, dest string) {
	t.Helper()
	// open compressed file from storage
	compressedFile, err := os.Open(src)
	require.NoError(t, err)

	// get a compressed reader
	compressedReader, err := gzip.NewReader(compressedFile)
	require.NoError(t, err)

	decompressedFile, err := os.Create(dest)
	require.NoError(t, err)

	// do the decompression
	_, err = io.Copy(decompressedFile, compressedReader)
	require.NoError(t, err)

	// close the references
	require.NoError(t, compressedFile.Close())
	require.NoError(t, decompressedFile.Close())
}

type DBsConfig struct {
	DBRecordsStart                     int
	NumUnCompactedDBs, NumCompactedDBs int
}

func (c DBsConfig) String() string {
	return fmt.Sprintf("Default Bucket DBs - UCDBs: %d, CDBs: %d", c.NumUnCompactedDBs, c.NumCompactedDBs)
}

type PerUserDBsConfig struct {
	DBsConfig
	NumUsers int
}

func (c PerUserDBsConfig) String() string {
	return fmt.Sprintf("Per User DBs - UCDBs: %d, CDBs: %d, Users: %d", c.NumUnCompactedDBs, c.NumCompactedDBs, c.NumUsers)
}

func SetupTable(t *testing.T, path string, commonDBsConfig DBsConfig, perUserDBsConfig PerUserDBsConfig) {
	numRecordsPerDB := 100

	commonDBsWithDefaultBucket := map[string]DBConfig{}
	commonDBsWithPerUserBucket := map[string]map[string]DBConfig{}
	perUserDBs := map[string]map[string]DBConfig{}

	for i := 0; i < commonDBsConfig.NumUnCompactedDBs; i++ {
		commonDBsWithDefaultBucket[fmt.Sprint(i)] = DBConfig{
			DBRecords: DBRecords{
				Start:      commonDBsConfig.DBRecordsStart + i*numRecordsPerDB,
				NumRecords: numRecordsPerDB,
			},
		}
	}

	for i := 0; i < commonDBsConfig.NumCompactedDBs; i++ {
		commonDBsWithDefaultBucket[fmt.Sprintf("compactor-%d", i)] = DBConfig{
			DBRecords: DBRecords{
				Start:      commonDBsConfig.DBRecordsStart + i*numRecordsPerDB,
				NumRecords: ((i + 1) * numRecordsPerDB) * 2,
			},
		}
	}

	for i := 0; i < perUserDBsConfig.NumUnCompactedDBs; i++ {
		dbName := fmt.Sprintf("per-user-bucket-db-%d", i)
		commonDBsWithPerUserBucket[dbName] = map[string]DBConfig{}
		for j := 0; j < perUserDBsConfig.NumUsers; j++ {
			commonDBsWithPerUserBucket[dbName][BuildUserID(j)] = DBConfig{
				DBRecords: DBRecords{
					Start:      perUserDBsConfig.DBRecordsStart + i*numRecordsPerDB,
					NumRecords: numRecordsPerDB,
				},
			}
		}
	}

	for i := 0; i < perUserDBsConfig.NumCompactedDBs; i++ {
		for j := 0; j < perUserDBsConfig.NumUsers; j++ {
			userID := BuildUserID(j)
			if i == 0 {
				perUserDBs[userID] = map[string]DBConfig{}
			}
			perUserDBs[userID][fmt.Sprintf("compactor-%d", i)] = DBConfig{
				DBRecords: DBRecords{
					Start:      perUserDBsConfig.DBRecordsStart + i*numRecordsPerDB,
					NumRecords: (i + 1) * numRecordsPerDB,
				},
			}
		}
	}

	SetupDBsAtPath(t, path, commonDBsWithDefaultBucket, local.IndexBucketName)

	for dbName, userRecords := range commonDBsWithPerUserBucket {
		for userID, dbConfig := range userRecords {
			SetupDBsAtPath(t, path, map[string]DBConfig{
				dbName: dbConfig,
			}, []byte(userID))
		}
	}

	for userID, dbRecords := range perUserDBs {
		SetupDBsAtPath(t, filepath.Join(path, userID), dbRecords, local.IndexBucketName)
	}
}

func BuildUserID(id int) string {
	return fmt.Sprintf("user-%d", id)
}

func queryBoltDB(ctx context.Context, db *bbolt.DB, userID []byte, queries []index.Query, callback index.QueryPagesCallback) error {
	return db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(userID)
		if bucket == nil {
			bucket = tx.Bucket(local.IndexBucketName)
			if bucket == nil {
				return nil
			}
		}

		for _, query := range queries {
			if err := local.QueryWithCursor(ctx, bucket.Cursor(), query, callback); err != nil {
				return err
			}
		}
		return nil
	})
}
