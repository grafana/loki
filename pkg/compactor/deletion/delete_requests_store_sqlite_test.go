package deletion

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"zombiezen.com/go/sqlite/sqlitex"

	"github.com/grafana/loki/v3/pkg/compactor/deletion/deletionproto"
)

func TestDeleteRequestsStoreSQLite(t *testing.T) {
	tc := setupStoreType(t, DeleteRequestsStoreDBTypeSQLite)
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

	var user1UnprocessedRequests []deletionproto.DeleteRequest
	var user2UnprocessedRequests []deletionproto.DeleteRequest

	// update some of the delete requests for both the users to processed
	for i := 0; i < len(tc.user1Requests); i++ {
		var request deletionproto.DeleteRequest
		if i%2 != 0 {
			user2UnprocessedRequests = append(user2UnprocessedRequests, tc.user2Requests[i])
			tc.user1Requests[i].Status = deletionproto.StatusProcessed
			request = tc.user1Requests[i]
		} else {
			user1UnprocessedRequests = append(user1UnprocessedRequests, tc.user1Requests[i])
			tc.user2Requests[i].Status = deletionproto.StatusProcessed
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

	// see if listing user requests for query time filtering eliminates processed requests
	tc.store.(*deleteRequestsStoreSQLite).indexUpdatePropagationMaxDelay = 0 // set the index propagation max delay to 0
	time.Sleep(time.Microsecond)                                             // sleep for a microsecond to avoid flaky tests

	user1Requests, err = tc.store.GetAllDeleteRequestsForUser(context.Background(), user1, true)
	require.NoError(t, err)
	compareRequests(t, user1UnprocessedRequests, user1Requests)

	user2Requests, err = tc.store.GetAllDeleteRequestsForUser(context.Background(), user2, true)
	require.NoError(t, err)
	compareRequests(t, user2UnprocessedRequests, user2Requests)

	// caches should not be invalidated when we mark delete request as processed
	updateGenNumber, err := tc.store.GetCacheGenerationNumber(context.Background(), user1)
	require.NoError(t, err)
	require.Equal(t, createGenNumber, updateGenNumber)

	updateGenNumber2, err := tc.store.GetCacheGenerationNumber(context.Background(), user2)
	require.NoError(t, err)
	require.Equal(t, createGenNumber2, updateGenNumber2)

	// delete the requests from the store updated previously
	var remainingRequests []deletionproto.DeleteRequest
	for i := 0; i < len(tc.user1Requests); i++ {
		var request deletionproto.DeleteRequest
		if i%2 != 0 {
			tc.user1Requests[i].Status = deletionproto.StatusProcessed
			request = tc.user1Requests[i]
			remainingRequests = append(remainingRequests, tc.user2Requests[i])
		} else {
			tc.user2Requests[i].Status = deletionproto.StatusProcessed
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

func TestBatchCreateGetSQLite(t *testing.T) {
	t.Run("it adds the requests with different sequence numbers but the same request id, status, and creation time", func(t *testing.T) {
		tc := setupStoreType(t, DeleteRequestsStoreDBTypeSQLite)
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

	t.Run("mark a single shard as processed", func(t *testing.T) {
		tc := setupStoreType(t, DeleteRequestsStoreDBTypeSQLite)
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
		tc := setupStoreType(t, DeleteRequestsStoreDBTypeSQLite)
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

func TestFixProcessedShardCount(t *testing.T) {
	tc := setupStoreType(t, DeleteRequestsStoreDBTypeSQLite)
	defer tc.store.Stop()

	// add a delete request
	reqID, err := tc.store.AddDeleteRequest(context.Background(), user1, `{foo="bar"}`, now.Add(-24*time.Hour), now, time.Hour)
	require.NoError(t, err)

	// get the current state from db
	before, err := tc.store.GetDeleteRequest(context.Background(), user1, reqID)
	require.NoError(t, err)

	sqliteStore := tc.store.(*deleteRequestsStoreSQLite)

	// trying to fix the shard count should have no effect
	require.NoError(t, sqliteStore.fixProcessedShardCount(context.Background()))

	after, err := tc.store.GetDeleteRequest(context.Background(), user1, reqID)
	require.NoError(t, err)
	require.Equal(t, before, after)

	shards, err := tc.store.GetUnprocessedShards(context.Background())
	require.NoError(t, err)

	// mark a shard as processed
	require.NoError(t, tc.store.MarkShardAsProcessed(context.Background(), shards[0]))

	// refresh the current state from db
	before, err = tc.store.GetDeleteRequest(context.Background(), user1, reqID)
	require.NoError(t, err)

	// trying to fix the shard count should have no effect
	require.NoError(t, sqliteStore.fixProcessedShardCount(context.Background()))
	after, err = tc.store.GetDeleteRequest(context.Background(), user1, reqID)
	require.NoError(t, err)
	require.Equal(t, before, after)

	// set the shard count to an incorrect value
	err = sqliteStore.sqliteStore.Exec(context.Background(), true, sqlQuery{
		query: sqlUpdateShardCount,
		execOpts: &sqlitex.ExecOptions{
			Args: []any{
				len(shards) + 1,
				len(shards) + 1,
				time.Now().UnixNano(),
				reqID,
			},
		},
		postUpdateExecCallback: func(numChanges int) error {
			require.Equal(t, 1, numChanges)
			return nil
		},
	})
	require.NoError(t, err)

	after, err = tc.store.GetDeleteRequest(context.Background(), user1, reqID)
	require.NoError(t, err)
	require.NotEqual(t, before, after)

	// try fixing the shard count and see if we get the correct state back
	require.NoError(t, sqliteStore.fixProcessedShardCount(context.Background()))
	after, err = tc.store.GetDeleteRequest(context.Background(), user1, reqID)
	require.NoError(t, err)
	require.Equal(t, before, after)

	// delete all shards of the request without touching the consolidated record for the request
	err = sqliteStore.sqliteStore.Exec(context.Background(), true, sqlQuery{
		query: sqlDeleteShards,
		execOpts: &sqlitex.ExecOptions{
			Args: []any{
				reqID,
				user1,
			},
		},
	})
	require.NoError(t, err)

	// ensure that the current status of the request is not processed
	require.NotEqual(t, deletionproto.StatusProcessed, after.Status)

	// fixing the shard count should mark the request as processed
	require.NoError(t, sqliteStore.fixProcessedShardCount(context.Background()))

	// verify that the request has been marked as processed in the db
	after, err = tc.store.GetDeleteRequest(context.Background(), user1, reqID)
	require.NoError(t, err)
	require.Equal(t, deletionproto.StatusProcessed, after.Status)
}
