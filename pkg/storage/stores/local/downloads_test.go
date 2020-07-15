package local

// import (
// 	"context"
// 	"io/ioutil"
// 	"os"
// 	"path/filepath"
// 	"strconv"
// 	"testing"
// 	"time"

// 	"github.com/cortexproject/cortex/pkg/chunk/local"

// 	"github.com/cortexproject/cortex/pkg/chunk"
// 	"github.com/stretchr/testify/require"
// )

// func queryTestBoltdb(t *testing.T, boltdbIndexClient *BoltdbIndexClientWithShipper, query chunk.IndexQuery) map[string]string {
// 	resp := map[string]string{}

// 	require.NoError(t, boltdbIndexClient.query(context.Background(), query, func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
// 		itr := batch.Iterator()
// 		for itr.Next() {
// 			resp[string(itr.RangeValue())] = string(itr.Value())
// 		}
// 		return true
// 	}))

// 	return resp
// }

// func writeTestData(t *testing.T, indexClient *BoltdbIndexClientWithShipper, tableName string, numRecords, startValue int) {
// 	time.Sleep(time.Second / 2)

// 	batch := indexClient.NewWriteBatch()
// 	for i := 0; i < numRecords; i++ {
// 		value := []byte(strconv.Itoa(startValue + i))
// 		batch.Add(tableName, "", value, value)
// 	}

// 	require.NoError(t, indexClient.BatchWrite(context.Background(), batch))

// 	boltdb, err := indexClient.GetDB(tableName, local.DBOperationWrite)
// 	require.NoError(t, err)

// 	require.NoError(t, boltdb.Sync())
// }

// func TestShipper_Downloads(t *testing.T) {
// 	tempDirForTests, err := ioutil.TempDir("", "test-dir")
// 	require.NoError(t, err)

// 	defer func() {
// 		require.NoError(t, os.RemoveAll(tempDirForTests))
// 	}()

// 	localStoreLocation, err := ioutil.TempDir(tempDirForTests, "local-store")
// 	require.NoError(t, err)

// 	boltDBWithShipper1 := createTestBoltDBWithShipper(t, tempDirForTests, "ingester1", localStoreLocation)
// 	boltDBWithShipper2 := createTestBoltDBWithShipper(t, tempDirForTests, "ingester2", localStoreLocation)

// 	// add a file to boltDBWithShipper1
// 	writeTestData(t, boltDBWithShipper1, "1", 10, 0)

// 	// upload files from boltDBWithShipper1
// 	require.NoError(t, boltDBWithShipper1.shipper.uploadFiles(context.Background()))

// 	// query data for same table from boltDBWithShipper2
// 	resp := queryTestBoltdb(t, boltDBWithShipper2, chunk.IndexQuery{
// 		TableName: "1",
// 	})

// 	// make sure we got same data that was added from boltDBWithShipper1
// 	checkExpectedKVsInBoltdbResp(t, resp, 10, 0)

// 	// add more data to the previous file added to boltDBWithShipper1 and the upload it
// 	writeTestData(t, boltDBWithShipper1, "1", 10, 10)
// 	require.NoError(t, boltDBWithShipper1.shipper.uploadFiles(context.Background()))

// 	// sync files in boltDBWithShipper2
// 	require.NoError(t, boltDBWithShipper2.shipper.syncLocalWithStorage(context.Background()))

// 	// query data for same table from boltDBWithShipper2
// 	resp = queryTestBoltdb(t, boltDBWithShipper2, chunk.IndexQuery{
// 		TableName: "1",
// 	})

// 	// make sure we also got new data that was added from boltDBWithShipper1
// 	checkExpectedKVsInBoltdbResp(t, resp, 20, 0)

// 	// add some data for same table in boltDBWithShipper2
// 	writeTestData(t, boltDBWithShipper2, "1", 10, 20)

// 	// query data for same table from boltDBWithShipper2
// 	resp = queryTestBoltdb(t, boltDBWithShipper2, chunk.IndexQuery{
// 		TableName: "1",
// 	})

// 	// make sure we data from boltDBWithShipper1 and boltDBWithShipper2
// 	checkExpectedKVsInBoltdbResp(t, resp, 30, 0)

// 	// stop boltDBWithShipper1
// 	boltDBWithShipper1.Stop()

// 	// delete the file from the store that was uploaded by boltDBWithShipper1
// 	require.NoError(t, os.Remove(filepath.Join(localStoreLocation, storageKeyPrefix, "1", boltDBWithShipper1.shipper.uploader)))

// 	// sync files in boltDBWithShipper2
// 	require.NoError(t, boltDBWithShipper2.shipper.syncLocalWithStorage(context.Background()))

// 	// query data for same table from boltDBWithShipper2
// 	resp = queryTestBoltdb(t, boltDBWithShipper2, chunk.IndexQuery{
// 		TableName: "1",
// 	})

// 	// make sure we got only data that was added to boltDBWithShipper2
// 	checkExpectedKVsInBoltdbResp(t, resp, 10, 20)

// 	boltDBWithShipper2.Stop()
// }
