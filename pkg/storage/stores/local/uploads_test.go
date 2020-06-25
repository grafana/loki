package local

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk/local"
	"github.com/cortexproject/cortex/pkg/chunk/util"
	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"
)

const testBucketName = "testBucket"

func createTestBoltDBWithShipper(t *testing.T, parentTempDir, ingesterName, localStoreLocation string) *BoltdbIndexClientWithShipper {
	cacheLocation := filepath.Join(parentTempDir, ingesterName, "cache")
	boltdbFilesLocation := filepath.Join(parentTempDir, ingesterName, "boltdb")

	require.NoError(t, util.EnsureDirectory(cacheLocation))
	require.NoError(t, util.EnsureDirectory(boltdbFilesLocation))

	shipperConfig := ShipperConfig{
		ActiveIndexDirectory: boltdbFilesLocation,
		CacheLocation:        cacheLocation,
		CacheTTL:             1 * time.Hour,
		ResyncInterval:       1 * time.Hour,
		IngesterName:         ingesterName,
		Mode:                 ShipperModeReadWrite,
	}

	archiveStoreClient, err := local.NewFSObjectClient(local.FSConfig{
		Directory: localStoreLocation,
	})
	require.NoError(t, err)

	boltdbIndexClientWithShipper, err := NewBoltDBIndexClientWithShipper(
		local.BoltDBConfig{Directory: shipperConfig.ActiveIndexDirectory}, archiveStoreClient, shipperConfig, nil)
	require.NoError(t, err)

	return boltdbIndexClientWithShipper.(*BoltdbIndexClientWithShipper)
}

func addTestRecordsToBoltDBFile(t *testing.T, boltdb *bbolt.DB, numRecords int, start int) {
	time.Sleep(time.Second / 2)

	err := boltdb.Update(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(testBucketName))
		if err != nil {
			return err
		}

		for i := 0; i < numRecords; i++ {
			kv := []byte(strconv.Itoa(start + i))

			err = b.Put(kv, kv)
			if err != nil {
				return err
			}
		}

		return nil
	})

	require.NoError(t, err)
	require.NoError(t, boltdb.Sync())
}

func readAllKVsFromBoltdbFile(t *testing.T, boltdb *bbolt.DB) map[string]string {
	resp := map[string]string{}

	err := boltdb.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(testBucketName))
		require.NotNil(t, b)

		return b.ForEach(func(k, v []byte) error {
			resp[string(k)] = string(v)
			return nil
		})
	})

	require.NoError(t, err)

	return resp
}

func readAllKVsFromBoltdbFileAtPath(t *testing.T, path string) map[string]string {
	boltDBFile, err := local.OpenBoltdbFile(path)
	require.NoError(t, err)

	defer func() {
		require.NoError(t, boltDBFile.Close())
	}()

	return readAllKVsFromBoltdbFile(t, boltDBFile)
}

func checkExpectedKVsInBoltdbResp(t *testing.T, resp map[string]string, expectedNumRecords, start int) {
	require.Equal(t, expectedNumRecords, len(resp), "responses", resp)

	for i := 0; i < expectedNumRecords; i++ {
		expectedKV := strconv.Itoa(start + i)

		val, ok := resp[expectedKV]
		require.Equal(t, true, ok)
		require.Equal(t, expectedKV, val)
	}
}

func TestShipper_Uploads(t *testing.T) {
	tempDirForTests, err := ioutil.TempDir("", "test-dir")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(tempDirForTests))
	}()

	localStoreLocation, err := ioutil.TempDir(tempDirForTests, "local-store")
	require.NoError(t, err)

	boltDBWithShipper := createTestBoltDBWithShipper(t, tempDirForTests, "ingester", localStoreLocation)

	// create a boltdb file for boltDBWithShipper to test upload.
	boltdbFile1, err := boltDBWithShipper.GetDB("file1", local.DBOperationWrite)
	require.NoError(t, err)
	file1PathInStorage := filepath.Join(localStoreLocation, storageKeyPrefix, filepath.Base(boltdbFile1.Path()), boltDBWithShipper.shipper.uploader)

	// add some test records to boltdbFile1
	addTestRecordsToBoltDBFile(t, boltdbFile1, 10, 1)

	// Upload files from boltDBWithShipper
	err = boltDBWithShipper.shipper.uploadFiles(context.Background())
	require.NoError(t, err)

	// open boltdbFile1 and verify it has expected records
	checkExpectedKVsInBoltdbResp(t, readAllKVsFromBoltdbFileAtPath(t, file1PathInStorage), 10, 1)

	// create another boltdb file for boltDBWithShipper to test upload.
	boltdbFile2, err := boltDBWithShipper.GetDB("file2", local.DBOperationWrite)
	require.NoError(t, err)
	file2PathInStorage := filepath.Join(localStoreLocation, storageKeyPrefix, filepath.Base(boltdbFile2.Path()), boltDBWithShipper.shipper.uploader)

	// add some test records to boltdbFile2 and some more records to boltdbFile1
	addTestRecordsToBoltDBFile(t, boltdbFile2, 10, 1)
	addTestRecordsToBoltDBFile(t, boltdbFile1, 5, 11)

	// Upload files from boltDBWithShipper
	err = boltDBWithShipper.shipper.uploadFiles(context.Background())
	require.NoError(t, err)

	// open boltdbFile1 and boltdbFile2 and verify it has expected records
	checkExpectedKVsInBoltdbResp(t, readAllKVsFromBoltdbFileAtPath(t, file2PathInStorage), 10, 1)
	checkExpectedKVsInBoltdbResp(t, readAllKVsFromBoltdbFileAtPath(t, file1PathInStorage), 15, 1)

	// modify boltdbFile2 again
	addTestRecordsToBoltDBFile(t, boltdbFile2, 10, 11)

	// stop boltDBWithShipper to make it upload all the new and changed to store
	boltDBWithShipper.Stop()

	checkExpectedKVsInBoltdbResp(t, readAllKVsFromBoltdbFileAtPath(t, file2PathInStorage), 20, 1)
}
