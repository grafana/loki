package purger

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql"
	"github.com/weaveworks/common/user"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/services"
)

const millisecondPerDay = int64(24 * time.Hour / time.Millisecond)

type deleteRequestWithLogger struct {
	DeleteRequest
	logger log.Logger // logger is initialized with userID and requestID to add context to every log generated using this
}

// Config holds config for DataPurger
type Config struct {
	Enable          bool   `yaml:"enable"`
	NumWorkers      int    `yaml:"num_workers"`
	ObjectStoreType string `yaml:"object_store_type"`
}

// RegisterFlags registers CLI flags for Config
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.Enable, "purger.enable", false, "Enable purger to allow deletion of series. Be aware that Delete series feature is still experimental")
	f.IntVar(&cfg.NumWorkers, "purger.num-workers", 2, "Number of workers executing delete plans in parallel")
	f.StringVar(&cfg.ObjectStoreType, "purger.object-store-type", "", "Name of the object store to use for storing delete plans")
}

type workerJob struct {
	planNo          int
	userID          string
	deleteRequestID string
	logger          log.Logger
}

// DataPurger does the purging of data which is requested to be deleted
type DataPurger struct {
	services.Service

	cfg          Config
	deleteStore  *DeleteStore
	chunkStore   chunk.Store
	objectClient chunk.ObjectClient

	executePlansChan chan deleteRequestWithLogger
	workerJobChan    chan workerJob

	// we would only allow processing of singe delete request at a time since delete requests touching same chunks could change the chunk IDs of partially deleted chunks
	// and break the purge plan for other requests
	inProcessRequestIDs    map[string]string
	inProcessRequestIDsMtx sync.RWMutex

	pendingPlansCount    map[string]int // per request pending plan count
	pendingPlansCountMtx sync.Mutex

	wg sync.WaitGroup
}

// NewDataPurger creates a new DataPurger
func NewDataPurger(cfg Config, deleteStore *DeleteStore, chunkStore chunk.Store, storageClient chunk.ObjectClient) (*DataPurger, error) {
	dataPurger := DataPurger{
		cfg:                 cfg,
		deleteStore:         deleteStore,
		chunkStore:          chunkStore,
		objectClient:        storageClient,
		executePlansChan:    make(chan deleteRequestWithLogger, 50),
		workerJobChan:       make(chan workerJob, 50),
		inProcessRequestIDs: map[string]string{},
		pendingPlansCount:   map[string]int{},
	}

	dataPurger.Service = services.NewTimerService(time.Hour, dataPurger.init, dataPurger.runOneIteration, dataPurger.stop)
	return &dataPurger, nil
}

// Run keeps pulling delete requests for planning after initializing necessary things
func (dp *DataPurger) runOneIteration(ctx context.Context) error {
	err := dp.pullDeleteRequestsToPlanDeletes()
	if err != nil {
		level.Error(util.Logger).Log("msg", "error pulling delete requests for building plans", "err", err)
	}
	// Don't return error here, or Timer service will stop.
	return nil
}

// init starts workers, scheduler and then loads in process delete requests
func (dp *DataPurger) init(ctx context.Context) error {
	for i := 0; i < dp.cfg.NumWorkers; i++ {
		dp.wg.Add(1)
		go dp.worker()
	}

	dp.wg.Add(1)
	go dp.jobScheduler(ctx)

	return dp.loadInprocessDeleteRequests()
}

// Stop waits until all background tasks stop.
func (dp *DataPurger) stop(_ error) error {
	dp.wg.Wait()
	return nil
}

func (dp *DataPurger) workerJobCleanup(job workerJob) {
	err := dp.removeDeletePlan(context.Background(), job.userID, job.deleteRequestID, job.planNo)
	if err != nil {
		level.Error(job.logger).Log("msg", "error removing delete plan",
			"plan_no", job.planNo, "err", err)
		return
	}

	dp.pendingPlansCountMtx.Lock()
	dp.pendingPlansCount[job.deleteRequestID]--

	if dp.pendingPlansCount[job.deleteRequestID] == 0 {
		level.Info(job.logger).Log("msg", "finished execution of all plans, cleaning up and updating status of request")

		err := dp.deleteStore.UpdateStatus(context.Background(), job.userID, job.deleteRequestID, StatusProcessed)
		if err != nil {
			level.Error(job.logger).Log("msg", "error updating delete request status to process", "err", err)
		}

		delete(dp.pendingPlansCount, job.deleteRequestID)
		dp.pendingPlansCountMtx.Unlock()

		dp.inProcessRequestIDsMtx.Lock()
		delete(dp.inProcessRequestIDs, job.userID)
		dp.inProcessRequestIDsMtx.Unlock()
	} else {
		dp.pendingPlansCountMtx.Unlock()
	}
}

// we send all the delete plans to workerJobChan
func (dp *DataPurger) jobScheduler(ctx context.Context) {
	defer dp.wg.Done()

	for {
		select {
		case req := <-dp.executePlansChan:
			numPlans := numPlans(req.StartTime, req.EndTime)
			level.Info(req.logger).Log("msg", "sending jobs to workers for purging data", "num_jobs", numPlans)

			dp.pendingPlansCountMtx.Lock()
			dp.pendingPlansCount[req.RequestID] = numPlans
			dp.pendingPlansCountMtx.Unlock()

			for i := 0; i < numPlans; i++ {
				dp.workerJobChan <- workerJob{planNo: i, userID: req.UserID,
					deleteRequestID: req.RequestID, logger: req.logger}
			}
		case <-ctx.Done():
			close(dp.workerJobChan)
			return
		}
	}
}

func (dp *DataPurger) worker() {
	defer dp.wg.Done()

	for job := range dp.workerJobChan {
		err := dp.executePlan(job.userID, job.deleteRequestID, job.planNo, job.logger)
		if err != nil {
			level.Error(job.logger).Log("msg", "error executing delete plan",
				"plan_no", job.planNo, "err", err)
			continue
		}

		dp.workerJobCleanup(job)
	}
}

func (dp *DataPurger) executePlan(userID, requestID string, planNo int, logger log.Logger) error {
	logger = log.With(logger, "plan_no", planNo)

	plan, err := dp.getDeletePlan(context.Background(), userID, requestID, planNo)
	if err != nil {
		if err == chunk.ErrStorageObjectNotFound {
			level.Info(logger).Log("msg", "plan not found, must have been executed already")
			// this means plan was already executed and got removed. Do nothing.
			return nil
		}
		return err
	}

	level.Info(logger).Log("msg", "executing plan")

	ctx := user.InjectOrgID(context.Background(), userID)

	for i := range plan.ChunksGroup {
		level.Debug(logger).Log("msg", "deleting chunks", "labels", plan.ChunksGroup[i].Labels)

		for _, chunkDetails := range plan.ChunksGroup[i].Chunks {
			chunkRef, err := chunk.ParseExternalKey(userID, chunkDetails.ID)
			if err != nil {
				return err
			}

			var partiallyDeletedInterval *model.Interval = nil
			if chunkDetails.PartiallyDeletedInterval != nil {
				partiallyDeletedInterval = &model.Interval{
					Start: model.Time(chunkDetails.PartiallyDeletedInterval.StartTimestampMs),
					End:   model.Time(chunkDetails.PartiallyDeletedInterval.EndTimestampMs),
				}
			}

			err = dp.chunkStore.DeleteChunk(ctx, chunkRef.From, chunkRef.Through, chunkRef.UserID,
				chunkDetails.ID, client.FromLabelAdaptersToLabels(plan.ChunksGroup[i].Labels), partiallyDeletedInterval)
			if err != nil {
				if isMissingChunkErr(err) {
					level.Error(logger).Log("msg", "chunk not found for deletion. We may have already deleted it",
						"chunk_id", chunkDetails.ID)
					continue
				}
				return err
			}
		}

		level.Debug(logger).Log("msg", "deleting series", "labels", plan.ChunksGroup[i].Labels)

		// this is mostly required to clean up series ids from series store
		err := dp.chunkStore.DeleteSeriesIDs(ctx, model.Time(plan.PlanInterval.StartTimestampMs), model.Time(plan.PlanInterval.EndTimestampMs),
			userID, client.FromLabelAdaptersToLabels(plan.ChunksGroup[i].Labels))
		if err != nil {
			return err
		}
	}

	level.Info(logger).Log("msg", "finished execution of plan")

	return nil
}

// we need to load all in process delete requests on startup to finish them first
func (dp *DataPurger) loadInprocessDeleteRequests() error {
	requestsWithBuildingPlanStatus, err := dp.deleteStore.GetDeleteRequestsByStatus(context.Background(), StatusBuildingPlan)
	if err != nil {
		return err
	}

	for _, deleteRequest := range requestsWithBuildingPlanStatus {
		req := makeDeleteRequestWithLogger(deleteRequest, util.Logger)

		level.Info(req.logger).Log("msg", "loaded in process delete requests with status building plan")

		dp.inProcessRequestIDs[deleteRequest.UserID] = deleteRequest.RequestID
		err := dp.buildDeletePlan(req)
		if err != nil {
			level.Error(req.logger).Log("msg", "error building delete plan", "err", err)
		}

		level.Info(req.logger).Log("msg", "sending delete request for execution")
		dp.executePlansChan <- req
	}

	requestsWithDeletingStatus, err := dp.deleteStore.GetDeleteRequestsByStatus(context.Background(), StatusDeleting)
	if err != nil {
		return err
	}

	for _, deleteRequest := range requestsWithDeletingStatus {
		req := makeDeleteRequestWithLogger(deleteRequest, util.Logger)
		level.Info(req.logger).Log("msg", "loaded in process delete requests with status deleting")

		dp.inProcessRequestIDs[deleteRequest.UserID] = deleteRequest.RequestID
		dp.executePlansChan <- req
	}

	return nil
}

// pullDeleteRequestsToPlanDeletes pulls delete requests which do not have their delete plans built yet and sends them for building delete plans
// after pulling delete requests for building plans, it updates its status to StatusBuildingPlan status to avoid picking this up again next time
func (dp *DataPurger) pullDeleteRequestsToPlanDeletes() error {
	deleteRequests, err := dp.deleteStore.GetDeleteRequestsByStatus(context.Background(), StatusReceived)
	if err != nil {
		return err
	}

	for _, deleteRequest := range deleteRequests {
		if deleteRequest.CreatedAt.Add(24 * time.Hour).After(model.Now()) {
			continue
		}

		dp.inProcessRequestIDsMtx.RLock()
		inprocessDeleteRequstID := dp.inProcessRequestIDs[deleteRequest.UserID]
		dp.inProcessRequestIDsMtx.RUnlock()

		if inprocessDeleteRequstID != "" {
			level.Debug(util.Logger).Log("msg", "skipping delete request processing for now since another request from same user is already in process",
				"inprocess_request_id", inprocessDeleteRequstID,
				"skipped_request_id", deleteRequest.RequestID, "user_id", deleteRequest.UserID)
			continue
		}

		err = dp.deleteStore.UpdateStatus(context.Background(), deleteRequest.UserID, deleteRequest.RequestID, StatusBuildingPlan)
		if err != nil {
			return err
		}

		dp.inProcessRequestIDsMtx.Lock()
		dp.inProcessRequestIDs[deleteRequest.UserID] = deleteRequest.RequestID
		dp.inProcessRequestIDsMtx.Unlock()

		req := makeDeleteRequestWithLogger(deleteRequest, util.Logger)

		level.Info(req.logger).Log("msg", "building plan for a new delete request")

		err := dp.buildDeletePlan(req)
		if err != nil {
			// We do not want to remove this delete request from inProcessRequestIDs to make sure
			// we do not move multiple deleting requests in deletion process.
			// None of the other delete requests from the user would be considered for processing until then.
			level.Error(req.logger).Log("msg", "error building delete plan", "err", err)
			return err
		}

		level.Info(req.logger).Log("msg", "sending delete request for execution")
		dp.executePlansChan <- req
	}

	return nil
}

// buildDeletePlan builds per day delete plan for given delete requests.
// A days plan will include chunk ids and labels of all the chunks which are supposed to be deleted.
// Chunks are grouped together by labels to avoid storing labels repetitively.
// After building delete plans it updates status of delete request to StatusDeleting and sends it for execution
func (dp *DataPurger) buildDeletePlan(req deleteRequestWithLogger) error {
	ctx := context.Background()
	ctx = user.InjectOrgID(ctx, req.UserID)

	perDayTimeRange := splitByDay(req.StartTime, req.EndTime)
	level.Info(req.logger).Log("msg", "building delete plan", "num_plans", len(perDayTimeRange))

	plans := make([][]byte, len(perDayTimeRange))
	for i, planRange := range perDayTimeRange {
		chunksGroups := []ChunksGroup{}

		for _, selector := range req.Selectors {
			matchers, err := promql.ParseMetricSelector(selector)
			if err != nil {
				return err
			}

			// ToDo: remove duplicate chunks
			chunks, err := dp.chunkStore.Get(ctx, req.UserID, planRange.Start, planRange.End, matchers...)
			if err != nil {
				return err
			}

			chunksGroups = append(chunksGroups, groupChunks(chunks, req.StartTime, req.EndTime)...)
		}

		plan := DeletePlan{
			PlanInterval: &Interval{
				StartTimestampMs: int64(planRange.Start),
				EndTimestampMs:   int64(planRange.End),
			},
			ChunksGroup: chunksGroups,
		}

		pb, err := proto.Marshal(&plan)
		if err != nil {
			return err
		}

		plans[i] = pb
	}

	err := dp.putDeletePlans(ctx, req.UserID, req.RequestID, plans)
	if err != nil {
		return err
	}

	err = dp.deleteStore.UpdateStatus(ctx, req.UserID, req.RequestID, StatusDeleting)
	if err != nil {
		return err
	}

	level.Info(req.logger).Log("msg", "built delete plans", "num_plans", len(perDayTimeRange))

	return nil
}

func (dp *DataPurger) putDeletePlans(ctx context.Context, userID, requestID string, plans [][]byte) error {
	for i, plan := range plans {
		objectKey := buildObjectKeyForPlan(userID, requestID, i)

		err := dp.objectClient.PutObject(ctx, objectKey, bytes.NewReader(plan))
		if err != nil {
			return err
		}
	}

	return nil
}

func (dp *DataPurger) getDeletePlan(ctx context.Context, userID, requestID string, planNo int) (*DeletePlan, error) {
	objectKey := buildObjectKeyForPlan(userID, requestID, planNo)

	readCloser, err := dp.objectClient.GetObject(ctx, objectKey)
	if err != nil {
		return nil, err
	}

	defer readCloser.Close()

	buf, err := ioutil.ReadAll(readCloser)
	if err != nil {
		return nil, err
	}

	var plan DeletePlan
	err = proto.Unmarshal(buf, &plan)
	if err != nil {
		return nil, err
	}

	return &plan, nil
}

func (dp *DataPurger) removeDeletePlan(ctx context.Context, userID, requestID string, planNo int) error {
	objectKey := buildObjectKeyForPlan(userID, requestID, planNo)
	return dp.objectClient.DeleteObject(ctx, objectKey)
}

// returns interval per plan
func splitByDay(start, end model.Time) []model.Interval {
	numOfDays := numPlans(start, end)

	perDayTimeRange := make([]model.Interval, numOfDays)
	startOfNextDay := model.Time(((int64(start) / millisecondPerDay) + 1) * millisecondPerDay)
	perDayTimeRange[0] = model.Interval{Start: start, End: startOfNextDay - 1}

	for i := 1; i < numOfDays; i++ {
		interval := model.Interval{Start: startOfNextDay}
		startOfNextDay += model.Time(millisecondPerDay)
		interval.End = startOfNextDay - 1
		perDayTimeRange[i] = interval
	}

	perDayTimeRange[numOfDays-1].End = end

	return perDayTimeRange
}

func numPlans(start, end model.Time) int {
	// rounding down start to start of the day
	if start%model.Time(millisecondPerDay) != 0 {
		start = model.Time((int64(start) / millisecondPerDay) * millisecondPerDay)
	}

	// rounding up end to end of the day
	if end%model.Time(millisecondPerDay) != 0 {
		end = model.Time((int64(end)/millisecondPerDay)*millisecondPerDay + millisecondPerDay)
	}

	return int(int64(end-start) / millisecondPerDay)
}

// groups chunks together by unique label sets i.e all the chunks with same labels would be stored in a group
// chunk details are stored in groups for each unique label set to avoid storing them repetitively for each chunk
func groupChunks(chunks []chunk.Chunk, deleteFrom, deleteThrough model.Time) []ChunksGroup {
	metricToChunks := make(map[string]ChunksGroup)

	for _, chk := range chunks {
		// chunk.Metric are assumed to be sorted which should give same value from String() for same series.
		// If they stop being sorted then in the worst case we would lose the benefit of grouping chunks to avoid storing labels repetitively.
		metricString := chk.Metric.String()
		group, ok := metricToChunks[metricString]
		if !ok {
			group = ChunksGroup{Labels: client.FromLabelsToLabelAdapters(chk.Metric)}
		}

		chunkDetails := ChunkDetails{ID: chk.ExternalKey()}

		if deleteFrom > chk.From || deleteThrough < chk.Through {
			partiallyDeletedInterval := Interval{StartTimestampMs: int64(chk.From), EndTimestampMs: int64(chk.Through)}

			if deleteFrom > chk.From {
				partiallyDeletedInterval.StartTimestampMs = int64(deleteFrom)
			}

			if deleteThrough < chk.Through {
				partiallyDeletedInterval.EndTimestampMs = int64(deleteThrough)
			}
			chunkDetails.PartiallyDeletedInterval = &partiallyDeletedInterval
		}

		group.Chunks = append(group.Chunks, chunkDetails)
		metricToChunks[metricString] = group
	}

	chunksGroups := make([]ChunksGroup, 0, len(metricToChunks))

	for _, group := range metricToChunks {
		chunksGroups = append(chunksGroups, group)
	}

	return chunksGroups
}

func isMissingChunkErr(err error) bool {
	if err == chunk.ErrStorageObjectNotFound {
		return true
	}
	if promqlStorageErr, ok := err.(promql.ErrStorage); ok && promqlStorageErr.Err == chunk.ErrStorageObjectNotFound {
		return true
	}

	return false
}

func buildObjectKeyForPlan(userID, requestID string, planNo int) string {
	return fmt.Sprintf("%s:%s/%d", userID, requestID, planNo)
}

func makeDeleteRequestWithLogger(deleteRequest DeleteRequest, l log.Logger) deleteRequestWithLogger {
	logger := log.With(l, "user_id", deleteRequest.UserID, "request_id", deleteRequest.RequestID)
	return deleteRequestWithLogger{deleteRequest, logger}
}
