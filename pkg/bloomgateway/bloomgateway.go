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
			      bloomshipper.Shipper
			             |
	     bloomshipper.BloomFileClient
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
	"github.com/grafana/loki/pkg/queue"
	"github.com/grafana/loki/pkg/storage"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
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
	queueDuration       prometheus.Histogram
	inflightRequests    prometheus.Summary
	chunkRefsUnfiltered prometheus.Counter
	chunkRefsFiltered   prometheus.Counter
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
		chunkRefsUnfiltered: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "chunkrefs_pre_filtering",
			Help:      "Total amount of chunk refs pre filtering. Does not count chunk refs in failed requests.",
		}),
		chunkRefsFiltered: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "chunkrefs_post_filtering",
			Help:      "Total amount of chunk refs post filtering.",
		}),
	}
}

func (m *metrics) addUnfilteredCount(n int) {
	m.chunkRefsUnfiltered.Add(float64(n))
}

func (m *metrics) addFilteredCount(n int) {
	m.chunkRefsFiltered.Add(float64(n))
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

	queue        *queue.RequestQueue
	activeUsers  *util.ActiveUsersCleanupService
	bloomShipper bloomshipper.Interface

	sharding ShardingStrategy

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
func New(cfg Config, schemaCfg config.SchemaConfig, storageCfg storage.Config, overrides Limits, shardingStrategy ShardingStrategy, cm storage.ClientMetrics, logger log.Logger, reg prometheus.Registerer) (*Gateway, error) {
	g := &Gateway{
		cfg:          cfg,
		logger:       logger,
		metrics:      newMetrics(reg, constants.Loki, metricsSubsystem),
		sharding:     shardingStrategy,
		pendingTasks: makePendingTasks(pendingTasksInitialCap),
		workerConfig: workerConfig{
			maxWaitTime: 200 * time.Millisecond,
			maxItems:    100,
		},
		workerMetrics: newWorkerMetrics(reg, constants.Loki, metricsSubsystem),
		queueMetrics:  queue.NewMetrics(reg, constants.Loki, metricsSubsystem),
	}

	g.queue = queue.NewRequestQueue(cfg.MaxOutstandingPerTenant, time.Minute, &fixedQueueLimits{100}, g.queueMetrics)
	g.activeUsers = util.NewActiveUsersCleanupWithDefaultValues(g.queueMetrics.Cleanup)

	client, err := bloomshipper.NewBloomClient(schemaCfg.Configs, storageCfg, cm)
	if err != nil {
		return nil, err
	}

	bloomShipper, err := bloomshipper.NewShipper(client, storageCfg.BloomShipperConfig, overrides, logger, reg)
	if err != nil {
		return nil, err
	}

	// We need to keep a reference to be able to call Stop() on shutdown of the gateway.
	g.bloomShipper = bloomShipper

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
		w := newWorker(id, g.workerConfig, g.queue, g.bloomShipper, g.pendingTasks, g.logger, g.workerMetrics)
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
	g.bloomShipper.Stop()
	return services.StopManagerAndAwaitStopped(context.Background(), g.serviceMngr)
}

// FilterChunkRefs implements BloomGatewayServer
func (g *Gateway) FilterChunkRefs(ctx context.Context, req *logproto.FilterChunkRefRequest) (*logproto.FilterChunkRefResponse, error) {
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

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

	numChunksUnfiltered := len(req.Refs)

	// Shortcut if request does not contain filters
	if len(req.Filters) == 0 {
		g.metrics.addUnfilteredCount(numChunksUnfiltered)
		g.metrics.addFilteredCount(len(req.Refs))
		return &logproto.FilterChunkRefResponse{
			ChunkRefs: req.Refs,
		}, nil
	}

	// Sort ChunkRefs by fingerprint in ascending order
	sort.Slice(req.Refs, func(i, j int) bool {
		return req.Refs[i].Fingerprint < req.Refs[j].Fingerprint
	})

	var expectedResponses int
	seriesWithBloomsPerDay := partitionRequest(req)

	// no tasks --> empty response
	if len(seriesWithBloomsPerDay) == 0 {
		return &logproto.FilterChunkRefResponse{
			ChunkRefs: []*logproto.GroupedChunkRefs{},
		}, nil
	}

	tasks := make([]Task, 0, len(seriesWithBloomsPerDay))
	for _, seriesWithBounds := range seriesWithBloomsPerDay {
		task, err := NewTask(tenantID, seriesWithBounds, req.Filters)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
		expectedResponses += len(seriesWithBounds.series)
	}

	g.activeUsers.UpdateUserTimestamp(tenantID, time.Now())

	errCh := make(chan error, 1)
	resCh := make(chan v1.Output, 1)

	for _, task := range tasks {
		level.Info(g.logger).Log("msg", "enqueue task", "task", task.ID, "day", task.day, "series", len(task.series))
		g.queue.Enqueue(tenantID, []string{}, task, func() {
			// When enqueuing, we also add the task to the pending tasks
			g.pendingTasks.Add(task.ID, task)
		})

		// Forward responses or error to the main channels
		// TODO(chaudum): Refactor to make tasks cancelable
		go func(t Task) {
			for {
				select {
				case <-ctx.Done():
					return
				case err := <-t.ErrCh:
					if ctx.Err() != nil {
						level.Warn(g.logger).Log("msg", "received err from channel, but context is already done", "err", ctx.Err())
						return
					}
					errCh <- err
				case res := <-t.ResCh:
					level.Debug(g.logger).Log("msg", "got partial result", "task", t.ID, "tenant", tenantID, "fp_int", uint64(res.Fp), "fp_hex", res.Fp, "chunks_to_remove", res.Removals.Len())
					if ctx.Err() != nil {
						level.Warn(g.logger).Log("msg", "received res from channel, but context is already done", "err", ctx.Err())
						return
					}
					resCh <- res
				}
			}
		}(task)
	}

	responses := responsesPool.Get(expectedResponses)
	defer responsesPool.Put(responses)

outer:
	for {
		select {
		case <-ctx.Done():
			return nil, errors.Wrap(ctx.Err(), "waiting for results")
		case err := <-errCh:
			return nil, errors.Wrap(err, "waiting for results")
		case res := <-resCh:
			responses = append(responses, res)
			// log line is helpful for debugging tests
			level.Debug(g.logger).Log("msg", "got partial result", "progress", fmt.Sprintf("%d/%d", len(responses), expectedResponses))
			// wait for all parts of the full response
			if len(responses) == expectedResponses {
				break outer
			}
		}
	}

	for _, o := range responses {
		if o.Removals.Len() == 0 {
			continue
		}
		removeNotMatchingChunks(req, o, g.logger)
	}

	g.metrics.addUnfilteredCount(numChunksUnfiltered)
	g.metrics.addFilteredCount(len(req.Refs))

	level.Debug(g.logger).Log("msg", "return filtered chunk refs", "unfiltered", numChunksUnfiltered, "filtered", len(req.Refs))
	return &logproto.FilterChunkRefResponse{ChunkRefs: req.Refs}, nil
}

func removeNotMatchingChunks(req *logproto.FilterChunkRefRequest, res v1.Output, logger log.Logger) {
	// binary search index of fingerprint
	idx := sort.Search(len(req.Refs), func(i int) bool {
		return req.Refs[i].Fingerprint >= uint64(res.Fp)
	})

	// fingerprint not found
	if idx >= len(req.Refs) {
		level.Error(logger).Log("msg", "index out of range", "idx", idx, "len", len(req.Refs), "fp", uint64(res.Fp))
		return
	}

	// if all chunks of a fingerprint are are removed
	// then remove the whole group from the response
	if len(req.Refs[idx].Refs) == res.Removals.Len() {
		req.Refs[idx] = nil // avoid leaking pointer
		req.Refs = append(req.Refs[:idx], req.Refs[idx+1:]...)
		return
	}

	for i := range res.Removals {
		toRemove := res.Removals[i]
		for j := 0; j < len(req.Refs[idx].Refs); j++ {
			if toRemove.Checksum == req.Refs[idx].Refs[j].Checksum {
				req.Refs[idx].Refs[j] = nil // avoid leaking pointer
				req.Refs[idx].Refs = append(req.Refs[idx].Refs[:j], req.Refs[idx].Refs[j+1:]...)
				j-- // since we removed the current item at index, we have to redo the same index
			}
		}
	}
}
