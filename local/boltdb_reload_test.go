package local

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/etcd-io/bbolt"
	"github.com/stretchr/testify/require"
)

var (
	testKey   = []byte("test-key")
	testValue = []byte("test-value")
)

func setupDb(t *testing.T, boltdbIndexClient *boltIndexClient, dbname string) {
	db, err := boltdbIndexClient.getDB(dbname)
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
	if err != nil {
		return
	}

	indexClient, err := NewBoltDBIndexClient(BoltDBConfig{
		Directory: dirname,
	})

	testDb1 := "test1"
	testDb2 := "test2"

	boltdbIndexClient := indexClient.(*boltIndexClient)
	setupDb(t, boltdbIndexClient, testDb1)
	setupDb(t, boltdbIndexClient, testDb2)

	boltdbIndexClient.reload()
	require.Equal(t, 2, len(boltdbIndexClient.dbs), "There should be 2 boltdbs open")

	require.NoError(t, os.Remove(filepath.Join(dirname, testDb1)))

	droppedDb, err := boltdbIndexClient.getDB(testDb1)
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

	boltdbIndexClient.Stop()
	require.NoError(t, os.RemoveAll(dirname))
}
