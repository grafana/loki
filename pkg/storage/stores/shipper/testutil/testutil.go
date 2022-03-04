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

	"github.com/klauspost/compress/gzip"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/local"
	chunk_util "github.com/grafana/loki/pkg/storage/chunk/util"
)

func AddRecordsToDB(t testing.TB, path string, dbClient *local.BoltIndexClient, start, numRecords int, bucketName []byte) {
	t.Helper()
	db, err := local.OpenBoltdbFile(path)
	require.NoError(t, err)

	batch := dbClient.NewWriteBatch()
	AddRecordsToBatch(batch, "test", start, numRecords)

	if len(bucketName) == 0 {
		bucketName = local.IndexBucketName
	}

	require.NoError(t, dbClient.WriteToDB(context.Background(), db, bucketName, batch.(*local.BoltWriteBatch).Writes["test"]))

	require.NoError(t, db.Sync())
	require.NoError(t, db.Close())
}

func AddRecordsToBatch(batch chunk.WriteBatch, tableName string, start, numRecords int) {
	for i := 0; i < numRecords; i++ {
		rec := []byte(strconv.Itoa(start + i))
		batch.Add(tableName, "", rec, rec)
	}
}

type SingleTableQuerier interface {
	MultiQueries(ctx context.Context, queries []chunk.IndexQuery, callback chunk.QueryPagesCallback) error
}

func TestSingleTableQuery(t *testing.T, userID string, queries []chunk.IndexQuery, querier SingleTableQuerier, start, numRecords int) {
	t.Helper()
	minValue := start
	maxValue := start + numRecords
	fetchedRecords := make(map[string]string)

	err := querier.MultiQueries(user.InjectOrgID(context.Background(), userID), queries, makeTestCallback(t, minValue, maxValue, fetchedRecords))

	require.NoError(t, err)
	require.Len(t, fetchedRecords, numRecords)
}

type SingleDBQuerier interface {
	QueryDB(ctx context.Context, db *bbolt.DB, bucketName []byte, query chunk.IndexQuery, callback chunk.QueryPagesCallback) error
}

func TestSingleDBQuery(t *testing.T, query chunk.IndexQuery, db *bbolt.DB, bucketName []byte, querier SingleDBQuerier, start, numRecords int) {
	t.Helper()
	minValue := start
	maxValue := start + numRecords
	fetchedRecords := make(map[string]string)

	err := querier.QueryDB(context.Background(), db, bucketName, query, makeTestCallback(t, minValue, maxValue, fetchedRecords))

	require.NoError(t, err)
	require.Len(t, fetchedRecords, numRecords)
}

type MultiTableQuerier interface {
	QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback chunk.QueryPagesCallback) error
}

func TestMultiTableQuery(t *testing.T, userID string, queries []chunk.IndexQuery, querier MultiTableQuerier, start, numRecords int) {
	t.Helper()
	minValue := start
	maxValue := start + numRecords
	fetchedRecords := make(map[string]string)

	err := querier.QueryPages(user.InjectOrgID(context.Background(), userID), queries, makeTestCallback(t, minValue, maxValue, fetchedRecords))

	require.NoError(t, err)
	require.Len(t, fetchedRecords, numRecords)
}

func makeTestCallback(t *testing.T, minValue, maxValue int, records map[string]string) chunk.QueryPagesCallback {
	t.Helper()
	recordsMtx := sync.Mutex{}
	return func(query chunk.IndexQuery, batch chunk.ReadBatch) (shouldContinue bool) {
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

func CompareDBs(t *testing.T, db1, db2 *bbolt.DB) {
	t.Helper()
	db1Records := readDB(t, db1)
	db2Records := readDB(t, db2)

	require.Equal(t, db1Records, db2Records)
}

func readDB(t *testing.T, db *bbolt.DB) map[string]map[string]string {
	t.Helper()
	dbRecords := map[string]map[string]string{}

	err := db.View(func(tx *bbolt.Tx) error {
		return tx.ForEach(func(name []byte, b *bbolt.Bucket) error {
			dbRecords[string(name)] = map[string]string{}
			return b.ForEach(func(k, v []byte) error {
				dbRecords[string(name)][string(k)] = string(v)
				return nil
			})
		})
	})

	require.NoError(t, err)
	return dbRecords
}

type DBConfig struct {
	CompressFile bool
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
		AddRecordsToDB(t, filepath.Join(path, name), boltIndexClient, dbConfig.Start, dbConfig.NumRecords, bucketName)
		if dbConfig.CompressFile {
			compressFile(t, filepath.Join(path, name))
		}
	}

	return path
}

func compressFile(t *testing.T, filepath string) {
	t.Helper()
	uncompressedFile, err := os.Open(filepath)
	require.NoError(t, err)

	compressedFile, err := os.Create(fmt.Sprintf("%s.gz", filepath))
	require.NoError(t, err)

	compressedWriter := gzip.NewWriter(compressedFile)

	_, err = io.Copy(compressedWriter, uncompressedFile)
	require.NoError(t, err)

	require.NoError(t, compressedWriter.Close())
	require.NoError(t, uncompressedFile.Close())
	require.NoError(t, compressedFile.Close())
	require.NoError(t, os.Remove(filepath))
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
			CompressFile: i%2 == 0,
			DBRecords: DBRecords{
				Start:      commonDBsConfig.DBRecordsStart + i*numRecordsPerDB,
				NumRecords: numRecordsPerDB,
			},
		}
	}

	for i := 0; i < commonDBsConfig.NumCompactedDBs; i++ {
		commonDBsWithDefaultBucket[fmt.Sprintf("compactor-%d", i)] = DBConfig{
			CompressFile: i%2 == 0,
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
				CompressFile: i%2 == 0,
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
