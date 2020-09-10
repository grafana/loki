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

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	"github.com/klauspost/compress/gzip"
	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"
)

var boltBucketName = []byte("index")

func AddRecordsToDB(t *testing.T, path string, dbClient *local.BoltIndexClient, start, numRecords int) {
	db, err := local.OpenBoltdbFile(path)
	require.NoError(t, err)

	batch := dbClient.NewWriteBatch()
	AddRecordsToBatch(batch, "test", start, numRecords)

	require.NoError(t, dbClient.WriteToDB(context.Background(), db, batch.(*local.BoltWriteBatch).Writes["test"]))

	require.NoError(t, db.Sync())
	require.NoError(t, db.Close())
}

func AddRecordsToBatch(batch chunk.WriteBatch, tableName string, start, numRecords int) {
	for i := 0; i < numRecords; i++ {
		rec := []byte(strconv.Itoa(start + i))
		batch.Add(tableName, "", rec, rec)
	}

	return
}

type SingleTableQuerier interface {
	Query(ctx context.Context, query chunk.IndexQuery, callback chunk_util.Callback) error
}

func TestSingleQuery(t *testing.T, query chunk.IndexQuery, querier SingleTableQuerier, start, numRecords int) {
	minValue := start
	maxValue := start + numRecords
	fetchedRecords := make(map[string]string)

	err := querier.Query(context.Background(), query, makeTestCallback(t, minValue, maxValue, fetchedRecords))

	require.NoError(t, err)
	require.Len(t, fetchedRecords, numRecords)
}

type SingleDBQuerier interface {
	QueryDB(ctx context.Context, db *bbolt.DB, query chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error
}

func TestSingleDBQuery(t *testing.T, query chunk.IndexQuery, db *bbolt.DB, querier SingleDBQuerier, start, numRecords int) {
	minValue := start
	maxValue := start + numRecords
	fetchedRecords := make(map[string]string)

	err := querier.QueryDB(context.Background(), db, query, makeTestCallback(t, minValue, maxValue, fetchedRecords))

	require.NoError(t, err)
	require.Len(t, fetchedRecords, numRecords)
}

type MultiTableQuerier interface {
	QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback chunk_util.Callback) error
}

func TestMultiTableQuery(t *testing.T, queries []chunk.IndexQuery, querier MultiTableQuerier, start, numRecords int) {
	minValue := start
	maxValue := start + numRecords
	fetchedRecords := make(map[string]string)

	err := querier.QueryPages(context.Background(), queries, makeTestCallback(t, minValue, maxValue, fetchedRecords))

	require.NoError(t, err)
	require.Len(t, fetchedRecords, numRecords)
}

func makeTestCallback(t *testing.T, minValue, maxValue int, records map[string]string) func(query chunk.IndexQuery, batch chunk.ReadBatch) (shouldContinue bool) {
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
	db1Records := readDB(t, db1)
	db2Records := readDB(t, db2)

	require.Equal(t, db1Records, db2Records)
}

func readDB(t *testing.T, db *bbolt.DB) map[string]string {
	dbRecords := map[string]string{}

	err := db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(boltBucketName)
		require.NotNil(t, b)

		return b.ForEach(func(k, v []byte) error {
			dbRecords[string(k)] = string(v)
			return nil
		})
	})

	require.NoError(t, err)
	return dbRecords
}

type DBRecords struct {
	Start, NumRecords int
}

func SetupDBTablesAtPath(t *testing.T, tableName, path string, dbs map[string]DBRecords, compressRandomFiles bool) string {
	boltIndexClient, err := local.NewBoltDBIndexClient(local.BoltDBConfig{Directory: path})
	require.NoError(t, err)

	defer boltIndexClient.Stop()

	tablePath := filepath.Join(path, tableName)
	require.NoError(t, chunk_util.EnsureDirectory(tablePath))

	var i int
	for name, dbRecords := range dbs {
		AddRecordsToDB(t, filepath.Join(tablePath, name), boltIndexClient, dbRecords.Start, dbRecords.NumRecords)
		if compressRandomFiles && i%2 == 0 {
			compressFile(t, filepath.Join(tablePath, name))
		}
		i++
	}

	return tablePath
}

func compressFile(t *testing.T, filepath string) {
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
