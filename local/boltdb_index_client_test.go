package local

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"
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
