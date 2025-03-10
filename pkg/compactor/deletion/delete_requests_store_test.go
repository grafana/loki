package deletion

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
)

func TestAllDeleteRequestsStoreTypes(t *testing.T) {
	for _, storeType := range []DeleteRequestsStoreDBType{DeleteRequestsStoreDBTypeBoltDB, DeleteRequestsStoreDBTypeSQLite} {
		t.Run(string(storeType), func(t *testing.T) {
			tc := setupStoreType(t, storeType)
			defer tc.store.Stop()

			// add requests for both the users to the store
			for i := 0; i < len(tc.user1Requests); i++ {
				resp, err := tc.store.AddDeleteRequest(
					context.Background(),
					tc.user1Requests[i].UserID,
					tc.user1Requests[i].Query,
					tc.user1Requests[i].StartTime,
					tc.user1Requests[i].EndTime,
					0,
				)
				require.NoError(t, err)
				tc.user1Requests[i].RequestID = resp

				resp, err = tc.store.AddDeleteRequest(
					context.Background(),
					tc.user2Requests[i].UserID,
					tc.user2Requests[i].Query,
					tc.user2Requests[i].StartTime,
					tc.user2Requests[i].EndTime,
					0,
				)
				require.NoError(t, err)
				tc.user2Requests[i].RequestID = resp
			}

			// get all requests with StatusReceived and see if they have expected values
			deleteRequests, err := tc.store.GetUnprocessedShards(context.Background())
			require.NoError(t, err)
			compareRequests(t, append(tc.user1Requests, tc.user2Requests...), deleteRequests)

			// get user specific requests and see if they have expected values
			user1Requests, err := tc.store.GetAllDeleteRequestsForUser(context.Background(), user1, false)
			require.NoError(t, err)
			compareRequests(t, tc.user1Requests, user1Requests)

			user2Requests, err := tc.store.GetAllDeleteRequestsForUser(context.Background(), user2, false)
			require.NoError(t, err)
			compareRequests(t, tc.user2Requests, user2Requests)

			createGenNumber, err := tc.store.GetCacheGenerationNumber(context.Background(), user1)
			require.NoError(t, err)
			require.NotEmpty(t, createGenNumber)

			createGenNumber2, err := tc.store.GetCacheGenerationNumber(context.Background(), user2)
			require.NoError(t, err)
			require.NotEmpty(t, createGenNumber2)

			// get individual delete requests by id and see if they have expected values
			for _, expectedRequest := range append(user1Requests, user2Requests...) {
				actualRequest, err := tc.store.GetDeleteRequest(context.Background(), expectedRequest.UserID, expectedRequest.RequestID)
				require.NoError(t, err)
				require.Equal(t, expectedRequest, actualRequest)
			}

			// try a non-existent request and see if it throws ErrDeleteRequestNotFound
			_, err = tc.store.GetDeleteRequest(context.Background(), "user3", "na")
			require.ErrorIs(t, err, ErrDeleteRequestNotFound)

			// update some of the delete requests for both the users to processed
			for i := 0; i < len(tc.user1Requests); i++ {
				var request DeleteRequest
				if i%2 != 0 {
					tc.user1Requests[i].Status = StatusProcessed
					request = tc.user1Requests[i]
				} else {
					tc.user2Requests[i].Status = StatusProcessed
					request = tc.user2Requests[i]
				}

				require.NoError(t, tc.store.MarkShardAsProcessed(context.Background(), request))
			}

			// see if requests in the store have right values
			user1Requests, err = tc.store.GetAllDeleteRequestsForUser(context.Background(), user1, false)
			require.NoError(t, err)
			compareRequests(t, tc.user1Requests, user1Requests)

			user2Requests, err = tc.store.GetAllDeleteRequestsForUser(context.Background(), user2, false)
			require.NoError(t, err)
			compareRequests(t, tc.user2Requests, user2Requests)

			// caches should not be invalidated when we mark delete request as processed
			updateGenNumber, err := tc.store.GetCacheGenerationNumber(context.Background(), user1)
			require.NoError(t, err)
			require.Equal(t, createGenNumber, updateGenNumber)

			updateGenNumber2, err := tc.store.GetCacheGenerationNumber(context.Background(), user2)
			require.NoError(t, err)
			require.Equal(t, createGenNumber2, updateGenNumber2)

			// delete the requests from the store updated previously
			var remainingRequests []DeleteRequest
			for i := 0; i < len(tc.user1Requests); i++ {
				var request DeleteRequest
				if i%2 != 0 {
					tc.user1Requests[i].Status = StatusProcessed
					request = tc.user1Requests[i]
					remainingRequests = append(remainingRequests, tc.user2Requests[i])
				} else {
					tc.user2Requests[i].Status = StatusProcessed
					request = tc.user2Requests[i]
					remainingRequests = append(remainingRequests, tc.user1Requests[i])
				}

				require.NoError(t, tc.store.RemoveDeleteRequest(context.Background(), request.UserID, request.RequestID))
			}

			// see if the store has the right remaining requests
			deleteRequests, err = tc.store.GetUnprocessedShards(context.Background())
			require.NoError(t, err)
			compareRequests(t, remainingRequests, deleteRequests)

			deleteGenNumber, err := tc.store.GetCacheGenerationNumber(context.Background(), user1)
			require.NoError(t, err)
			require.NotEqual(t, updateGenNumber, deleteGenNumber)

			deleteGenNumber2, err := tc.store.GetCacheGenerationNumber(context.Background(), user2)
			require.NoError(t, err)
			require.NotEqual(t, updateGenNumber2, deleteGenNumber2)
		})
	}
}

func TestBatchCreateGetAllStoreTypes(t *testing.T) {
	for _, storeType := range []DeleteRequestsStoreDBType{DeleteRequestsStoreDBTypeBoltDB, DeleteRequestsStoreDBTypeSQLite} {
		t.Run(string(storeType), func(t *testing.T) {
			t.Run("it adds the requests with the same request id, status, and creation time", func(t *testing.T) {
				tc := setupStoreType(t, storeType)
				defer tc.store.Stop()

				reqID, err := tc.store.AddDeleteRequest(context.Background(), user1, `{foo="bar"}`, now.Add(-24*time.Hour), now, time.Hour)
				require.NoError(t, err)

				requests, err := tc.store.GetUnprocessedShards(context.Background())
				require.NoError(t, err)

				// ensure that creation time is set close to now
				require.InDelta(t, int64(model.Now()), int64(requests[0].CreatedAt), float64(5*time.Second))

				for _, req := range requests {
					require.Equal(t, reqID, requests[0].RequestID)
					require.Equal(t, req.Status, requests[0].Status)
					require.Equal(t, req.CreatedAt, requests[0].CreatedAt)
					require.Equal(t, req.Query, requests[0].Query)
					require.Equal(t, req.UserID, requests[0].UserID)
				}
			})

			t.Run("mark a single request as processed", func(t *testing.T) {
				tc := setupStoreType(t, storeType)
				defer tc.store.Stop()

				reqID, err := tc.store.AddDeleteRequest(context.Background(), user1, `{foo="bar"}`, now.Add(-24*time.Hour), now, time.Hour)
				require.NoError(t, err)

				savedRequests, err := tc.store.GetUnprocessedShards(context.Background())
				require.NoError(t, err)

				err = tc.store.MarkShardAsProcessed(context.Background(), savedRequests[1])
				require.NoError(t, err)

				results, err := tc.store.GetUnprocessedShards(context.Background())
				require.NoError(t, err)

				require.Len(t, results, len(savedRequests)-1)
				require.Equal(t, append(savedRequests[0:1], savedRequests[2:]...), results)

				req, err := tc.store.GetDeleteRequest(context.Background(), user1, reqID)
				require.NoError(t, err)
				require.Equal(t, deleteRequestStatus(1, len(savedRequests)), req.Status)
			})

			t.Run("deletes several delete requests", func(t *testing.T) {
				tc := setupStoreType(t, storeType)
				defer tc.store.Stop()

				reqID, err := tc.store.AddDeleteRequest(context.Background(), user1, `{foo="bar"}`, now.Add(-24*time.Hour), now, time.Hour)
				require.NoError(t, err)

				err = tc.store.RemoveDeleteRequest(context.Background(), user1, reqID)
				require.NoError(t, err)

				results, err := tc.store.GetDeleteRequest(context.Background(), user1, reqID)
				require.ErrorIs(t, err, ErrDeleteRequestNotFound)
				require.Empty(t, results)
			})
		})
	}
}

func TestCopyData(t *testing.T) {
	tempDir := t.TempDir()
	workingDir := filepath.Join(tempDir, "working-dir")
	objectStorePath := filepath.Join(tempDir, "object-store")

	objectClient, err := local.NewFSObjectClient(local.FSConfig{
		Directory: objectStorePath,
	})
	require.NoError(t, err)

	indexStorageClient := storage.NewIndexStorageClient(objectClient, "")

	// First create and populate a boltdb store
	boltdbStore, err := NewDeleteRequestsStore(DeleteRequestsStoreDBTypeBoltDB, workingDir, indexStorageClient, "", time.Hour)
	require.NoError(t, err)

	// Add some test data to boltdb
	reqID1, err := boltdbStore.AddDeleteRequest(context.Background(), user1, `{foo="bar1"}`, now.Add(-24*time.Hour), now, time.Hour)
	require.NoError(t, err)
	reqID2, err := boltdbStore.AddDeleteRequest(context.Background(), user2, `{foo="bar2"}`, now.Add(-48*time.Hour), now, 2*time.Hour)
	require.NoError(t, err)
	reqID3, err := boltdbStore.AddDeleteRequest(context.Background(), user2, `{foo="bar3"}`, now.Add(-48*time.Hour), now, time.Hour)
	require.NoError(t, err)

	// Mark all shards for reqID1 as processed
	requests, err := boltdbStore.GetUnprocessedShards(context.Background())
	require.NoError(t, err)
	for _, req := range requests {
		if req.RequestID != reqID1 {
			continue
		}
		err = boltdbStore.MarkShardAsProcessed(context.Background(), req)
		require.NoError(t, err)
	}

	// Mark just one of the shards for reqID2 as processed
	for _, req := range requests {
		if req.RequestID == reqID2 {
			err = boltdbStore.MarkShardAsProcessed(context.Background(), req)
			require.NoError(t, err)
			break
		}
	}

	// Get all requests from boltdb for comparison
	boltdbRequests, err := boltdbStore.GetAllRequests(context.Background())
	require.NoError(t, err)
	require.Len(t, boltdbRequests, 3)

	// close boltdbStore since we can't open same boltdb file multiple times
	boltdbStore.Stop()

	// Create sqlite store which should automatically copy data from boltdb
	sqliteStore, err := NewDeleteRequestsStore(DeleteRequestsStoreDBTypeSQLite, workingDir, indexStorageClient, "", time.Hour)
	require.NoError(t, err)
	defer sqliteStore.Stop()

	boltdbStore, err = NewDeleteRequestsStore(DeleteRequestsStoreDBTypeBoltDB, workingDir, indexStorageClient, "", time.Hour)
	require.NoError(t, err)
	defer boltdbStore.Stop()

	// Verify data was copied correctly
	t.Run("verify all requests were copied", func(t *testing.T) {
		sqliteRequests, err := sqliteStore.GetAllRequests(context.Background())
		require.NoError(t, err)
		require.Len(t, sqliteRequests, len(boltdbRequests))

		// Reset sequence numbers since they're boltdb specific
		resetSequenceNumInRequests(sqliteRequests)
		resetSequenceNumInRequests(boltdbRequests)

		require.ElementsMatch(t, boltdbRequests, sqliteRequests)
	})

	t.Run("verify request status was preserved", func(t *testing.T) {
		// Check specific requests
		req1, err := sqliteStore.GetDeleteRequest(context.Background(), user1, reqID1)
		require.NoError(t, err)
		req2, err := sqliteStore.GetDeleteRequest(context.Background(), user2, reqID2)
		require.NoError(t, err)
		req3, err := sqliteStore.GetDeleteRequest(context.Background(), user2, reqID3)
		require.NoError(t, err)

		// req1 should be processed, req2 should be partially processed and req3 should not have been processed at all
		require.True(t, req1.Status == StatusProcessed)
		require.True(t, req2.Status != StatusReceived && req2.Status != StatusProcessed)
		require.True(t, req3.Status == StatusReceived)
	})

	t.Run("verify unprocessed shards were copied", func(t *testing.T) {
		boltdbShards, err := boltdbStore.GetUnprocessedShards(context.Background())
		require.NoError(t, err)

		sqliteShards, err := sqliteStore.GetUnprocessedShards(context.Background())
		require.NoError(t, err)

		require.Len(t, sqliteShards, len(boltdbShards))

		// Reset sequence numbers for comparison
		resetSequenceNumInRequests(boltdbShards)

		require.ElementsMatch(t, boltdbShards, sqliteShards)
	})

	t.Run("verify cache generation numbers were copied", func(t *testing.T) {
		boltdbGen1, err := boltdbStore.GetCacheGenerationNumber(context.Background(), user1)
		require.NoError(t, err)
		sqliteGen1, err := sqliteStore.GetCacheGenerationNumber(context.Background(), user1)
		require.NoError(t, err)
		require.Equal(t, boltdbGen1, sqliteGen1)

		boltdbGen2, err := boltdbStore.GetCacheGenerationNumber(context.Background(), user2)
		require.NoError(t, err)
		sqliteGen2, err := sqliteStore.GetCacheGenerationNumber(context.Background(), user2)
		require.NoError(t, err)
		require.Equal(t, boltdbGen2, sqliteGen2)
	})
}

func TestNewDeleteRequestsStoreTee(t *testing.T) {
	tempDir := t.TempDir()

	workingDir := filepath.Join(tempDir, "working-dir")
	objectStorePath := filepath.Join(tempDir, "object-store")

	objectClient, err := local.NewFSObjectClient(local.FSConfig{
		Directory: objectStorePath,
	})
	require.NoError(t, err)

	indexStorageClient := storage.NewIndexStorageClient(objectClient, "")

	deleteRequestsStore, err := newDeleteRequestsStore(DeleteRequestsStoreDBTypeSQLite, workingDir, indexStorageClient, DeleteRequestsStoreDBTypeBoltDB, time.Hour)
	require.NoError(t, err)
	storeTee := deleteRequestsStore.(deleteRequestsStoreTee)

	t.Run("add and get delete request", func(t *testing.T) {
		reqID, err := storeTee.AddDeleteRequest(context.Background(), user1, `{foo="bar"}`, now.Add(-2*time.Hour), now, time.Hour)
		require.NoError(t, err)

		_, err = storeTee.GetDeleteRequest(context.Background(), user1, reqID)
		require.NoError(t, err)

		requestsFromTee, err := storeTee.GetAllRequests(context.Background())
		require.NoError(t, err)
		require.Len(t, requestsFromTee, 1)

		requestsFromPrimaryStore, err := storeTee.primaryStore.GetAllRequests(context.Background())
		require.NoError(t, err)
		require.Len(t, requestsFromPrimaryStore, 1)
		require.True(t, requestsAreEqual(requestsFromTee[0], requestsFromPrimaryStore[0]))

		requestsFromBackupStore, err := storeTee.backupStore.GetAllRequests(context.Background())
		require.NoError(t, err)
		require.Len(t, requestsFromBackupStore, 1)
		require.True(t, requestsAreEqual(requestsFromTee[0], requestsFromBackupStore[0]))
	})

	t.Run("mark shard as processed", func(t *testing.T) {
		savedShards, err := storeTee.GetUnprocessedShards(context.Background())
		require.NoError(t, err)

		err = storeTee.MarkShardAsProcessed(context.Background(), savedShards[0])
		require.NoError(t, err)

		shardsFromTee, err := storeTee.GetUnprocessedShards(context.Background())
		require.NoError(t, err)
		require.Len(t, shardsFromTee, len(savedShards)-1)
		require.Equal(t, savedShards[1:], shardsFromTee)

		shardsFromPrimaryStore, err := storeTee.primaryStore.GetUnprocessedShards(context.Background())
		require.NoError(t, err)
		require.Len(t, shardsFromPrimaryStore, len(savedShards)-1)
		require.Equal(t, savedShards[1:], shardsFromPrimaryStore)

		shardsFromBackupStore, err := storeTee.backupStore.GetUnprocessedShards(context.Background())
		require.NoError(t, err)
		require.Len(t, shardsFromBackupStore, len(savedShards)-1)
		resetSequenceNumInRequests(shardsFromBackupStore)
		compareRequests(t, savedShards[1:], shardsFromBackupStore)
	})

	t.Run("remove delete request", func(t *testing.T) {
		reqID, err := storeTee.AddDeleteRequest(context.Background(), user1, `{foo="bar"}`, now.Add(-2*time.Hour), now, time.Hour)
		require.NoError(t, err)

		err = storeTee.RemoveDeleteRequest(context.Background(), user1, reqID)
		require.NoError(t, err)

		_, err = storeTee.GetDeleteRequest(context.Background(), user1, reqID)
		require.ErrorIs(t, err, ErrDeleteRequestNotFound)

		_, err = storeTee.primaryStore.GetDeleteRequest(context.Background(), user1, reqID)
		require.ErrorIs(t, err, ErrDeleteRequestNotFound)

		_, err = storeTee.backupStore.GetDeleteRequest(context.Background(), user1, reqID)
		require.ErrorIs(t, err, ErrDeleteRequestNotFound)
	})

	t.Run("stopping the store should upload both the db types", func(t *testing.T) {
		storeTee.Stop()
		storageBoltDBFilePath := filepath.Join(objectStorePath, DeleteRequestsTableName+"/"+deleteRequestsDBBoltDBFileName)
		require.FileExists(t, storageBoltDBFilePath)

		storageSQLiteFilePath := filepath.Join(objectStorePath, DeleteRequestsTableName+"/"+deleteRequestsDBSQLiteFileNameGZ)
		require.FileExists(t, storageSQLiteFilePath)

		// remove working dir to delete the local dbs
		require.NoError(t, os.RemoveAll(workingDir))
		require.NoError(t, err)
	})

	t.Run("initializing stores should get dbs from storage", func(t *testing.T) {
		deleteRequestsStore, err := newDeleteRequestsStore(DeleteRequestsStoreDBTypeSQLite, workingDir, indexStorageClient, DeleteRequestsStoreDBTypeBoltDB, time.Hour)
		require.NoError(t, err)
		storeTee = deleteRequestsStore.(deleteRequestsStoreTee)

		reqsFromTee, err := storeTee.GetAllRequests(context.Background())
		require.NoError(t, err)
		require.Len(t, reqsFromTee, 1)
		require.NotEqual(t, StatusReceived, reqsFromTee[0].Status)

		reqsFromPrimaryStore, err := storeTee.primaryStore.GetAllRequests(context.Background())
		require.NoError(t, err)
		require.Equal(t, reqsFromTee, reqsFromPrimaryStore)

		reqsFromBackupStore, err := storeTee.backupStore.GetAllRequests(context.Background())
		require.NoError(t, err)
		compareRequests(t, reqsFromTee, reqsFromBackupStore)
	})

	t.Run("rollback to boltdb should cleanup sqlite db", func(t *testing.T) {
		storeTee.Stop()

		_, err := newDeleteRequestsStore(DeleteRequestsStoreDBTypeBoltDB, workingDir, indexStorageClient, "", time.Hour)
		require.NoError(t, err)

		require.NoFileExists(t, filepath.Join(workingDir, deleteRequestsWorkingDirName, deleteRequestsDBSQLiteFileName))
		_, err = indexStorageClient.GetFile(context.Background(), DeleteRequestsTableName, deleteRequestsDBSQLiteFileNameGZ)
		require.True(t, indexStorageClient.IsFileNotFoundErr(err))
	})
}

// resetSequenceNumInRequests resets sequence number in delete requests.
// Since sequence number is only relevant for boltdb, it makes it easier to compare same requests from sqlite vs boltdb.
func resetSequenceNumInRequests(reqs []DeleteRequest) {
	for i := range reqs {
		reqs[i].SequenceNum = 0
	}
}

type testContext struct {
	user1Requests []DeleteRequest
	user2Requests []DeleteRequest
	store         DeleteRequestsStore
}

func setupStoreType(t *testing.T, storeType DeleteRequestsStoreDBType) *testContext {
	t.Helper()
	tc := &testContext{}
	// build some test requests to add to the store
	for i := time.Duration(1); i <= 24; i++ {
		tc.user1Requests = append(tc.user1Requests, DeleteRequest{
			UserID:    user1,
			StartTime: now.Add(-i * time.Hour),
			EndTime:   now.Add(-i * time.Hour).Add(30 * time.Minute),
			Query:     fmt.Sprintf(`{foo="%d", user="%s"}`, i, user1),
			Status:    StatusReceived,
		})
		tc.user2Requests = append(tc.user2Requests, DeleteRequest{
			UserID:    user2,
			StartTime: now.Add(-i * time.Hour),
			EndTime:   now.Add(-(i + 1) * time.Hour),
			Query:     fmt.Sprintf(`{foo="%d", user="%s"}`, i, user2),
			Status:    StatusReceived,
		})
	}

	// build the store
	tempDir := t.TempDir()

	workingDir := filepath.Join(tempDir, "working-dir")
	objectStorePath := filepath.Join(tempDir, "object-store")

	objectClient, err := local.NewFSObjectClient(local.FSConfig{
		Directory: objectStorePath,
	})
	require.NoError(t, err)

	if storeType == DeleteRequestsStoreDBTypeBoltDB {
		var err error
		tc.store, err = newDeleteRequestsStoreBoltDB(workingDir, storage.NewIndexStorageClient(objectClient, ""))
		require.NoError(t, err)
	} else {
		var err error
		tc.store, err = newDeleteRequestsStoreSQLite(workingDir, storage.NewIndexStorageClient(objectClient, ""), time.Hour)
		require.NoError(t, err)
	}

	return tc
}

var (
	now   = model.Now()
	user1 = "user1"
	user2 = "user2"
)
