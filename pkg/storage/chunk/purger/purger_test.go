package purger

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util/test"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/testutils"
	util_log "github.com/grafana/loki/pkg/util/log"
)

const (
	userID        = "userID"
	modelTimeDay  = model.Time(millisecondPerDay)
	modelTimeHour = model.Time(time.Hour / time.Millisecond)
)

func setupTestDeleteStore(t *testing.T) *DeleteStore {
	var (
		deleteStoreConfig DeleteStoreConfig
		tbmConfig         chunk.TableManagerConfig
		schemaCfg         = chunk.DefaultSchemaConfig("", "v10", 0)
	)
	flagext.DefaultValues(&deleteStoreConfig)
	flagext.DefaultValues(&tbmConfig)

	mockStorage := chunk.NewMockStorage()

	extraTables := []chunk.ExtraTables{{TableClient: mockStorage, Tables: deleteStoreConfig.GetTables()}}
	tableManager, err := chunk.NewTableManager(tbmConfig, schemaCfg, 12*time.Hour, mockStorage, nil, extraTables, nil)
	require.NoError(t, err)

	require.NoError(t, tableManager.SyncTables(context.Background()))

	deleteStore, err := NewDeleteStore(deleteStoreConfig, mockStorage)
	require.NoError(t, err)

	return deleteStore
}

func setupStoresAndPurger(t *testing.T) (*DeleteStore, chunk.Store, chunk.ObjectClient, *Purger, *prometheus.Registry) {
	deleteStore := setupTestDeleteStore(t)

	chunkStore, err := testutils.SetupTestChunkStore()
	require.NoError(t, err)

	storageClient, err := testutils.SetupTestObjectStore()
	require.NoError(t, err)

	purger, registry := setupPurger(t, deleteStore, chunkStore, storageClient)

	return deleteStore, chunkStore, storageClient, purger, registry
}

func setupPurger(t *testing.T, deleteStore *DeleteStore, chunkStore chunk.Store, storageClient chunk.ObjectClient) (*Purger, *prometheus.Registry) {
	registry := prometheus.NewRegistry()

	var cfg Config
	flagext.DefaultValues(&cfg)

	purger, err := NewPurger(cfg, deleteStore, chunkStore, storageClient, registry)
	require.NoError(t, err)

	return purger, registry
}

func buildChunks(from, through model.Time, batchSize int) ([]chunk.Chunk, error) {
	var chunks []chunk.Chunk
	for ; from < through; from = from.Add(time.Hour) {
		// creating batchSize number of chunks chunks per hour
		_, testChunks, err := testutils.CreateChunks(0, batchSize, from, from.Add(time.Hour))
		if err != nil {
			return nil, err
		}

		chunks = append(chunks, testChunks...)
	}

	return chunks, nil
}

var purgePlanTestCases = []struct {
	name                              string
	chunkStoreDataInterval            model.Interval
	deleteRequestInterval             model.Interval
	expectedNumberOfPlans             int
	numChunksToDelete                 int
	firstChunkPartialDeletionInterval *Interval
	lastChunkPartialDeletionInterval  *Interval
	batchSize                         int
}{
	{
		name:                   "deleting whole hour from a one hour data",
		chunkStoreDataInterval: model.Interval{End: modelTimeHour},
		deleteRequestInterval:  model.Interval{End: modelTimeHour},
		expectedNumberOfPlans:  1,
		numChunksToDelete:      1,
	},
	{
		name:                   "deleting half a day from a days data",
		chunkStoreDataInterval: model.Interval{End: modelTimeDay},
		deleteRequestInterval:  model.Interval{End: model.Time(millisecondPerDay / 2)},
		expectedNumberOfPlans:  1,
		numChunksToDelete:      12 + 1, // one chunk for each hour + end time touches chunk at boundary
		lastChunkPartialDeletionInterval: &Interval{
			StartTimestampMs: millisecondPerDay / 2,
			EndTimestampMs:   millisecondPerDay / 2,
		},
	},
	{
		name:                   "deleting a full day from 2 days data",
		chunkStoreDataInterval: model.Interval{End: modelTimeDay * 2},
		deleteRequestInterval:  model.Interval{End: modelTimeDay},
		expectedNumberOfPlans:  1,
		numChunksToDelete:      24 + 1, // one chunk for each hour + end time touches chunk at boundary
		lastChunkPartialDeletionInterval: &Interval{
			StartTimestampMs: millisecondPerDay,
			EndTimestampMs:   millisecondPerDay,
		},
	},
	{
		name:                   "deleting 2 days partially from 2 days data",
		chunkStoreDataInterval: model.Interval{End: modelTimeDay * 2},
		deleteRequestInterval: model.Interval{
			Start: model.Time(millisecondPerDay / 2),
			End:   model.Time(millisecondPerDay + millisecondPerDay/2),
		},
		expectedNumberOfPlans: 2,
		numChunksToDelete:     24 + 2, // one chunk for each hour + start and end time touches chunk at boundary
		firstChunkPartialDeletionInterval: &Interval{
			StartTimestampMs: millisecondPerDay / 2,
			EndTimestampMs:   millisecondPerDay / 2,
		},
		lastChunkPartialDeletionInterval: &Interval{
			StartTimestampMs: millisecondPerDay + millisecondPerDay/2,
			EndTimestampMs:   millisecondPerDay + millisecondPerDay/2,
		},
	},
	{
		name:                   "deleting 2 days partially, not aligned with hour, from 2 days data",
		chunkStoreDataInterval: model.Interval{End: modelTimeDay * 2},
		deleteRequestInterval: model.Interval{
			Start: model.Time(millisecondPerDay / 2).Add(time.Minute),
			End:   model.Time(millisecondPerDay + millisecondPerDay/2).Add(-time.Minute),
		},
		expectedNumberOfPlans: 2,
		numChunksToDelete:     24, // one chunk for each hour, no chunks touched at boundary
		firstChunkPartialDeletionInterval: &Interval{
			StartTimestampMs: int64(model.Time(millisecondPerDay / 2).Add(time.Minute)),
			EndTimestampMs:   int64(model.Time(millisecondPerDay / 2).Add(time.Hour)),
		},
		lastChunkPartialDeletionInterval: &Interval{
			StartTimestampMs: int64(model.Time(millisecondPerDay + millisecondPerDay/2).Add(-time.Hour)),
			EndTimestampMs:   int64(model.Time(millisecondPerDay + millisecondPerDay/2).Add(-time.Minute)),
		},
	},
	{
		name:                   "deleting data outside of period of existing data",
		chunkStoreDataInterval: model.Interval{End: modelTimeDay},
		deleteRequestInterval:  model.Interval{Start: model.Time(millisecondPerDay * 2), End: model.Time(millisecondPerDay * 3)},
		expectedNumberOfPlans:  1,
		numChunksToDelete:      0,
	},
	{
		name:                   "building multi-day chunk and deleting part of it from first day",
		chunkStoreDataInterval: model.Interval{Start: modelTimeDay.Add(-30 * time.Minute), End: modelTimeDay.Add(30 * time.Minute)},
		deleteRequestInterval:  model.Interval{Start: modelTimeDay.Add(-30 * time.Minute), End: modelTimeDay.Add(-15 * time.Minute)},
		expectedNumberOfPlans:  1,
		numChunksToDelete:      1,
		firstChunkPartialDeletionInterval: &Interval{
			StartTimestampMs: int64(modelTimeDay.Add(-30 * time.Minute)),
			EndTimestampMs:   int64(modelTimeDay.Add(-15 * time.Minute)),
		},
	},
	{
		name:                   "building multi-day chunk and deleting part of it for each day",
		chunkStoreDataInterval: model.Interval{Start: modelTimeDay.Add(-30 * time.Minute), End: modelTimeDay.Add(30 * time.Minute)},
		deleteRequestInterval:  model.Interval{Start: modelTimeDay.Add(-15 * time.Minute), End: modelTimeDay.Add(15 * time.Minute)},
		expectedNumberOfPlans:  2,
		numChunksToDelete:      1,
		firstChunkPartialDeletionInterval: &Interval{
			StartTimestampMs: int64(modelTimeDay.Add(-15 * time.Minute)),
			EndTimestampMs:   int64(modelTimeDay.Add(15 * time.Minute)),
		},
	},
}

func TestPurger_BuildPlan(t *testing.T) {
	for _, tc := range purgePlanTestCases {
		for batchSize := 1; batchSize <= 5; batchSize++ {
			t.Run(fmt.Sprintf("%s/batch-size=%d", tc.name, batchSize), func(t *testing.T) {
				deleteStore, chunkStore, storageClient, purger, _ := setupStoresAndPurger(t)
				defer func() {
					purger.StopAsync()
					chunkStore.Stop()
				}()

				chunks, err := buildChunks(tc.chunkStoreDataInterval.Start, tc.chunkStoreDataInterval.End, batchSize)
				require.NoError(t, err)

				require.NoError(t, chunkStore.Put(context.Background(), chunks))

				err = deleteStore.AddDeleteRequest(context.Background(), userID, tc.deleteRequestInterval.Start,
					tc.deleteRequestInterval.End, []string{"foo"})
				require.NoError(t, err)

				deleteRequests, err := deleteStore.GetAllDeleteRequestsForUser(context.Background(), userID)
				require.NoError(t, err)

				deleteRequest := deleteRequests[0]
				requestWithLogger := makeDeleteRequestWithLogger(deleteRequest, util_log.Logger)

				err = purger.buildDeletePlan(requestWithLogger)
				require.NoError(t, err)
				planPath := fmt.Sprintf("%s:%s/", userID, deleteRequest.RequestID)

				plans, _, err := storageClient.List(context.Background(), planPath, "/")
				require.NoError(t, err)
				require.Equal(t, tc.expectedNumberOfPlans, len(plans))

				numPlans := tc.expectedNumberOfPlans
				var nilPurgePlanInterval *Interval
				numChunks := 0

				chunkIDs := map[string]struct{}{}

				for i := range plans {
					deletePlan, err := purger.getDeletePlan(context.Background(), userID, deleteRequest.RequestID, i)
					require.NoError(t, err)
					for _, chunksGroup := range deletePlan.ChunksGroup {
						numChunksInGroup := len(chunksGroup.Chunks)
						chunks := chunksGroup.Chunks
						numChunks += numChunksInGroup

						sort.Slice(chunks, func(i, j int) bool {
							chunkI, err := chunk.ParseExternalKey(userID, chunks[i].ID)
							require.NoError(t, err)

							chunkJ, err := chunk.ParseExternalKey(userID, chunks[j].ID)
							require.NoError(t, err)

							return chunkI.From < chunkJ.From
						})

						for j, chunkDetails := range chunksGroup.Chunks {
							chunkIDs[chunkDetails.ID] = struct{}{}
							if i == 0 && j == 0 && tc.firstChunkPartialDeletionInterval != nil {
								require.Equal(t, *tc.firstChunkPartialDeletionInterval, *chunkDetails.PartiallyDeletedInterval)
							} else if i == numPlans-1 && j == numChunksInGroup-1 && tc.lastChunkPartialDeletionInterval != nil {
								require.Equal(t, *tc.lastChunkPartialDeletionInterval, *chunkDetails.PartiallyDeletedInterval)
							} else {
								require.Equal(t, nilPurgePlanInterval, chunkDetails.PartiallyDeletedInterval)
							}
						}
					}
				}

				require.Equal(t, tc.numChunksToDelete*batchSize, len(chunkIDs))
				require.Equal(t, float64(tc.numChunksToDelete*batchSize), testutil.ToFloat64(purger.metrics.deleteRequestsChunksSelectedTotal))
			})
		}
	}
}

func TestPurger_ExecutePlan(t *testing.T) {
	fooMetricNameMatcher, err := parser.ParseMetricSelector(`foo`)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range purgePlanTestCases {
		for batchSize := 1; batchSize <= 5; batchSize++ {
			t.Run(fmt.Sprintf("%s/batch-size=%d", tc.name, batchSize), func(t *testing.T) {
				deleteStore, chunkStore, _, purger, _ := setupStoresAndPurger(t)
				defer func() {
					purger.StopAsync()
					chunkStore.Stop()
				}()

				chunks, err := buildChunks(tc.chunkStoreDataInterval.Start, tc.chunkStoreDataInterval.End, batchSize)
				require.NoError(t, err)

				require.NoError(t, chunkStore.Put(context.Background(), chunks))

				// calculate the expected number of chunks that should be there in store before deletion
				chunkStoreDataIntervalTotal := tc.chunkStoreDataInterval.End - tc.chunkStoreDataInterval.Start
				numChunksExpected := int(chunkStoreDataIntervalTotal / model.Time(time.Hour/time.Millisecond))

				// see if store actually has expected number of chunks
				chunks, err = chunkStore.Get(context.Background(), userID, tc.chunkStoreDataInterval.Start, tc.chunkStoreDataInterval.End, fooMetricNameMatcher...)
				require.NoError(t, err)
				require.Equal(t, numChunksExpected*batchSize, len(chunks))

				// delete chunks
				err = deleteStore.AddDeleteRequest(context.Background(), userID, tc.deleteRequestInterval.Start,
					tc.deleteRequestInterval.End, []string{"foo"})
				require.NoError(t, err)

				// get the delete request
				deleteRequests, err := deleteStore.GetAllDeleteRequestsForUser(context.Background(), userID)
				require.NoError(t, err)

				deleteRequest := deleteRequests[0]
				requestWithLogger := makeDeleteRequestWithLogger(deleteRequest, util_log.Logger)
				err = purger.buildDeletePlan(requestWithLogger)
				require.NoError(t, err)

				// execute all the plans
				for i := 0; i < tc.expectedNumberOfPlans; i++ {
					err := purger.executePlan(userID, deleteRequest.RequestID, i, requestWithLogger.logger)
					require.NoError(t, err)
				}

				// calculate the expected number of chunks that should be there in store after deletion
				numChunksExpectedAfterDeletion := 0
				for chunkStart := tc.chunkStoreDataInterval.Start; chunkStart < tc.chunkStoreDataInterval.End; chunkStart += modelTimeHour {
					numChunksExpectedAfterDeletion += len(getNonDeletedIntervals(model.Interval{Start: chunkStart, End: chunkStart + modelTimeHour}, tc.deleteRequestInterval))
				}

				// see if store actually has expected number of chunks
				chunks, err = chunkStore.Get(context.Background(), userID, tc.chunkStoreDataInterval.Start, tc.chunkStoreDataInterval.End, fooMetricNameMatcher...)
				require.NoError(t, err)
				require.Equal(t, numChunksExpectedAfterDeletion*batchSize, len(chunks))
			})
		}
	}
}

func TestPurger_Restarts(t *testing.T) {
	fooMetricNameMatcher, err := parser.ParseMetricSelector(`foo`)
	if err != nil {
		t.Fatal(err)
	}

	deleteStore, chunkStore, storageClient, purger, _ := setupStoresAndPurger(t)
	defer func() {
		chunkStore.Stop()
	}()

	chunks, err := buildChunks(0, model.Time(0).Add(10*24*time.Hour), 1)
	require.NoError(t, err)

	require.NoError(t, chunkStore.Put(context.Background(), chunks))

	// delete chunks
	err = deleteStore.AddDeleteRequest(context.Background(), userID, model.Time(0).Add(24*time.Hour),
		model.Time(0).Add(8*24*time.Hour), []string{"foo"})
	require.NoError(t, err)

	// get the delete request
	deleteRequests, err := deleteStore.GetAllDeleteRequestsForUser(context.Background(), userID)
	require.NoError(t, err)

	deleteRequest := deleteRequests[0]
	requestWithLogger := makeDeleteRequestWithLogger(deleteRequest, util_log.Logger)
	err = purger.buildDeletePlan(requestWithLogger)
	require.NoError(t, err)

	// stop the existing purger
	require.NoError(t, services.StopAndAwaitTerminated(context.Background(), purger))

	// create a new purger to check whether it picks up in process delete requests
	newPurger, _ := setupPurger(t, deleteStore, chunkStore, storageClient)

	// load in process delete requests by calling Run
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), newPurger))

	defer newPurger.StopAsync()

	test.Poll(t, time.Minute, 0, func() interface{} {
		return newPurger.inProcessRequests.len()
	})

	// check whether data got deleted from the store since delete request has been processed
	chunks, err = chunkStore.Get(context.Background(), userID, 0, model.Time(0).Add(10*24*time.Hour), fooMetricNameMatcher...)
	require.NoError(t, err)

	// we are deleting 7 days out of 10 so there should we 3 days data left in store which means 72 chunks
	require.Equal(t, 72, len(chunks))

	deleteRequests, err = deleteStore.GetAllDeleteRequestsForUser(context.Background(), userID)
	require.NoError(t, err)
	require.Equal(t, StatusProcessed, deleteRequests[0].Status)

	require.Equal(t, float64(1), testutil.ToFloat64(newPurger.metrics.deleteRequestsProcessedTotal))
	require.PanicsWithError(t, "collected 0 metrics instead of exactly 1", func() {
		testutil.ToFloat64(newPurger.metrics.deleteRequestsProcessingFailures)
	})
}

// nolint
func TestPurger_Metrics(t *testing.T) {
	deleteStore, chunkStore, storageClient, purger, registry := setupStoresAndPurger(t)
	defer func() {
		purger.StopAsync()
		chunkStore.Stop()
	}()

	// add delete requests without starting purger loops to load and process delete requests.
	// add delete request whose createdAt is now
	err := deleteStore.AddDeleteRequest(context.Background(), userID, model.Time(0).Add(24*time.Hour),
		model.Time(0).Add(2*24*time.Hour), []string{"foo"})
	require.NoError(t, err)

	// add delete request whose createdAt is 2 days back
	err = deleteStore.addDeleteRequest(context.Background(), userID, model.Now().Add(-2*24*time.Hour), model.Time(0).Add(24*time.Hour),
		model.Time(0).Add(2*24*time.Hour), []string{"foo"})
	require.NoError(t, err)

	// add delete request whose createdAt is 3 days back
	err = deleteStore.addDeleteRequest(context.Background(), userID, model.Now().Add(-3*24*time.Hour), model.Time(0).Add(24*time.Hour),
		model.Time(0).Add(8*24*time.Hour), []string{"foo"})
	require.NoError(t, err)

	// load new delete requests for processing
	require.NoError(t, purger.pullDeleteRequestsToPlanDeletes())

	// there must be 2 pending delete requests, oldest being 2 days old since its cancellation time is over
	require.InDelta(t, float64(2*86400), testutil.ToFloat64(purger.metrics.oldestPendingDeleteRequestAgeSeconds), 1)
	require.Equal(t, float64(2), testutil.ToFloat64(purger.metrics.pendingDeleteRequestsCount))

	// stop the existing purger
	require.NoError(t, services.StopAndAwaitTerminated(context.Background(), purger))

	// create a new purger
	purger, registry = setupPurger(t, deleteStore, chunkStore, storageClient)

	// load in process delete requests by starting the service
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), purger))

	defer purger.StopAsync()

	// wait until purger_delete_requests_processed_total starts to show up.
	test.Poll(t, 2*time.Second, 1, func() interface{} {
		count, err := testutil.GatherAndCount(registry, "loki_purger_delete_requests_processed_total")
		require.NoError(t, err)
		return count
	})

	// wait until both the pending delete requests are processed.
	test.Poll(t, 2*time.Second, float64(2), func() interface{} {
		return testutil.ToFloat64(purger.metrics.deleteRequestsProcessedTotal)
	})

	// wait until oldest pending request age becomes 0
	test.Poll(t, 2*time.Second, float64(0), func() interface{} {
		return testutil.ToFloat64(purger.metrics.oldestPendingDeleteRequestAgeSeconds)
	})

	// wait until pending delete requests count becomes 0
	test.Poll(t, 2*time.Second, float64(0), func() interface{} {
		return testutil.ToFloat64(purger.metrics.pendingDeleteRequestsCount)
	})
}

func TestPurger_retryFailedRequests(t *testing.T) {
	// setup chunks store
	indexMockStorage := chunk.NewMockStorage()
	chunksMockStorage := chunk.NewMockStorage()

	deleteStore := setupTestDeleteStore(t)
	chunkStore, err := testutils.SetupTestChunkStoreWithClients(indexMockStorage, chunksMockStorage, indexMockStorage)
	require.NoError(t, err)

	// create a purger instance
	purgerMockStorage := chunk.NewMockStorage()
	purger, _ := setupPurger(t, deleteStore, chunkStore, purgerMockStorage)
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), purger))

	defer func() {
		require.NoError(t, services.StopAndAwaitTerminated(context.Background(), purger))
	}()

	// add some chunks
	chunks, err := buildChunks(0, model.Time(0).Add(3*24*time.Hour), 1)
	require.NoError(t, err)

	require.NoError(t, chunkStore.Put(context.Background(), chunks))

	// add a request to delete some chunks
	err = deleteStore.addDeleteRequest(context.Background(), userID, model.Now().Add(-25*time.Hour), model.Time(0).Add(24*time.Hour),
		model.Time(0).Add(2*24*time.Hour), []string{"foo"})
	require.NoError(t, err)

	// change purgerMockStorage to allow only reads. This would fail putting plans to the storage and hence fail build plans operation.
	purgerMockStorage.SetMode(chunk.MockStorageModeReadOnly)

	// pull requests to process and ensure that it has failed.
	err = purger.pullDeleteRequestsToPlanDeletes()
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "permission denied"))

	// there must be 1 delete request in process and the userID must be in failed requests list.
	require.NotNil(t, purger.inProcessRequests.get(userID))
	require.Len(t, purger.inProcessRequests.listUsersWithFailedRequest(), 1)

	// now allow writes to purgerMockStorage to allow building plans to succeed.
	purgerMockStorage.SetMode(chunk.MockStorageModeReadWrite)

	// but change mode of chunksMockStorage to read only which would deny permission to delete any chunks and in turn
	// fail to execute delete plans.
	chunksMockStorage.SetMode(chunk.MockStorageModeReadOnly)

	// retry processing of failed requests
	purger.retryFailedRequests()

	// the delete request status should now change to StatusDeleting since the building of plan should have succeeded.
	test.Poll(t, time.Second, StatusDeleting, func() interface{} {
		return purger.inProcessRequests.get(userID).Status
	})
	// the request should have failed again since we did not give permission to delete chunks.
	test.Poll(t, time.Second, 1, func() interface{} {
		return len(purger.inProcessRequests.listUsersWithFailedRequest())
	})

	// now allow writes to chunksMockStorage so the requests do not fail anymore.
	chunksMockStorage.SetMode(chunk.MockStorageModeReadWrite)

	// retry processing of failed requests.
	purger.retryFailedRequests()
	// there must be no in process requests anymore.
	test.Poll(t, time.Second, true, func() interface{} {
		return purger.inProcessRequests.get(userID) == nil
	})
	// there must be no users having failed requests.
	require.Len(t, purger.inProcessRequests.listUsersWithFailedRequest(), 0)
}

func getNonDeletedIntervals(originalInterval, deletedInterval model.Interval) []model.Interval {
	nonDeletedIntervals := []model.Interval{}
	if deletedInterval.Start > originalInterval.Start {
		nonDeletedIntervals = append(nonDeletedIntervals, model.Interval{Start: originalInterval.Start, End: deletedInterval.Start - 1})
	}

	if deletedInterval.End < originalInterval.End {
		nonDeletedIntervals = append(nonDeletedIntervals, model.Interval{Start: deletedInterval.End + 1, End: originalInterval.End})
	}

	return nonDeletedIntervals
}
