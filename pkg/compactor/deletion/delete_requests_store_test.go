package deletion

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
)

func TestDeleteRequestsStore(t *testing.T) {
	tc := setup(t)
	defer tc.store.Stop()

	// add requests for both the users to the store
	for i := 0; i < len(tc.user1Requests); i++ {
		resp, err := tc.store.AddDeleteRequestGroup(
			context.Background(),
			[]DeleteRequest{tc.user1Requests[i]},
		)
		require.NoError(t, err)
		tc.user1Requests[i] = resp[0]

		resp, err = tc.store.AddDeleteRequestGroup(
			context.Background(),
			[]DeleteRequest{tc.user2Requests[i]},
		)
		require.NoError(t, err)
		tc.user2Requests[i] = resp[0]
	}

	// get all requests with StatusReceived and see if they have expected values
	deleteRequests, err := tc.store.GetDeleteRequestsByStatus(context.Background(), StatusReceived)
	require.NoError(t, err)
	compareRequests(t, append(tc.user1Requests, tc.user2Requests...), deleteRequests)

	// get user specific requests and see if they have expected values
	user1Requests, err := tc.store.GetAllDeleteRequestsForUser(context.Background(), user1)
	require.NoError(t, err)
	compareRequests(t, tc.user1Requests, user1Requests)

	user2Requests, err := tc.store.GetAllDeleteRequestsForUser(context.Background(), user2)
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
		actualRequest, err := tc.store.GetDeleteRequestGroup(context.Background(), expectedRequest.UserID, expectedRequest.RequestID)
		require.NoError(t, err)
		require.Len(t, actualRequest, 1)
		require.Equal(t, expectedRequest, actualRequest[0])
	}

	// try a non-existent request and see if it throws ErrDeleteRequestNotFound
	_, err = tc.store.GetDeleteRequestGroup(context.Background(), "user3", "na")
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

		require.NoError(t, tc.store.UpdateStatus(context.Background(), request, StatusProcessed))
	}

	// see if requests in the store have right values
	user1Requests, err = tc.store.GetAllDeleteRequestsForUser(context.Background(), user1)
	require.NoError(t, err)
	compareRequests(t, tc.user1Requests, user1Requests)

	user2Requests, err = tc.store.GetAllDeleteRequestsForUser(context.Background(), user2)
	require.NoError(t, err)
	compareRequests(t, tc.user2Requests, user2Requests)

	updateGenNumber, err := tc.store.GetCacheGenerationNumber(context.Background(), user1)
	require.NoError(t, err)
	require.NotEqual(t, createGenNumber, updateGenNumber)

	updateGenNumber2, err := tc.store.GetCacheGenerationNumber(context.Background(), user2)
	require.NoError(t, err)
	require.NotEqual(t, createGenNumber2, updateGenNumber2)

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

		require.NoError(t, tc.store.RemoveDeleteRequests(context.Background(), []DeleteRequest{request}))
	}

	// see if the store has the right remaining requests
	deleteRequests, err = tc.store.GetDeleteRequestsByStatus(context.Background(), StatusReceived)
	require.NoError(t, err)
	compareRequests(t, remainingRequests, deleteRequests)

	deleteGenNumber, err := tc.store.GetCacheGenerationNumber(context.Background(), user1)
	require.NoError(t, err)
	require.NotEqual(t, updateGenNumber, deleteGenNumber)

	deleteGenNumber2, err := tc.store.GetCacheGenerationNumber(context.Background(), user2)
	require.NoError(t, err)
	require.NotEqual(t, updateGenNumber2, deleteGenNumber2)
}

func TestBatchCreateGet(t *testing.T) {
	t.Run("it adds the requests with different sequence numbers but the same request id, status, and creation time", func(t *testing.T) {
		tc := setup(t)
		defer tc.store.Stop()

		requests, err := tc.store.AddDeleteRequestGroup(context.Background(), tc.user1Requests)
		require.NoError(t, err)

		for i, req := range requests {
			require.Equal(t, req.RequestID, requests[0].RequestID)
			require.Equal(t, req.Status, requests[0].Status)
			require.Equal(t, req.CreatedAt, requests[0].CreatedAt)

			require.Equal(t, req.SequenceNum, int64(i))
		}
	})

	t.Run("returns all the requests that share a request id", func(t *testing.T) {
		tc := setup(t)
		defer tc.store.Stop()

		savedRequests, err := tc.store.AddDeleteRequestGroup(context.Background(), tc.user1Requests)
		require.NoError(t, err)

		results, err := tc.store.GetDeleteRequestGroup(context.Background(), savedRequests[0].UserID, savedRequests[0].RequestID)
		require.NoError(t, err)

		require.Equal(t, savedRequests, results)
	})

	t.Run("updates a single request with a new status", func(t *testing.T) {
		tc := setup(t)
		defer tc.store.Stop()

		savedRequests, err := tc.store.AddDeleteRequestGroup(context.Background(), tc.user1Requests)
		require.NoError(t, err)

		err = tc.store.UpdateStatus(context.Background(), savedRequests[1], StatusProcessed)
		require.NoError(t, err)

		results, err := tc.store.GetDeleteRequestGroup(context.Background(), savedRequests[0].UserID, savedRequests[0].RequestID)
		require.NoError(t, err)

		require.Equal(t, StatusProcessed, results[1].Status)
	})

	t.Run("deletes several delete requests", func(t *testing.T) {
		tc := setup(t)
		defer tc.store.Stop()

		savedRequests, err := tc.store.AddDeleteRequestGroup(context.Background(), tc.user1Requests)
		require.NoError(t, err)

		err = tc.store.RemoveDeleteRequests(context.Background(), savedRequests)
		require.NoError(t, err)

		results, err := tc.store.GetDeleteRequestGroup(context.Background(), savedRequests[0].UserID, savedRequests[0].RequestID)
		require.ErrorIs(t, err, ErrDeleteRequestNotFound)
		require.Empty(t, results)
	})
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
		require.Equal(t, expected[i], deleteRequest)
	}
}

type testContext struct {
	user1Requests []DeleteRequest
	user2Requests []DeleteRequest
	store         *deleteRequestsStore
}

func setup(t *testing.T) *testContext {
	t.Helper()
	tc := &testContext{}
	// build some test requests to add to the store
	for i := time.Duration(1); i <= 24; i++ {
		tc.user1Requests = append(tc.user1Requests, DeleteRequest{
			UserID:    user1,
			StartTime: now.Add(-i * time.Hour),
			EndTime:   now.Add(-i * time.Hour).Add(30 * time.Minute),
			CreatedAt: model.Time(38),
			Query:     fmt.Sprintf(`{foo="%d", user="%s"}`, i, user1),
			Status:    StatusReceived,
		})
		tc.user2Requests = append(tc.user2Requests, DeleteRequest{
			UserID:    user2,
			StartTime: now.Add(-i * time.Hour),
			EndTime:   now.Add(-(i + 1) * time.Hour),
			CreatedAt: model.Time(38),
			Query:     fmt.Sprintf(`{foo="%d", user="%s"}`, i, user2),
			Status:    StatusReceived,
		})
	}

	// build the store
	tempDir := t.TempDir()
	//tempDir := os.TempDir()
	fmt.Println(tempDir)

	workingDir := filepath.Join(tempDir, "working-dir")
	objectStorePath := filepath.Join(tempDir, "object-store")

	objectClient, err := local.NewFSObjectClient(local.FSConfig{
		Directory: objectStorePath,
	})
	require.NoError(t, err)
	ds, err := NewDeleteStore(workingDir, storage.NewIndexStorageClient(objectClient, ""))
	require.NoError(t, err)

	store := ds.(*deleteRequestsStore)
	store.now = func() model.Time { return model.Time(38) }
	tc.store = store

	return tc
}

var (
	now   = model.Now()
	user1 = "user1"
	user2 = "user2"
)
