/*
Bloom Gateway package

The bloom gateway is a component that can be run as a standalone microserivce
target and provides capabilities for filtering ChunkRefs based on a given list
of line filter expressions.

			     Querier   Query Frontend
			        |           |
			................................... service boundary
			        |           |
			        +----+------+
			             |
			     indexgateway.Gateway
			             |
			   bloomgateway.BloomQuerier
			             |
			   bloomgateway.GatewayClient
			             |
			  logproto.BloomGatewayClient
			             |
			................................... service boundary
			             |
			      bloomgateway.Gateway
			             |
		       queue.RequestQueue
			             |
		       bloomgateway.Worker
			             |
		     bloomgateway.Processor
			             |
	         bloomshipper.Store
			             |
	         bloomshipper.Client
			             |
			        ObjectClient
			             |
			................................... service boundary
			             |
		         object storage
*/
package bloomgateway

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/queue"
	"github.com/grafana/loki/pkg/storage"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/constants"
)

var errGatewayUnhealthy = errors.New("bloom-gateway is unhealthy in the ring")

const (
	pendingTasksInitialCap = 1024
	metricsSubsystem       = "bloom_gateway"
)

var (
	// responsesPool pooling array of v1.Output [64, 128, 256, ..., 65536]
	responsesPool = queue.NewSlicePool[v1.Output](1<<6, 1<<16, 2)
)

type metrics struct {
	queueDuration    prometheus.Histogram
	inflightRequests prometheus.Summary
	chunkRemovals    *prometheus.CounterVec
}

func newMetrics(registerer prometheus.Registerer, namespace, subsystem string) *metrics {
	return &metrics{
		queueDuration: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "queue_duration_seconds",
			Help:      "Time spent by tasks in queue before getting picked up by a worker.",
			Buckets:   prometheus.DefBuckets,
		}),
		inflightRequests: promauto.With(registerer).NewSummary(prometheus.SummaryOpts{
			Namespace:  namespace,
			Subsystem:  subsystem,
			Name:       "inflight_tasks",
			Help:       "Number of inflight tasks (either queued or processing) sampled at a regular interval. Quantile buckets keep track of inflight tasks over the last 60s.",
			Objectives: map[float64]float64{0.5: 0.05, 0.75: 0.02, 0.8: 0.02, 0.9: 0.01, 0.95: 0.01, 0.99: 0.001},
			MaxAge:     time.Minute,
			AgeBuckets: 6,
		}),
		chunkRemovals: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "chunk_removals_total",
			Help:      "Total amount of removals received from the block querier partitioned by state. The state 'accepted' means that the removals are processed, the state 'dropped' means that the removals were received after the task context was done (e.g. client timeout, etc).",
		}, []string{"state"}),
	}
}

// SyncMap is a map structure which can be synchronized using the RWMutex
type SyncMap[k comparable, v any] struct {
	sync.RWMutex
	Map map[k]v
}

type pendingTasks SyncMap[ulid.ULID, Task]

func (t *pendingTasks) Len() int {
	t.RLock()
	defer t.RUnlock()
	return len(t.Map)
}

func (t *pendingTasks) Add(k ulid.ULID, v Task) {
	t.Lock()
	t.Map[k] = v
	t.Unlock()
}

func (t *pendingTasks) Delete(k ulid.ULID) {
	t.Lock()
	delete(t.Map, k)
	t.Unlock()
}

// makePendingTasks creates a SyncMap that holds pending tasks
func makePendingTasks(n int) *pendingTasks {
	return &pendingTasks{
		RWMutex: sync.RWMutex{},
		Map:     make(map[ulid.ULID]Task, n),
	}
}

type Gateway struct {
	services.Service

	cfg    Config
	logger log.Logger

	metrics       *metrics
	workerMetrics *workerMetrics
	queueMetrics  *queue.Metrics

	queue       *queue.RequestQueue
	activeUsers *util.ActiveUsersCleanupService
	bloomStore  bloomshipper.Store

	pendingTasks *pendingTasks

	serviceMngr    *services.Manager
	serviceWatcher *services.FailureWatcher

	workerConfig workerConfig
}

type fixedQueueLimits struct {
	maxConsumers int
}

func (l *fixedQueueLimits) MaxConsumers(_ string, _ int) int {
	return l.maxConsumers
}

// New returns a new instance of the Bloom Gateway.
func New(cfg Config, schemaCfg config.SchemaConfig, storageCfg storage.Config, overrides Limits, cm storage.ClientMetrics, logger log.Logger, reg prometheus.Registerer) (*Gateway, error) {
	g := &Gateway{
		cfg:          cfg,
		logger:       logger,
		metrics:      newMetrics(reg, constants.Loki, metricsSubsystem),
		pendingTasks: makePendingTasks(pendingTasksInitialCap),
		workerConfig: workerConfig{
			maxItems: 100,
		},
		workerMetrics: newWorkerMetrics(reg, constants.Loki, metricsSubsystem),
		queueMetrics:  queue.NewMetrics(reg, constants.Loki, metricsSubsystem),
	}
	var err error

	g.queue = queue.NewRequestQueue(cfg.MaxOutstandingPerTenant, time.Minute, &fixedQueueLimits{0}, g.queueMetrics)
	g.activeUsers = util.NewActiveUsersCleanupWithDefaultValues(g.queueMetrics.Cleanup)

	var metasCache cache.Cache
	mcCfg := storageCfg.BloomShipperConfig.MetasCache
	if cache.IsCacheConfigured(mcCfg) {
		metasCache, err = cache.New(mcCfg, reg, logger, stats.BloomMetasCache, constants.Loki)
		if err != nil {
			return nil, err
		}
	}

	var blocksCache cache.TypedCache[string, bloomshipper.BlockDirectory]
	bcCfg := storageCfg.BloomShipperConfig.BlocksCache
	if bcCfg.IsEnabled() {
		blocksCache = bloomshipper.NewBlocksCache(bcCfg, reg, logger)
	}

	store, err := bloomshipper.NewBloomStore(schemaCfg.Configs, storageCfg, cm, metasCache, blocksCache, logger)
	if err != nil {
		return nil, err
	}

	// We need to keep a reference to be able to call Stop() on shutdown of the gateway.
	g.bloomStore = store

	if err := g.initServices(); err != nil {
		return nil, err
	}
	g.Service = services.NewBasicService(g.starting, g.running, g.stopping).WithName("bloom-gateway")

	return g, nil
}

func (g *Gateway) initServices() error {
	var err error
	svcs := []services.Service{g.queue, g.activeUsers}
	for i := 0; i < g.cfg.WorkerConcurrency; i++ {
		id := fmt.Sprintf("bloom-query-worker-%d", i)
		w := newWorker(id, g.workerConfig, g.queue, g.bloomStore, g.pendingTasks, g.logger, g.workerMetrics)
		svcs = append(svcs, w)
	}
	g.serviceMngr, err = services.NewManager(svcs...)
	if err != nil {
		return err
	}
	g.serviceWatcher = services.NewFailureWatcher()
	g.serviceWatcher.WatchManager(g.serviceMngr)
	return nil
}

func (g *Gateway) starting(ctx context.Context) error {
	var err error
	defer func() {
		if err == nil || g.serviceMngr == nil {
			return
		}
		if err := services.StopManagerAndAwaitStopped(context.Background(), g.serviceMngr); err != nil {
			level.Error(g.logger).Log("msg", "failed to gracefully stop bloom gateway dependencies", "err", err)
		}
	}()

	if err := services.StartManagerAndAwaitHealthy(ctx, g.serviceMngr); err != nil {
		return errors.Wrap(err, "unable to start bloom gateway subservices")
	}

	return nil
}

func (g *Gateway) running(ctx context.Context) error {
	// We observe inflight tasks frequently and at regular intervals, to have a good
	// approximation of max inflight tasks over percentiles of time. We also do it with
	// a ticker so that we keep tracking it even if we have no new requests but stuck inflight
	// tasks (eg. worker are all exhausted).
	inflightTasksTicker := time.NewTicker(250 * time.Millisecond)
	defer inflightTasksTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-g.serviceWatcher.Chan():
			return errors.Wrap(err, "bloom gateway subservice failed")
		case <-inflightTasksTicker.C:
			inflight := g.pendingTasks.Len()
			g.metrics.inflightRequests.Observe(float64(inflight))
		}
	}
}

func (g *Gateway) stopping(_ error) error {
	g.bloomStore.Stop()
	return services.StopManagerAndAwaitStopped(context.Background(), g.serviceMngr)
}

// FilterChunkRefs implements BloomGatewayServer
func (g *Gateway) FilterChunkRefs(ctx context.Context, req *logproto.FilterChunkRefRequest) (*logproto.FilterChunkRefResponse, error) {
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	logger := log.With(g.logger, "tenant", tenantID)

	// start time == end time --> empty response
	if req.From.Equal(req.Through) {
		return &logproto.FilterChunkRefResponse{
			ChunkRefs: []*logproto.GroupedChunkRefs{},
		}, nil
	}

	// start time > end time --> error response
	if req.Through.Before(req.From) {
		return nil, errors.New("from time must not be after through time")
	}

	// Shortcut if request does not contain filters
	if len(req.Filters) == 0 {
		return &logproto.FilterChunkRefResponse{
			ChunkRefs: req.Refs,
		}, nil
	}

	// Sort ChunkRefs by fingerprint in ascending order
	sort.Slice(req.Refs, func(i, j int) bool {
		return req.Refs[i].Fingerprint < req.Refs[j].Fingerprint
	})

	var numSeries int
	seriesByDay := partitionRequest(req)

	// no tasks --> empty response
	if len(seriesByDay) == 0 {
		return &logproto.FilterChunkRefResponse{
			ChunkRefs: []*logproto.GroupedChunkRefs{},
		}, nil
	}

	tasks := make([]Task, 0, len(seriesByDay))
	for _, seriesWithBounds := range seriesByDay {
		task, err := NewTask(ctx, tenantID, seriesWithBounds, req.Filters)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
		numSeries += len(seriesWithBounds.series)
	}

	g.activeUsers.UpdateUserTimestamp(tenantID, time.Now())

	// Ideally we could use an unbuffered channel here, but since we return the
	// request on the first error, there can be cases where the request context
	// is not done yet and the consumeTask() function wants to send to the
	// tasksCh, but nobody reads from it any more.
	tasksCh := make(chan Task, len(tasks))
	for _, task := range tasks {
		task := task
		level.Info(logger).Log("msg", "enqueue task", "task", task.ID, "table", task.table, "series", len(task.series))
		g.queue.Enqueue(tenantID, []string{}, task, func() {
			// When enqueuing, we also add the task to the pending tasks
			g.pendingTasks.Add(task.ID, task)
		})
		go g.consumeTask(ctx, task, tasksCh)
	}

	responses := responsesPool.Get(numSeries)
	defer responsesPool.Put(responses)
	remaining := len(tasks)

	for remaining > 0 {
		select {
		case <-ctx.Done():
			return nil, errors.Wrap(ctx.Err(), "request failed")
		case task := <-tasksCh:
			level.Info(logger).Log("msg", "task done", "task", task.ID, "err", task.Err())
			if task.Err() != nil {
				return nil, errors.Wrap(task.Err(), "request failed")
			}
			responses = append(responses, task.responses...)
			remaining--
		}
	}

	preFilterSeries := len(req.Refs)

	// TODO(chaudum): Don't wait for all responses before starting to filter chunks.
	filtered := g.processResponses(req, responses)

	postFilterSeries := len(req.Refs)

	level.Info(logger).Log("msg", "return filtered chunk refs", "pre_filter_series", preFilterSeries, "post_filter_series", postFilterSeries, "filtered_chunks", filtered)
	return &logproto.FilterChunkRefResponse{ChunkRefs: req.Refs}, nil
}

// consumeTask receives v1.Output yielded from the block querier on the task's
// result channel and stores them on the task.
// In case the context task is done, it drains the remaining items until the
// task is closed by the worker.
// Once the tasks is closed, it will send the task with the results from the
// block querier to the supplied task channel.
func (g *Gateway) consumeTask(ctx context.Context, task Task, tasksCh chan<- Task) {
	logger := log.With(g.logger, "task", task.ID)

	for res := range task.resCh {
		select {
		case <-ctx.Done():
			level.Debug(logger).Log("msg", "drop partial result", "fp_int", uint64(res.Fp), "fp_hex", res.Fp, "chunks_to_remove", res.Removals.Len())
			g.metrics.chunkRemovals.WithLabelValues("dropped").Add(float64(res.Removals.Len()))
		default:
			level.Debug(logger).Log("msg", "accept partial result", "fp_int", uint64(res.Fp), "fp_hex", res.Fp, "chunks_to_remove", res.Removals.Len())
			task.responses = append(task.responses, res)
			g.metrics.chunkRemovals.WithLabelValues("accepted").Add(float64(res.Removals.Len()))
		}
	}

	select {
	case <-ctx.Done():
		// do nothing
	case <-task.Done():
		// notify request handler about finished task
		tasksCh <- task
	}
}

func (g *Gateway) processResponses(req *logproto.FilterChunkRefRequest, responses []v1.Output) (filtered int) {
	for _, o := range responses {
		if o.Removals.Len() == 0 {
			continue
		}
		filtered += g.removeNotMatchingChunks(req, o)
	}
	return
}

func (g *Gateway) removeNotMatchingChunks(req *logproto.FilterChunkRefRequest, res v1.Output) (filtered int) {

	// binary search index of fingerprint
	idx := sort.Search(len(req.Refs), func(i int) bool {
		return req.Refs[i].Fingerprint >= uint64(res.Fp)
	})

	// fingerprint not found
	if idx >= len(req.Refs) {
		level.Error(g.logger).Log("msg", "index out of range", "idx", idx, "len", len(req.Refs), "fp", uint64(res.Fp))
		return
	}

	// if all chunks of a fingerprint are are removed
	// then remove the whole group from the response
	if len(req.Refs[idx].Refs) == res.Removals.Len() {
		filtered += len(req.Refs[idx].Refs)

		req.Refs[idx] = nil // avoid leaking pointer
		req.Refs = append(req.Refs[:idx], req.Refs[idx+1:]...)
		return
	}

	for i := range res.Removals {
		toRemove := res.Removals[i]
		for j := 0; j < len(req.Refs[idx].Refs); j++ {
			if toRemove.Checksum == req.Refs[idx].Refs[j].Checksum {
				filtered += 1

				req.Refs[idx].Refs[j] = nil // avoid leaking pointer
				req.Refs[idx].Refs = append(req.Refs[idx].Refs[:j], req.Refs[idx].Refs[j+1:]...)
				j-- // since we removed the current item at index, we have to redo the same index
			}
		}
	}
	return
}
