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

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/queue"
	"github.com/grafana/loki/pkg/storage"
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

// Task is the data structure that is enqueued to the internal queue and queued by query workers
type Task struct {
	// ID is a lexcographically sortable unique identifier of the task
	ID ulid.ULID
	// Tenant is the tenant ID
	Tenant string
	// Request is the original request
	Request *logproto.FilterChunkRefRequest
	// ErrCh is a send-only channel to write an error to
	ErrCh chan<- error
	// ResCh is a send-only channel to write partial responses to
	ResCh chan<- *logproto.GroupedChunkRefs
}

// newTask returns a new Task that can be enqueued to the task queue.
// As additional arguments, it returns a result and an error channel, as well
// as an error if the instantiation fails.
func newTask(tenantID string, req *logproto.FilterChunkRefRequest) (Task, chan *logproto.GroupedChunkRefs, chan error, error) {
	key, err := ulid.New(ulid.Now(), nil)
	if err != nil {
		return Task{}, nil, nil, err
	}
	errCh := make(chan error, 1)
	resCh := make(chan *logproto.GroupedChunkRefs, 1)
	task := Task{
		ID:      key,
		Tenant:  tenantID,
		Request: req,
		ErrCh:   errCh,
		ResCh:   resCh,
	}
	return task, resCh, errCh, nil
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

	cfg     Config
	logger  log.Logger
	metrics *metrics

	queue        *queue.RequestQueue
	queueMetrics *queue.Metrics
	activeUsers  *util.ActiveUsersCleanupService
	bloomStore   bloomshipper.Store

	sharding ShardingStrategy

	pendingTasks *pendingTasks

	serviceMngr    *services.Manager
	serviceWatcher *services.FailureWatcher
}

// New returns a new instance of the Bloom Gateway.
func New(cfg Config, schemaCfg config.SchemaConfig, storageCfg storage.Config, overrides Limits, shardingStrategy ShardingStrategy, cm storage.ClientMetrics, logger log.Logger, reg prometheus.Registerer) (*Gateway, error) {
	g := &Gateway{
		cfg:          cfg,
		logger:       logger,
		metrics:      newMetrics("bloom_gateway", reg),
		sharding:     shardingStrategy,
		pendingTasks: makePendingTasks(pendingTasksInitialCap),
	}

	g.queueMetrics = queue.NewMetrics(reg, constants.Loki, "bloom_gateway")
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

	svcs := []services.Service{g.queue, g.activeUsers}
	for i := 0; i < numWorkers; i++ {
		w := newWorker(i, g.queue, g.bloomStore, g.pendingTasks, logger)
		svcs = append(svcs, w)
	}
	g.serviceMngr, err = services.NewManager(svcs...)
	if err != nil {
		return nil, err
	}
	g.serviceWatcher = services.NewFailureWatcher()
	g.serviceWatcher.WatchManager(g.serviceMngr)

	g.Service = services.NewBasicService(g.starting, g.running, g.stopping).WithName("bloom-gateway")

	return g, nil
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

	for _, ref := range req.Refs {
		if ref.Tenant != tenantID {
			return nil, errors.Wrapf(errInvalidTenant, "expected chunk refs from tenant %s, got tenant %s", tenantID, ref.Tenant)
		}
	}

	// Sort ChunkRefs by fingerprint in ascending order
	sort.Slice(req.Refs, func(i, j int) bool {
		return req.Refs[i].Fingerprint < req.Refs[j].Fingerprint
	})

	task, resCh, errCh, err := newTask(tenantID, req)
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
			return nil, ctx.Err()
		case err := <-errCh:
			return nil, err
		case res := <-resCh:
			level.Info(g.logger).Log("msg", "got result", "task", task.ID, "tenant", tenantID, "res", res)
			// wait for all parts of the full response
			response = append(response, res)
			if len(response) == len(req.Refs) {
				return &logproto.FilterChunkRefResponse{ChunkRefs: response}, nil
			}
		}
	}
}

// Worker is a datastructure that consumes tasks from the request queue,
// processes them and returns the result/error back to the response channels of
// the tasks.
// It is responsible for multiplexing tasks so they can be processes in a more
// efficient way.
type worker struct {
	services.Service

	ID     string
	queue  *queue.RequestQueue
	store  bloomshipper.Store
	tasks  *pendingTasks
	logger log.Logger
}

func newWorker(i int, queue *queue.RequestQueue, store bloomshipper.Store, tasks *pendingTasks, logger log.Logger) *worker {
	id := fmt.Sprintf("bloom-query-worker-%d", i)
	w := &worker{
		ID:     id,
		queue:  queue,
		store:  store,
		tasks:  tasks,
		logger: log.With(logger, "worker", id),
	}
	w.Service = services.NewBasicService(w.starting, w.running, w.stopping)
	return w
}

func (w *worker) starting(_ context.Context) error {
	level.Debug(w.logger).Log("msg", "starting worker")
	w.queue.RegisterConsumerConnection(w.ID)
	return nil
}

func (w *worker) running(_ context.Context) error {
	idx := queue.StartIndexWithLocalQueue

	for {
		ctx := context.Background()
		item, newIdx, err := w.queue.Dequeue(ctx, idx, w.ID)
		if err != nil {
			if err != queue.ErrStopped {
				level.Error(w.logger).Log("msg", "failed to dequeue task", "err", err)
				continue
			}
			return err
		}
		task, ok := item.(Task)
		if !ok {
			level.Error(w.logger).Log("msg", "failed to cast dequeued item to Task", "item", item)
			continue
		}
		level.Debug(w.logger).Log("msg", "dequeued task", "task", task.ID)

		w.tasks.Delete(task.ID)

		idx = newIdx

		r := task.Request
		if len(r.Filters) > 0 {
			r.Refs, err = w.store.FilterChunkRefs(ctx, task.Tenant, r.From.Time(), r.Through.Time(), r.Refs, r.Filters...)
		}
		if err != nil {
			task.ErrCh <- err
		} else {
			for _, ref := range r.Refs {
				task.ResCh <- ref
			}
		}
	}
}

func (w *worker) stopping(err error) error {
	level.Debug(w.logger).Log("msg", "stopping worker", "err", err)
	w.queue.UnregisterConsumerConnection(w.ID)
	return nil
}
