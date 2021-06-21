package local

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/storage/chunk"
)

var (
	testKey   = []byte("test-key")
	testValue = []byte("test-value")
)

func setupDB(t *testing.T, boltdbIndexClient *BoltIndexClient, dbname string) {
	db, err := boltdbIndexClient.GetDB(dbname, DBOperationWrite)
	require.NoError(t, err)

	err = db.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(bucketName)
		if err != nil {
			return err
		}

		return b.Put(testKey, testValue)
	})
	require.NoError(t, err)
}

func TestBoltDBReload(t *testing.T) {
	dirname, err := ioutil.TempDir(os.TempDir(), "boltdb")
	require.NoError(t, err)

	defer require.NoError(t, os.RemoveAll(dirname))

	boltdbIndexClient, err := NewBoltDBIndexClient(BoltDBConfig{
		Directory: dirname,
	})
	require.NoError(t, err)

	defer boltdbIndexClient.Stop()

	testDb1 := "test1"
	testDb2 := "test2"

	setupDB(t, boltdbIndexClient, testDb1)
	setupDB(t, boltdbIndexClient, testDb2)

	boltdbIndexClient.reload()
	require.Equal(t, 2, len(boltdbIndexClient.dbs), "There should be 2 boltdbs open")

	require.NoError(t, os.Remove(filepath.Join(dirname, testDb1)))

	droppedDb, err := boltdbIndexClient.GetDB(testDb1, DBOperationRead)
	require.NoError(t, err)

	valueFromDb := []byte{}
	_ = droppedDb.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketName)
		valueFromDb = b.Get(testKey)
		return nil
	})
	require.Equal(t, testValue, valueFromDb, "should match value from db")

	boltdbIndexClient.reload()

	require.Equal(t, 1, len(boltdbIndexClient.dbs), "There should be 1 boltdb open")

	_, err = boltdbIndexClient.GetDB(testDb1, DBOperationRead)
	require.Equal(t, ErrUnexistentBoltDB, err)
}

func TestBoltDB_GetDB(t *testing.T) {
	dirname, err := ioutil.TempDir(os.TempDir(), "boltdb")
	require.NoError(t, err)

	defer require.NoError(t, os.RemoveAll(dirname))

	boltdbIndexClient, err := NewBoltDBIndexClient(BoltDBConfig{
		Directory: dirname,
	})
	require.NoError(t, err)

	// setup a db to already exist
	testDb1 := "test1"
	setupDB(t, boltdbIndexClient, testDb1)

	// check whether an existing db can be fetched for reading
	_, err = boltdbIndexClient.GetDB(testDb1, DBOperationRead)
	require.NoError(t, err)

	// check whether read operation throws ErrUnexistentBoltDB error for db which does not exists
	unexistentDb := "unexistent-db"

	_, err = boltdbIndexClient.GetDB(unexistentDb, DBOperationRead)
	require.Equal(t, ErrUnexistentBoltDB, err)

	// check whether write operation sets up a new db for writing
	db, err := boltdbIndexClient.GetDB(unexistentDb, DBOperationWrite)
	require.NoError(t, err)
	require.NotEqual(t, nil, db)

	// recreate index client to check whether we can read already created test1 db without writing first
	boltdbIndexClient.Stop()
	boltdbIndexClient, err = NewBoltDBIndexClient(BoltDBConfig{
		Directory: dirname,
	})
	require.NoError(t, err)
	defer boltdbIndexClient.Stop()

	_, err = boltdbIndexClient.GetDB(testDb1, DBOperationRead)
	require.NoError(t, err)
}

func Test_CreateTable_BoltdbRW(t *testing.T) {
	tableName := "test"
	dirname, err := ioutil.TempDir(os.TempDir(), "boltdb")
	require.NoError(t, err)

	indexClient, err := NewBoltDBIndexClient(BoltDBConfig{
		Directory: dirname,
	})
	require.NoError(t, err)

	tableClient, err := NewTableClient(dirname)
	require.NoError(t, err)

	err = tableClient.CreateTable(context.Background(), chunk.TableDesc{
		Name: tableName,
	})
	require.NoError(t, err)

	batch := indexClient.NewWriteBatch()
	batch.Add(tableName, fmt.Sprintf("hash%s", "test"), []byte(fmt.Sprintf("range%s", "value")), nil)

	err = indexClient.BatchWrite(context.Background(), batch)
	require.NoError(t, err)

	// try to create the same file which is already existing
	err = tableClient.CreateTable(context.Background(), chunk.TableDesc{
		Name: tableName,
	})
	require.NoError(t, err)

	// make sure file content is not modified
	entry := chunk.IndexQuery{
		TableName: tableName,
		HashValue: fmt.Sprintf("hash%s", "test"),
	}
	var have []chunk.IndexEntry
	err = indexClient.query(context.Background(), entry, func(_ chunk.IndexQuery, read chunk.ReadBatch) bool {
		iter := read.Iterator()
		for iter.Next() {
			have = append(have, chunk.IndexEntry{
				RangeValue: iter.RangeValue(),
			})
		}
		return true
	})
	require.NoError(t, err)
	require.Equal(t, []chunk.IndexEntry{
		{RangeValue: []byte(fmt.Sprintf("range%s", "value"))},
	}, have)
}

func TestBoltDB_Writes(t *testing.T) {
	dirname, err := ioutil.TempDir(os.TempDir(), "boltdb")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(dirname))
	}()

	for i, tc := range []struct {
		name              string
		initialPuts       []string
		testPuts          []string
		testDeletes       []string
		err               error
		valuesAfterWrites []string
	}{
		{
			name:              "just puts",
			testPuts:          []string{"1", "2"},
			valuesAfterWrites: []string{"1", "2"},
		},
		{
			name:              "just deletes",
			initialPuts:       []string{"1", "2", "3", "4"},
			testDeletes:       []string{"1", "2"},
			valuesAfterWrites: []string{"3", "4"},
		},
		{
			name:              "both puts and deletes",
			initialPuts:       []string{"1", "2", "3", "4"},
			testPuts:          []string{"5", "6"},
			testDeletes:       []string{"1", "2"},
			valuesAfterWrites: []string{"3", "4", "5", "6"},
		},
		{
			name:        "deletes without initial writes",
			testDeletes: []string{"1", "2"},
			err:         fmt.Errorf("bucket %s not found in table 3", bucketName),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tableName := fmt.Sprint(i)

			indexClient, err := NewBoltDBIndexClient(BoltDBConfig{
				Directory: dirname,
			})
			require.NoError(t, err)

			defer func() {
				indexClient.Stop()
			}()

			// doing initial writes if there are any
			if len(tc.initialPuts) != 0 {
				batch := indexClient.NewWriteBatch()
				for _, put := range tc.initialPuts {
					batch.Add(tableName, "hash", []byte(put), []byte(put))
				}

				require.NoError(t, indexClient.BatchWrite(context.Background(), batch))
			}

			// doing writes with testPuts and testDeletes
			batch := indexClient.NewWriteBatch()
			for _, put := range tc.testPuts {
				batch.Add(tableName, "hash", []byte(put), []byte(put))
			}
			for _, put := range tc.testDeletes {
				batch.Delete(tableName, "hash", []byte(put))
			}

			require.Equal(t, tc.err, indexClient.BatchWrite(context.Background(), batch))

			// verifying test writes by querying
			var have []chunk.IndexEntry
			err = indexClient.query(context.Background(), chunk.IndexQuery{
				TableName: tableName,
				HashValue: "hash",
			}, func(_ chunk.IndexQuery, read chunk.ReadBatch) bool {
				iter := read.Iterator()
				for iter.Next() {
					have = append(have, chunk.IndexEntry{
						RangeValue: iter.RangeValue(),
						Value:      iter.Value(),
					})
				}
				return true
			})

			require.NoError(t, err)
			require.Len(t, have, len(tc.valuesAfterWrites))

			for i, value := range tc.valuesAfterWrites {
				require.Equal(t, chunk.IndexEntry{
					RangeValue: []byte(value),
					Value:      []byte(value),
				}, have[i])
			}
		})
	}
}
