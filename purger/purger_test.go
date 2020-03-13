package purger

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/testutils"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/cortexproject/cortex/pkg/util/services"
)

const (
	userID        = "userID"
	modelTimeDay  = model.Time(millisecondPerDay)
	modelTimeHour = model.Time(time.Hour / time.Millisecond)
)

func setupTestDeleteStore() (*DeleteStore, error) {
	var deleteStoreConfig DeleteStoreConfig
	flagext.DefaultValues(&deleteStoreConfig)

	mockStorage := chunk.NewMockStorage()

	err := mockStorage.CreateTable(context.Background(), chunk.TableDesc{
		Name: deleteStoreConfig.RequestsTableName,
	})
	if err != nil {
		return nil, err
	}

	return NewDeleteStore(deleteStoreConfig, mockStorage)
}

func setupStoresAndPurger(t *testing.T) (*DeleteStore, chunk.Store, chunk.ObjectClient, *DataPurger) {
	deleteStore, err := setupTestDeleteStore()
	require.NoError(t, err)

	chunkStore, err := testutils.SetupTestChunkStore()
	require.NoError(t, err)

	storageClient, err := testutils.SetupTestObjectStore()
	require.NoError(t, err)

	var cfg Config
	flagext.DefaultValues(&cfg)

	dataPurger, err := NewDataPurger(cfg, deleteStore, chunkStore, storageClient)
	require.NoError(t, err)

	return deleteStore, chunkStore, storageClient, dataPurger
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
		lastChunkPartialDeletionInterval: &Interval{StartTimestampMs: int64(millisecondPerDay / 2),
			EndTimestampMs: int64(millisecondPerDay / 2)},
	},
	{
		name:                   "deleting a full day from 2 days data",
		chunkStoreDataInterval: model.Interval{End: modelTimeDay * 2},
		deleteRequestInterval:  model.Interval{End: modelTimeDay},
		expectedNumberOfPlans:  1,
		numChunksToDelete:      24 + 1, // one chunk for each hour + end time touches chunk at boundary
		lastChunkPartialDeletionInterval: &Interval{StartTimestampMs: millisecondPerDay,
			EndTimestampMs: millisecondPerDay},
	},
	{
		name:                   "deleting 2 days partially from 2 days data",
		chunkStoreDataInterval: model.Interval{End: modelTimeDay * 2},
		deleteRequestInterval: model.Interval{Start: model.Time(millisecondPerDay / 2),
			End: model.Time(millisecondPerDay + millisecondPerDay/2)},
		expectedNumberOfPlans: 2,
		numChunksToDelete:     24 + 2, // one chunk for each hour + start and end time touches chunk at boundary
		firstChunkPartialDeletionInterval: &Interval{StartTimestampMs: int64(millisecondPerDay / 2),
			EndTimestampMs: int64(millisecondPerDay / 2)},
		lastChunkPartialDeletionInterval: &Interval{StartTimestampMs: millisecondPerDay + millisecondPerDay/2,
			EndTimestampMs: millisecondPerDay + millisecondPerDay/2},
	},
	{
		name:                   "deleting 2 days partially, not aligned with hour, from 2 days data",
		chunkStoreDataInterval: model.Interval{End: modelTimeDay * 2},
		deleteRequestInterval: model.Interval{Start: model.Time(millisecondPerDay / 2).Add(time.Minute),
			End: model.Time(millisecondPerDay + millisecondPerDay/2).Add(-time.Minute)},
		expectedNumberOfPlans: 2,
		numChunksToDelete:     24, // one chunk for each hour, no chunks touched at boundary
		firstChunkPartialDeletionInterval: &Interval{StartTimestampMs: int64(model.Time(millisecondPerDay / 2).Add(time.Minute)),
			EndTimestampMs: int64(model.Time(millisecondPerDay / 2).Add(time.Hour))},
		lastChunkPartialDeletionInterval: &Interval{StartTimestampMs: int64(model.Time(millisecondPerDay + millisecondPerDay/2).Add(-time.Hour)),
			EndTimestampMs: int64(model.Time(millisecondPerDay + millisecondPerDay/2).Add(-time.Minute))},
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
		firstChunkPartialDeletionInterval: &Interval{StartTimestampMs: int64(modelTimeDay.Add(-30 * time.Minute)),
			EndTimestampMs: int64(modelTimeDay.Add(-15 * time.Minute))},
	},
}

func TestDataPurger_BuildPlan(t *testing.T) {
	for _, tc := range purgePlanTestCases {
		for batchSize := 1; batchSize <= 5; batchSize++ {
			t.Run(fmt.Sprintf("%s/batch-size=%d", tc.name, batchSize), func(t *testing.T) {
				deleteStore, chunkStore, storageClient, dataPurger := setupStoresAndPurger(t)
				defer func() {
					dataPurger.StopAsync()
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
				requestWithLogger := makeDeleteRequestWithLogger(deleteRequest, util.Logger)

				err = dataPurger.buildDeletePlan(requestWithLogger)
				require.NoError(t, err)
				planPath := fmt.Sprintf("%s:%s/", userID, deleteRequest.RequestID)

				plans, err := storageClient.List(context.Background(), planPath)
				require.NoError(t, err)
				require.Equal(t, tc.expectedNumberOfPlans, len(plans))

				numPlans := tc.expectedNumberOfPlans
				var nilPurgePlanInterval *Interval
				numChunks := 0

				chunkIDs := map[string]struct{}{}

				for i := range plans {
					deletePlan, err := dataPurger.getDeletePlan(context.Background(), userID, deleteRequest.RequestID, i)
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
			})
		}
	}
}

func TestDataPurger_ExecutePlan(t *testing.T) {
	fooMetricNameMatcher, err := promql.ParseMetricSelector(`foo`)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range purgePlanTestCases {
		for batchSize := 1; batchSize <= 5; batchSize++ {
			t.Run(fmt.Sprintf("%s/batch-size=%d", tc.name, batchSize), func(t *testing.T) {
				deleteStore, chunkStore, _, dataPurger := setupStoresAndPurger(t)
				defer func() {
					dataPurger.StopAsync()
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
				requestWithLogger := makeDeleteRequestWithLogger(deleteRequest, util.Logger)
				err = dataPurger.buildDeletePlan(requestWithLogger)
				require.NoError(t, err)

				// execute all the plans
				for i := 0; i < tc.expectedNumberOfPlans; i++ {
					err := dataPurger.executePlan(userID, deleteRequest.RequestID, i, requestWithLogger.logger)
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

func TestDataPurger_Restarts(t *testing.T) {
	fooMetricNameMatcher, err := promql.ParseMetricSelector(`foo`)
	if err != nil {
		t.Fatal(err)
	}

	deleteStore, chunkStore, storageClient, dataPurger := setupStoresAndPurger(t)
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
	requestWithLogger := makeDeleteRequestWithLogger(deleteRequest, util.Logger)
	err = dataPurger.buildDeletePlan(requestWithLogger)
	require.NoError(t, err)

	// stop the existing purger
	require.NoError(t, services.StopAndAwaitTerminated(context.Background(), dataPurger))

	// create a new purger to check whether it picks up in process delete requests
	var cfg Config
	flagext.DefaultValues(&cfg)
	newPurger, err := NewDataPurger(cfg, deleteStore, chunkStore, storageClient)
	require.NoError(t, err)

	// load in process delete requests by calling Run
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), newPurger))

	defer newPurger.StopAsync()

	// lets wait till purger finishes execution of in process delete requests
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	for ctx.Err() == nil {
		newPurger.inProcessRequestIDsMtx.RLock()

		if len(newPurger.inProcessRequestIDs) == 0 {
			newPurger.inProcessRequestIDsMtx.RUnlock()
			break
		}

		newPurger.inProcessRequestIDsMtx.RUnlock()
		time.Sleep(time.Second / 2)
	}
	require.NoError(t, ctx.Err())

	// check whether data got deleted from the store since delete request has been processed
	chunks, err = chunkStore.Get(context.Background(), userID, 0, model.Time(0).Add(10*24*time.Hour), fooMetricNameMatcher...)
	require.NoError(t, err)

	// we are deleting 7 days out of 10 so there should we 3 days data left in store which means 72 chunks
	require.Equal(t, 72, len(chunks))

	deleteRequests, err = deleteStore.GetAllDeleteRequestsForUser(context.Background(), userID)
	require.NoError(t, err)
	require.Equal(t, StatusProcessed, deleteRequests[0].Status)
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
