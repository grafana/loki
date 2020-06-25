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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/weaveworks/common/user"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/services"
)

const (
	millisecondPerDay = int64(24 * time.Hour / time.Millisecond)
	statusSuccess     = "success"
	statusFail        = "fail"
)

type purgerMetrics struct {
	deleteRequestsProcessedTotal         *prometheus.CounterVec
	deleteRequestsChunksSelectedTotal    *prometheus.CounterVec
	deleteRequestsProcessingFailures     *prometheus.CounterVec
	loadPendingRequestsAttempsTotal      *prometheus.CounterVec
	oldestPendingDeleteRequestAgeSeconds prometheus.Gauge
	pendingDeleteRequestsCount           prometheus.Gauge
}

func newPurgerMetrics(r prometheus.Registerer) *purgerMetrics {
	m := purgerMetrics{}

	m.deleteRequestsProcessedTotal = promauto.With(r).NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "purger_delete_requests_processed_total",
		Help:      "Number of delete requests processed per user",
	}, []string{"user"})
	m.deleteRequestsChunksSelectedTotal = promauto.With(r).NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "purger_delete_requests_chunks_selected_total",
		Help:      "Number of chunks selected while building delete plans per user",
	}, []string{"user"})
	m.deleteRequestsProcessingFailures = promauto.With(r).NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "purger_delete_requests_processing_failures_total",
		Help:      "Number of delete requests processing failures per user",
	}, []string{"user"})
	m.loadPendingRequestsAttempsTotal = promauto.With(r).NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "purger_load_pending_requests_attempts_total",
		Help:      "Number of attempts that were made to load pending requests with status",
	}, []string{"status"})
	m.oldestPendingDeleteRequestAgeSeconds = promauto.With(r).NewGauge(prometheus.GaugeOpts{
		Namespace: "cortex",
		Name:      "purger_oldest_pending_delete_request_age_seconds",
		Help:      "Age of oldest pending delete request in seconds",
	})
	m.pendingDeleteRequestsCount = promauto.With(r).NewGauge(prometheus.GaugeOpts{
		Namespace: "cortex",
		Name:      "purger_pending_delete_requests_count",
		Help:      "Count of requests which are in process or are ready to be processed",
	})

	return &m
}

type deleteRequestWithLogger struct {
	DeleteRequest
	logger log.Logger // logger is initialized with userID and requestID to add context to every log generated using this
}

// Config holds config for Purger
type Config struct {
	Enable                    bool          `yaml:"enable"`
	NumWorkers                int           `yaml:"num_workers"`
	ObjectStoreType           string        `yaml:"object_store_type"`
	DeleteRequestCancelPeriod time.Duration `yaml:"delete_request_cancel_period"`
}

// RegisterFlags registers CLI flags for Config
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.Enable, "purger.enable", false, "Enable purger to allow deletion of series. Be aware that Delete series feature is still experimental")
	f.IntVar(&cfg.NumWorkers, "purger.num-workers", 2, "Number of workers executing delete plans in parallel")
	f.StringVar(&cfg.ObjectStoreType, "purger.object-store-type", "", "Name of the object store to use for storing delete plans")
	f.DurationVar(&cfg.DeleteRequestCancelPeriod, "purger.delete-request-cancel-period", 24*time.Hour, "Allow cancellation of delete request until duration after they are created. Data would be deleted only after delete requests have been older than this duration. Ideally this should be set to at least 24h.")
}

type workerJob struct {
	planNo          int
	userID          string
	deleteRequestID string
	logger          log.Logger
}

// Purger does the purging of data which is requested to be deleted
type Purger struct {
	services.Service

	cfg          Config
	deleteStore  *DeleteStore
	chunkStore   chunk.Store
	objectClient chunk.ObjectClient
	metrics      *purgerMetrics

	executePlansChan chan deleteRequestWithLogger
	workerJobChan    chan workerJob

	// we would only allow processing of singe delete request at a time since delete requests touching same chunks could change the chunk IDs of partially deleted chunks
	// and break the purge plan for other requests
	inProcessRequests      map[string]DeleteRequest
	inProcessRequestIDsMtx sync.RWMutex

	// We do not want to limit pulling new delete requests to a fixed interval which otherwise would limit number of delete requests we process per user.
	// While loading delete requests if we find more requests from user pending to be processed, we just set their id in usersWithPendingRequests and
	// when a user's delete request gets processed we just check this map to see whether we want to load more requests without waiting for next ticker to load new batch.
	usersWithPendingRequests    map[string]struct{}
	usersWithPendingRequestsMtx sync.Mutex
	pullNewRequestsChan         chan struct{}

	pendingPlansCount    map[string]int // per request pending plan count
	pendingPlansCountMtx sync.Mutex

	wg sync.WaitGroup
}

// NewPurger creates a new Purger
func NewPurger(cfg Config, deleteStore *DeleteStore, chunkStore chunk.Store, storageClient chunk.ObjectClient, registerer prometheus.Registerer) (*Purger, error) {
	util.WarnExperimentalUse("Delete series API")

	purger := Purger{
		cfg:                      cfg,
		deleteStore:              deleteStore,
		chunkStore:               chunkStore,
		objectClient:             storageClient,
		metrics:                  newPurgerMetrics(registerer),
		pullNewRequestsChan:      make(chan struct{}, 1),
		executePlansChan:         make(chan deleteRequestWithLogger, 50),
		workerJobChan:            make(chan workerJob, 50),
		inProcessRequests:        map[string]DeleteRequest{},
		usersWithPendingRequests: map[string]struct{}{},
		pendingPlansCount:        map[string]int{},
	}

	purger.Service = services.NewBasicService(purger.init, purger.loop, purger.stop)
	return &purger, nil
}

// init starts workers, scheduler and then loads in process delete requests
func (p *Purger) init(ctx context.Context) error {
	for i := 0; i < p.cfg.NumWorkers; i++ {
		p.wg.Add(1)
		go p.worker()
	}

	p.wg.Add(1)
	go p.jobScheduler(ctx)

	return p.loadInprocessDeleteRequests()
}

func (p *Purger) loop(ctx context.Context) error {
	loadRequests := func() {
		status := statusSuccess

		err := p.pullDeleteRequestsToPlanDeletes()
		if err != nil {
			status = statusFail
			level.Error(util.Logger).Log("msg", "error pulling delete requests for building plans", "err", err)
		}

		p.metrics.loadPendingRequestsAttempsTotal.WithLabelValues(status).Inc()
	}

	// load requests on startup instead of waiting for first ticker
	loadRequests()

	loadRequestsTicker := time.NewTicker(time.Hour)
	defer loadRequestsTicker.Stop()

	for {
		select {
		case <-loadRequestsTicker.C:
			loadRequests()
		case <-p.pullNewRequestsChan:
			loadRequests()
		case <-ctx.Done():
			return nil
		}
	}
}

// Stop waits until all background tasks stop.
func (p *Purger) stop(_ error) error {
	p.wg.Wait()
	return nil
}

func (p *Purger) workerJobCleanup(job workerJob) {
	err := p.removeDeletePlan(context.Background(), job.userID, job.deleteRequestID, job.planNo)
	if err != nil {
		level.Error(job.logger).Log("msg", "error removing delete plan",
			"plan_no", job.planNo, "err", err)
		return
	}

	p.pendingPlansCountMtx.Lock()
	p.pendingPlansCount[job.deleteRequestID]--

	if p.pendingPlansCount[job.deleteRequestID] == 0 {
		level.Info(job.logger).Log("msg", "finished execution of all plans, cleaning up and updating status of request")

		err := p.deleteStore.UpdateStatus(context.Background(), job.userID, job.deleteRequestID, StatusProcessed)
		if err != nil {
			level.Error(job.logger).Log("msg", "error updating delete request status to process", "err", err)
		}

		p.metrics.deleteRequestsProcessedTotal.WithLabelValues(job.userID).Inc()
		delete(p.pendingPlansCount, job.deleteRequestID)
		p.pendingPlansCountMtx.Unlock()

		p.inProcessRequestIDsMtx.Lock()
		delete(p.inProcessRequests, job.userID)
		p.inProcessRequestIDsMtx.Unlock()

		// request loading of more delete request if
		// - user has more pending requests and
		// - we do not have a pending request to load more requests
		p.usersWithPendingRequestsMtx.Lock()
		defer p.usersWithPendingRequestsMtx.Unlock()
		if _, ok := p.usersWithPendingRequests[job.userID]; ok {
			delete(p.usersWithPendingRequests, job.userID)
			select {
			case p.pullNewRequestsChan <- struct{}{}:
				// sent
			default:
				// already sent
			}
		}
	} else {
		p.pendingPlansCountMtx.Unlock()
	}
}

// we send all the delete plans to workerJobChan
func (p *Purger) jobScheduler(ctx context.Context) {
	defer p.wg.Done()

	for {
		select {
		case req := <-p.executePlansChan:
			numPlans := numPlans(req.StartTime, req.EndTime)
			level.Info(req.logger).Log("msg", "sending jobs to workers for purging data", "num_jobs", numPlans)

			p.pendingPlansCountMtx.Lock()
			p.pendingPlansCount[req.RequestID] = numPlans
			p.pendingPlansCountMtx.Unlock()

			for i := 0; i < numPlans; i++ {
				p.workerJobChan <- workerJob{planNo: i, userID: req.UserID,
					deleteRequestID: req.RequestID, logger: req.logger}
			}
		case <-ctx.Done():
			close(p.workerJobChan)
			return
		}
	}
}

func (p *Purger) worker() {
	defer p.wg.Done()

	for job := range p.workerJobChan {
		err := p.executePlan(job.userID, job.deleteRequestID, job.planNo, job.logger)
		if err != nil {
			p.metrics.deleteRequestsProcessingFailures.WithLabelValues(job.userID).Inc()
			level.Error(job.logger).Log("msg", "error executing delete plan",
				"plan_no", job.planNo, "err", err)
			continue
		}

		p.workerJobCleanup(job)
	}
}

func (p *Purger) executePlan(userID, requestID string, planNo int, logger log.Logger) error {
	logger = log.With(logger, "plan_no", planNo)

	plan, err := p.getDeletePlan(context.Background(), userID, requestID, planNo)
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

			err = p.chunkStore.DeleteChunk(ctx, chunkRef.From, chunkRef.Through, chunkRef.UserID,
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
		err := p.chunkStore.DeleteSeriesIDs(ctx, model.Time(plan.PlanInterval.StartTimestampMs), model.Time(plan.PlanInterval.EndTimestampMs),
			userID, client.FromLabelAdaptersToLabels(plan.ChunksGroup[i].Labels))
		if err != nil {
			return err
		}
	}

	level.Info(logger).Log("msg", "finished execution of plan")

	return nil
}

// we need to load all in process delete requests on startup to finish them first
func (p *Purger) loadInprocessDeleteRequests() error {
	requestsWithBuildingPlanStatus, err := p.deleteStore.GetDeleteRequestsByStatus(context.Background(), StatusBuildingPlan)
	if err != nil {
		return err
	}

	for _, deleteRequest := range requestsWithBuildingPlanStatus {
		req := makeDeleteRequestWithLogger(deleteRequest, util.Logger)

		level.Info(req.logger).Log("msg", "loaded in process delete requests with status building plan")

		p.inProcessRequests[deleteRequest.UserID] = deleteRequest
		err := p.buildDeletePlan(req)
		if err != nil {
			p.metrics.deleteRequestsProcessingFailures.WithLabelValues(deleteRequest.UserID).Inc()
			level.Error(req.logger).Log("msg", "error building delete plan", "err", err)
			continue
		}

		level.Info(req.logger).Log("msg", "sending delete request for execution")
		p.executePlansChan <- req
	}

	requestsWithDeletingStatus, err := p.deleteStore.GetDeleteRequestsByStatus(context.Background(), StatusDeleting)
	if err != nil {
		return err
	}

	for _, deleteRequest := range requestsWithDeletingStatus {
		req := makeDeleteRequestWithLogger(deleteRequest, util.Logger)
		level.Info(req.logger).Log("msg", "loaded in process delete requests with status deleting")

		p.inProcessRequests[deleteRequest.UserID] = deleteRequest
		p.executePlansChan <- req
	}

	return nil
}

// pullDeleteRequestsToPlanDeletes pulls delete requests which do not have their delete plans built yet and sends them for building delete plans
// after pulling delete requests for building plans, it updates its status to StatusBuildingPlan status to avoid picking this up again next time
func (p *Purger) pullDeleteRequestsToPlanDeletes() error {
	deleteRequests, err := p.deleteStore.GetDeleteRequestsByStatus(context.Background(), StatusReceived)
	if err != nil {
		return err
	}

	p.inProcessRequestIDsMtx.RLock()
	pendingDeleteRequestsCount := len(p.inProcessRequests)
	p.inProcessRequestIDsMtx.RUnlock()

	now := model.Now()
	oldestPendingRequestCreatedAt := now

	// requests which are still being processed are also considered pending
	if pendingDeleteRequestsCount != 0 {
		oldestInProcessRequest := p.getOldestInProcessRequest()
		if oldestInProcessRequest != nil {
			oldestPendingRequestCreatedAt = oldestInProcessRequest.CreatedAt
		}
	}

	for _, deleteRequest := range deleteRequests {
		// adding an extra minute here to avoid a race between cancellation of request and picking of the request for processing
		if deleteRequest.CreatedAt.Add(p.cfg.DeleteRequestCancelPeriod).Add(time.Minute).After(model.Now()) {
			continue
		}

		pendingDeleteRequestsCount++
		if deleteRequest.CreatedAt.Before(oldestPendingRequestCreatedAt) {
			oldestPendingRequestCreatedAt = deleteRequest.CreatedAt
		}

		p.inProcessRequestIDsMtx.RLock()
		inprocessDeleteRequest, ok := p.inProcessRequests[deleteRequest.UserID]
		p.inProcessRequestIDsMtx.RUnlock()

		if ok {
			p.usersWithPendingRequestsMtx.Lock()
			p.usersWithPendingRequests[deleteRequest.UserID] = struct{}{}
			p.usersWithPendingRequestsMtx.Unlock()

			level.Debug(util.Logger).Log("msg", "skipping delete request processing for now since another request from same user is already in process",
				"inprocess_request_id", inprocessDeleteRequest.RequestID,
				"skipped_request_id", deleteRequest.RequestID, "user_id", deleteRequest.UserID)
			continue
		}

		err = p.deleteStore.UpdateStatus(context.Background(), deleteRequest.UserID, deleteRequest.RequestID, StatusBuildingPlan)
		if err != nil {
			return err
		}

		p.inProcessRequestIDsMtx.Lock()
		p.inProcessRequests[deleteRequest.UserID] = deleteRequest
		p.inProcessRequestIDsMtx.Unlock()

		req := makeDeleteRequestWithLogger(deleteRequest, util.Logger)

		level.Info(req.logger).Log("msg", "building plan for a new delete request")

		err := p.buildDeletePlan(req)
		if err != nil {
			p.metrics.deleteRequestsProcessingFailures.WithLabelValues(deleteRequest.UserID).Inc()

			// We do not want to remove this delete request from inProcessRequests to make sure
			// we do not move multiple deleting requests in deletion process.
			// None of the other delete requests from the user would be considered for processing until then.
			level.Error(req.logger).Log("msg", "error building delete plan", "err", err)
			return err
		}

		level.Info(req.logger).Log("msg", "sending delete request for execution")
		p.executePlansChan <- req
	}

	p.metrics.oldestPendingDeleteRequestAgeSeconds.Set(float64(now.Sub(oldestPendingRequestCreatedAt) / time.Second))
	p.metrics.pendingDeleteRequestsCount.Set(float64(pendingDeleteRequestsCount))

	return nil
}

// buildDeletePlan builds per day delete plan for given delete requests.
// A days plan will include chunk ids and labels of all the chunks which are supposed to be deleted.
// Chunks are grouped together by labels to avoid storing labels repetitively.
// After building delete plans it updates status of delete request to StatusDeleting and sends it for execution
func (p *Purger) buildDeletePlan(req deleteRequestWithLogger) error {
	ctx := context.Background()
	ctx = user.InjectOrgID(ctx, req.UserID)

	perDayTimeRange := splitByDay(req.StartTime, req.EndTime)
	level.Info(req.logger).Log("msg", "building delete plan", "num_plans", len(perDayTimeRange))

	plans := make([][]byte, len(perDayTimeRange))
	includedChunkIDs := map[string]struct{}{}

	for i, planRange := range perDayTimeRange {
		chunksGroups := []ChunksGroup{}

		for _, selector := range req.Selectors {
			matchers, err := parser.ParseMetricSelector(selector)
			if err != nil {
				return err
			}

			chunks, err := p.chunkStore.Get(ctx, req.UserID, planRange.Start, planRange.End, matchers...)
			if err != nil {
				return err
			}

			var cg []ChunksGroup
			cg, includedChunkIDs = groupChunks(chunks, req.StartTime, req.EndTime, includedChunkIDs)

			if len(cg) != 0 {
				chunksGroups = append(chunksGroups, cg...)
			}
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

	err := p.putDeletePlans(ctx, req.UserID, req.RequestID, plans)
	if err != nil {
		return err
	}

	err = p.deleteStore.UpdateStatus(ctx, req.UserID, req.RequestID, StatusDeleting)
	if err != nil {
		return err
	}

	p.metrics.deleteRequestsChunksSelectedTotal.WithLabelValues(req.UserID).Add(float64(len(includedChunkIDs)))

	level.Info(req.logger).Log("msg", "built delete plans", "num_plans", len(perDayTimeRange))

	return nil
}

func (p *Purger) putDeletePlans(ctx context.Context, userID, requestID string, plans [][]byte) error {
	for i, plan := range plans {
		objectKey := buildObjectKeyForPlan(userID, requestID, i)

		err := p.objectClient.PutObject(ctx, objectKey, bytes.NewReader(plan))
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *Purger) getDeletePlan(ctx context.Context, userID, requestID string, planNo int) (*DeletePlan, error) {
	objectKey := buildObjectKeyForPlan(userID, requestID, planNo)

	readCloser, err := p.objectClient.GetObject(ctx, objectKey)
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

func (p *Purger) removeDeletePlan(ctx context.Context, userID, requestID string, planNo int) error {
	objectKey := buildObjectKeyForPlan(userID, requestID, planNo)
	return p.objectClient.DeleteObject(ctx, objectKey)
}

func (p *Purger) getOldestInProcessRequest() *DeleteRequest {
	p.inProcessRequestIDsMtx.RLock()
	defer p.inProcessRequestIDsMtx.RUnlock()

	var oldestRequest *DeleteRequest
	for _, request := range p.inProcessRequests {
		if oldestRequest == nil || request.CreatedAt.Before(oldestRequest.CreatedAt) {
			oldestRequest = &request
		}
	}

	return oldestRequest
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
func groupChunks(chunks []chunk.Chunk, deleteFrom, deleteThrough model.Time, includedChunkIDs map[string]struct{}) ([]ChunksGroup, map[string]struct{}) {
	metricToChunks := make(map[string]ChunksGroup)

	for _, chk := range chunks {
		chunkID := chk.ExternalKey()

		if _, ok := includedChunkIDs[chunkID]; ok {
			continue
		}
		// chunk.Metric are assumed to be sorted which should give same value from String() for same series.
		// If they stop being sorted then in the worst case we would lose the benefit of grouping chunks to avoid storing labels repetitively.
		metricString := chk.Metric.String()
		group, ok := metricToChunks[metricString]
		if !ok {
			group = ChunksGroup{Labels: client.FromLabelsToLabelAdapters(chk.Metric)}
		}

		chunkDetails := ChunkDetails{ID: chunkID}

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
		includedChunkIDs[chunkID] = struct{}{}
		metricToChunks[metricString] = group
	}

	chunksGroups := make([]ChunksGroup, 0, len(metricToChunks))

	for _, group := range metricToChunks {
		chunksGroups = append(chunksGroups, group)
	}

	return chunksGroups, includedChunkIDs
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
