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
			       bloomshipper.Store
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
	"github.com/prometheus/common/model"

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
var errInvalidTenant = errors.New("invalid tenant in chunk refs")

// TODO(chaudum): Make these configurable
const (
	numWorkers             = 4
	maxTasksPerTenant      = 1024
	pendingTasksInitialCap = 1024
)

type metrics struct {
	queueDuration    prometheus.Histogram
	inflightRequests prometheus.Summary
}

func newMetrics(subsystem string, registerer prometheus.Registerer) *metrics {
	return &metrics{
		queueDuration: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Subsystem: subsystem,
			Name:      "queue_duration_seconds",
			Help:      "Time spent by tasks in queue before getting picked up by a worker.",
			Buckets:   prometheus.DefBuckets,
		}),
		inflightRequests: promauto.With(registerer).NewSummary(prometheus.SummaryOpts{
			Namespace:  constants.Loki,
			Subsystem:  subsystem,
			Name:       "inflight_tasks",
			Help:       "Number of inflight tasks (either queued or processing) sampled at a regular interval. Quantile buckets keep track of inflight tasks over the last 60s.",
			Objectives: map[float64]float64{0.5: 0.05, 0.75: 0.02, 0.8: 0.02, 0.9: 0.01, 0.95: 0.01, 0.99: 0.001},
			MaxAge:     time.Minute,
			AgeBuckets: 6,
		}),
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

	sharding ShardingStrategy

	pendingTasks *pendingTasks

	serviceMngr    *services.Manager
	serviceWatcher *services.FailureWatcher

	workerConfig workerConfig
}

// New returns a new instance of the Bloom Gateway.
func New(cfg Config, schemaCfg config.SchemaConfig, storageCfg storage.Config, overrides Limits, shardingStrategy ShardingStrategy, cm storage.ClientMetrics, logger log.Logger, reg prometheus.Registerer) (*Gateway, error) {
	metricsSubsystem := "bloom_gateway"

	g := &Gateway{
		cfg:          cfg,
		logger:       logger,
		metrics:      newMetrics(metricsSubsystem, reg),
		sharding:     shardingStrategy,
		pendingTasks: makePendingTasks(pendingTasksInitialCap),
		workerConfig: workerConfig{
			maxWaitTime: 200 * time.Millisecond,
			maxItems:    100,
		},
		workerMetrics: newWorkerMetrics(metricsSubsystem, reg),
		queueMetrics:  queue.NewMetrics(reg, constants.Loki, metricsSubsystem),
	}

	g.queue = queue.NewRequestQueue(maxTasksPerTenant, time.Minute, g.queueMetrics)
	g.activeUsers = util.NewActiveUsersCleanupWithDefaultValues(g.queueMetrics.Cleanup)

	client, err := bloomshipper.NewBloomClient(schemaCfg.Configs, storageCfg, cm)
	if err != nil {
		return nil, err
	}

	bloomShipper, err := bloomshipper.NewShipper(client, storageCfg.BloomShipperConfig, overrides, logger, reg)
	if err != nil {
		return nil, err
	}

	bloomStore, err := bloomshipper.NewBloomStore(bloomShipper)
	if err != nil {
		return nil, err
	}

	// We need to keep a reference to be able to call Stop() on shutdown of the gateway.
	g.bloomStore = bloomStore

	if err := g.initServices(); err != nil {
		return nil, err
	}
	g.Service = services.NewBasicService(g.starting, g.running, g.stopping).WithName("bloom-gateway")

	return g, nil
}

func (g *Gateway) initServices() error {
	var err error
	svcs := []services.Service{g.queue, g.activeUsers}
	for i := 0; i < numWorkers; i++ {
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

	// Shortcut if request does not contain filters
	if len(req.Filters) == 0 {
		return &logproto.FilterChunkRefResponse{
			ChunkRefs: req.Refs,
		}, nil
	}

	for _, ref := range req.Refs {
		if ref.Tenant != tenantID {
			return nil, errors.Wrapf(errInvalidTenant, "expected chunk refs from tenant %s, got tenant %s", tenantID, ref.Tenant)
		}
		// Sort ShortRefs by From time in ascending order
		sort.Slice(ref.Refs, func(i, j int) bool {
			return ref.Refs[i].From.Before(ref.Refs[j].From)
		})
	}

	// Sort ChunkRefs by fingerprint in ascending order
	sort.Slice(req.Refs, func(i, j int) bool {
		return req.Refs[i].Fingerprint < req.Refs[j].Fingerprint
	})

	task, resCh, errCh, err := NewTask(tenantID, req)
	if err != nil {
		return nil, err
	}

	g.activeUsers.UpdateUserTimestamp(tenantID, time.Now())
	level.Info(g.logger).Log("msg", "enqueue task", "task", task.ID)
	g.queue.Enqueue(tenantID, []string{}, task, 100, func() {
		// When enqueuing, we also add the task to the pending tasks
		g.pendingTasks.Add(task.ID, task)
	})

	response := make([]*logproto.GroupedChunkRefs, 0, len(req.Refs))
	for {
		select {
		case <-ctx.Done():
			return nil, errors.Wrap(ctx.Err(), "waiting for results")
		case err := <-errCh:
			return nil, errors.Wrap(err, "waiting for results")
		case res := <-resCh:
			// log line is helpful for debugging tests
			// level.Debug(g.logger).Log("msg", "got partial result", "task", task.ID, "tenant", tenantID, "fp", res.Fp, "refs", res.Chks.Len())
			response = append(response, &logproto.GroupedChunkRefs{
				Tenant:      tenantID,
				Fingerprint: uint64(res.Fp),
				Refs:        convertToShortRefs(res.Chks),
			})
			// wait for all parts of the full response
			if len(response) == len(req.Refs) {
				return &logproto.FilterChunkRefResponse{ChunkRefs: response}, nil
			}
		}
	}
}

type workerConfig struct {
	maxWaitTime time.Duration
	maxItems    int
}

type workerMetrics struct {
	dequeuedTasks      *prometheus.CounterVec
	dequeueErrors      *prometheus.CounterVec
	dequeueWaitTime    *prometheus.SummaryVec
	storeAccessLatency *prometheus.HistogramVec
}

func newWorkerMetrics(subsystem string, registerer prometheus.Registerer) *workerMetrics {
	labels := []string{"worker"}
	return &workerMetrics{
		dequeuedTasks: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki",
			Subsystem: subsystem,
			Name:      "dequeued_tasks_total",
			Help:      "Total amount of tasks that the worker dequeued from the bloom query queue",
		}, labels),
		dequeueErrors: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki",
			Subsystem: subsystem,
			Name:      "dequeue_errors_total",
			Help:      "Total amount of failed dequeue operations",
		}, labels),
		dequeueWaitTime: promauto.With(registerer).NewSummaryVec(prometheus.SummaryOpts{
			Namespace: "loki",
			Subsystem: subsystem,
			Name:      "dequeue_wait_time",
			Help:      "Time spent waiting for dequeuing tasks from queue",
		}, labels),
		storeAccessLatency: promauto.With(registerer).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki",
			Subsystem: subsystem,
			Name:      "store_latency",
			Help:      "Latency in seconds of accessing the bloom store component",
		}, append(labels, "operation")),
	}
}

// worker is a datastructure that consumes tasks from the request queue,
// processes them and returns the result/error back to the response channels of
// the tasks.
// It is responsible for multiplexing tasks so they can be processes in a more
// efficient way.
type worker struct {
	services.Service

	id      string
	cfg     workerConfig
	queue   *queue.RequestQueue
	store   bloomshipper.Store
	tasks   *pendingTasks
	logger  log.Logger
	metrics *workerMetrics
	rp      RequestPool
}

func newWorker(id string, cfg workerConfig, queue *queue.RequestQueue, store bloomshipper.Store, tasks *pendingTasks, logger log.Logger, metrics *workerMetrics) *worker {
	w := &worker{
		id:      id,
		cfg:     cfg,
		queue:   queue,
		store:   store,
		tasks:   tasks,
		logger:  log.With(logger, "worker", id),
		metrics: metrics,
	}
	w.Service = services.NewBasicService(w.starting, w.running, w.stopping).WithName(id)
	w.rp = RequestPool{
		Pool: sync.Pool{
			New: func() any {
				return make([]v1.Request, 0, 1024)
			},
		},
	}
	return w
}

func (w *worker) starting(_ context.Context) error {
	level.Debug(w.logger).Log("msg", "starting worker")
	w.queue.RegisterConsumerConnection(w.id)
	return nil
}

func (w *worker) running(ctx context.Context) error {
	idx := queue.StartIndexWithLocalQueue

	for ctx.Err() == nil {
		taskCtx := context.Background()
		dequeueStart := time.Now()
		items, newIdx, err := w.queue.DequeueMany(taskCtx, idx, w.id, w.cfg.maxItems, w.cfg.maxWaitTime)
		w.metrics.dequeueWaitTime.WithLabelValues(w.id).Observe(time.Since(dequeueStart).Seconds())
		if err != nil {
			// We only return an error if the queue is stopped and dequeuing did not yield any items
			if err == queue.ErrStopped && len(items) == 0 {
				return err
			}
			w.metrics.dequeueErrors.WithLabelValues(w.id).Inc()
			level.Error(w.logger).Log("msg", "failed to dequeue tasks", "err", err, "items", len(items))
		}
		idx = newIdx

		if len(items) == 0 {
			continue
		}
		w.metrics.dequeuedTasks.WithLabelValues(w.id).Add(float64(len(items)))

		tasksPerDay := make(map[time.Time][]Task)

		for _, item := range items {
			task, ok := item.(Task)
			if !ok {
				// This really should never happen, because only the bloom gateway itself can enqueue tasks.
				return errors.Errorf("failed to cast dequeued item to Task: %v", item)
			}
			level.Debug(w.logger).Log("msg", "dequeued task", "task", task.ID)
			w.tasks.Delete(task.ID)

			fromDay, throughDay := task.Bounds()

			if fromDay.Equal(throughDay) {
				tasksPerDay[fromDay] = append(tasksPerDay[fromDay], task)
			} else {
				// split task into separate tasks per day
				for i := fromDay; i.Before(throughDay); i = i.Add(24 * time.Hour) {
					r := filterRequestForDay(task.Request, i)
					t := task.WithRequest(r)
					tasksPerDay[i] = append(tasksPerDay[i], t)
				}
			}
		}

		for day, tasks := range tasksPerDay {
			logger := log.With(w.logger, "day", day)
			level.Debug(logger).Log("msg", "process tasks", "tasks", len(tasks))

			it := newTaskMergeIterator(tasks...)

			fingerprints := make([]uint64, 0, 1024)
			for it.Next() {
				// fingerprints are already sorted. we can skip duplicates by checking
				// if the next is greater than the previous
				fp := uint64(it.At().Fp)
				if len(fingerprints) > 0 && fp <= fingerprints[len(fingerprints)-1] {
					continue
				}
				fingerprints = append(fingerprints, fp)
			}

			it.Reset()

			storeFetchStart := time.Now()
			// GetBlockQueriers() waits until all blocks are downloaded and available for querying.
			// TODO(chaudum): Add API that allows to process blocks as soon as they become available.
			// This will require to change the taskMergeIterator to a slice of requests so we can seek
			// to the appropriate fingerprint range within the slice that matches the block's fingerprint range.
			bqs, err := w.store.GetBlockQueriers(taskCtx, tasks[0].Tenant, day, day.Add(24*time.Hour), fingerprints)
			w.metrics.storeAccessLatency.WithLabelValues(w.id, "GetBlockQueriers").Observe(time.Since(storeFetchStart).Seconds())
			if err != nil {
				for _, t := range tasks {
					t.ErrCh <- err
				}
				// continue with tasks of next day
				continue
			}

			// No blocks found.
			// Since there are no blocks for the given tasks, we need to return the
			// unfiltered list of chunk refs.
			if len(bqs) == 0 {
				level.Warn(logger).Log("msg", "no blocks found")
				for _, t := range tasks {
					for _, ref := range t.Request.Refs {
						t.ResCh <- v1.Output{
							Fp:   model.Fingerprint(ref.Fingerprint),
							Chks: convertToChunkRefs(ref.Refs),
						}
					}
				}
				// continue with tasks of next day
				continue
			}

			hasNext := it.Next()
			requests := w.rp.Get()
			for _, bq := range bqs {
				requests = requests[:0]
				for hasNext && it.At().Fp <= bq.MaxFp {
					requests = append(requests, it.At().Request)
					hasNext = it.Next()
				}
				// no fingerprints in the fingerprint range of the current block
				if len(requests) == 0 {
					continue
				}
				fq := bq.Fuse([]v1.PeekingIterator[v1.Request]{NewIterWithIndex(0, requests)})
				err := fq.Run()
				if err != nil {
					for _, t := range tasks {
						t.ErrCh <- errors.Wrap(err, "failed to run chunk check")
					}
					// return slice back to pool
					w.rp.Put(requests)
					continue
				}
			}
			// return slice back to pool
			w.rp.Put(requests)
		}
	}
	return ctx.Err()
}

func (w *worker) stopping(err error) error {
	level.Debug(w.logger).Log("msg", "stopping worker", "err", err)
	w.queue.UnregisterConsumerConnection(w.id)
	return nil
}
