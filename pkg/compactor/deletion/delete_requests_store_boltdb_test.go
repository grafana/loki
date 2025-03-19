package deletion

import (
	"context"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
)

func TestDeleteRequestsStoreBoltDB(t *testing.T) {
	tc := setupStoreType(t, DeleteRequestsStoreDBTypeBoltDB)
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
}

func TestBatchCreateGetBoltDB(t *testing.T) {
	t.Run("it adds the requests with different sequence numbers but the same request id, status, and creation time", func(t *testing.T) {
		tc := setupStoreType(t, DeleteRequestsStoreDBTypeBoltDB)
		defer tc.store.Stop()

		reqID, err := tc.store.AddDeleteRequest(context.Background(), user1, `{foo="bar"}`, now.Add(-24*time.Hour), now, time.Hour)
		require.NoError(t, err)

		requests, err := tc.store.(*deleteRequestsStoreBoltDB).getDeleteRequestGroup(context.Background(), user1, reqID)
		require.NoError(t, err)

		// ensure that creation time is set close to now
		require.InDelta(t, int64(model.Now()), int64(requests[0].CreatedAt), float64(5*time.Second))

		for i, req := range requests {
			require.Equal(t, req.RequestID, requests[0].RequestID)
			require.Equal(t, req.Status, requests[0].Status)
			require.Equal(t, req.CreatedAt, requests[0].CreatedAt)
			require.Equal(t, req.Query, requests[0].Query)
			require.Equal(t, req.UserID, requests[0].UserID)

			require.Equal(t, req.SequenceNum, int64(i))
		}
	})

	t.Run("updates a single request with a new status", func(t *testing.T) {
		tc := setupStoreType(t, DeleteRequestsStoreDBTypeBoltDB)
		defer tc.store.Stop()

		reqID, err := tc.store.AddDeleteRequest(context.Background(), user1, `{foo="bar"}`, now.Add(-24*time.Hour), now, time.Hour)
		require.NoError(t, err)

		savedRequests, err := tc.store.(*deleteRequestsStoreBoltDB).getDeleteRequestGroup(context.Background(), user1, reqID)
		require.NoError(t, err)

		err = tc.store.MarkShardAsProcessed(context.Background(), savedRequests[1])
		require.NoError(t, err)

		results, err := tc.store.(*deleteRequestsStoreBoltDB).getDeleteRequestGroup(context.Background(), savedRequests[0].UserID, savedRequests[0].RequestID)
		require.NoError(t, err)

		require.Equal(t, StatusProcessed, results[1].Status)
	})

	t.Run("deletes several delete requests", func(t *testing.T) {
		tc := setupStoreType(t, DeleteRequestsStoreDBTypeBoltDB)
		defer tc.store.Stop()

		reqID, err := tc.store.AddDeleteRequest(context.Background(), user1, `{foo="bar"}`, now.Add(-24*time.Hour), now, time.Hour)
		require.NoError(t, err)

		err = tc.store.RemoveDeleteRequest(context.Background(), user1, reqID)
		require.NoError(t, err)

		results, err := tc.store.GetDeleteRequest(context.Background(), user1, reqID)
		require.ErrorIs(t, err, ErrDeleteRequestNotFound)
		require.Empty(t, results)
	})
}

func TestDeleteRequestsStore_MergeShardedRequests(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		reqsToAdd              []storeAddReqDetails
		shouldMarkProcessed    func(DeleteRequest) bool
		requestsShouldBeMerged bool
	}{
		{
			name: "no requests in store",
		},
		{
			name: "none of the requests are processed - should not merge",
			reqsToAdd: []storeAddReqDetails{
				{
					userID:          user1,
					query:           `{foo="bar"}`,
					startTime:       now.Add(-24 * time.Hour),
					endTime:         now,
					shardByInterval: time.Hour,
				},
			},
			shouldMarkProcessed: func(_ DeleteRequest) bool {
				return false
			},
		},
		{
			name: "not all requests are processed - should not merge",
			reqsToAdd: []storeAddReqDetails{
				{
					userID:          user1,
					query:           `{foo="bar"}`,
					startTime:       now.Add(-24 * time.Hour),
					endTime:         now,
					shardByInterval: time.Hour,
				},
			},
			shouldMarkProcessed: func(request DeleteRequest) bool {
				return request.SequenceNum%2 == 0
			},
		},
		{
			name: "all the requests are processed - should merge",
			reqsToAdd: []storeAddReqDetails{
				{
					userID:          user1,
					query:           `{foo="bar"}`,
					startTime:       now.Add(-24 * time.Hour),
					endTime:         now,
					shardByInterval: time.Hour,
				},
			},
			shouldMarkProcessed: func(_ DeleteRequest) bool {
				return true
			},
			requestsShouldBeMerged: true,
		},
		{ // build requests for 2 different users and mark all requests as processed for just one of the two
			name: "merging requests from one user should not touch another users requests",
			reqsToAdd: []storeAddReqDetails{
				{
					userID:          user1,
					query:           `{foo="bar"}`,
					startTime:       now.Add(-24 * time.Hour),
					endTime:         now,
					shardByInterval: time.Hour,
				},
				{
					userID:          user2,
					query:           `{foo="bar"}`,
					startTime:       now.Add(-24 * time.Hour),
					endTime:         now,
					shardByInterval: time.Hour,
				},
			},
			shouldMarkProcessed: func(request DeleteRequest) bool {
				return request.UserID == user2
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()

			workingDir := filepath.Join(tempDir, "working-dir")
			objectStorePath := filepath.Join(tempDir, "object-store")

			objectClient, err := local.NewFSObjectClient(local.FSConfig{
				Directory: objectStorePath,
			})
			require.NoError(t, err)
			ds, err := newDeleteRequestsStoreBoltDB(workingDir, storage.NewIndexStorageClient(objectClient, ""))
			require.NoError(t, err)

			for _, addReqDetails := range tc.reqsToAdd {
				_, err := ds.AddDeleteRequest(context.Background(), addReqDetails.userID, addReqDetails.query, addReqDetails.startTime, addReqDetails.endTime, addReqDetails.shardByInterval)
				require.NoError(t, err)
			}

			reqs, err := ds.getAllShards(context.Background())
			require.NoError(t, err)

			for _, req := range reqs {
				if !tc.shouldMarkProcessed(req) {
					continue
				}
				require.NoError(t, ds.MarkShardAsProcessed(context.Background(), req))
			}

			inStoreReqs, err := ds.GetAllDeleteRequestsForUser(context.Background(), user1, false)
			require.NoError(t, err)

			require.NoError(t, ds.MergeShardedRequests(context.Background()))
			inStoreReqsAfterMerging, err := ds.GetAllDeleteRequestsForUser(context.Background(), user1, false)
			require.NoError(t, err)

			if tc.requestsShouldBeMerged {
				require.Len(t, inStoreReqsAfterMerging, 1)
				require.True(t, requestsAreEqual(inStoreReqsAfterMerging[0], DeleteRequest{
					RequestID: inStoreReqs[0].RequestID,
					UserID:    user1,
					Query:     tc.reqsToAdd[0].query,
					StartTime: tc.reqsToAdd[0].startTime,
					EndTime:   tc.reqsToAdd[len(tc.reqsToAdd)-1].endTime,
					Status:    StatusProcessed,
				}))
			} else {
				require.Len(t, inStoreReqsAfterMerging, len(inStoreReqs))
				require.Equal(t, inStoreReqs, inStoreReqsAfterMerging)
			}
		})
	}
}

func compareRequests(t *testing.T, expected []DeleteRequest, actual []DeleteRequest) {
	require.Len(t, actual, len(expected))
	sort.Slice(expected, func(i, j int) bool {
		return expected[i].RequestID < expected[j].RequestID
	})
	sort.Slice(actual, func(i, j int) bool {
		return actual[i].RequestID < actual[j].RequestID
	})
	for i, deleteRequest := range actual {
		require.True(t, requestsAreEqual(expected[i], deleteRequest))
	}
}
