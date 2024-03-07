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
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/atomic"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/queue"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/constants"
)

var errGatewayUnhealthy = errors.New("bloom-gateway is unhealthy in the ring")

const (
	metricsSubsystem        = "bloom_gateway"
	querierMetricsSubsystem = "bloom_gateway_querier"
)

var (
	// responsesPool pooling array of v1.Output [64, 128, 256, ..., 65536]
	responsesPool = queue.NewSlicePool[v1.Output](1<<6, 1<<16, 2)
)

// SyncMap is a map structure which can be synchronized using the RWMutex
type SyncMap[k comparable, v any] struct {
	sync.RWMutex
	Map map[k]v
}

type Gateway struct {
	services.Service

	cfg     Config
	logger  log.Logger
	metrics *metrics

	queue       *queue.RequestQueue
	activeUsers *util.ActiveUsersCleanupService
	bloomStore  bloomshipper.Store

	pendingTasks *atomic.Int64

	serviceMngr    *services.Manager
	serviceWatcher *services.FailureWatcher

	workerConfig workerConfig
}

// fixedQueueLimits is a queue.Limits implementation that returns a fixed value for MaxConsumers.
// Notably this lets us run with "disabled" max consumers (0) for the bloom gateway meaning it will
// distribute any request to any receiver.
type fixedQueueLimits struct {
	maxConsumers int
}

func (l *fixedQueueLimits) MaxConsumers(_ string, _ int) int {
	return l.maxConsumers
}

// New returns a new instance of the Bloom Gateway.
func New(cfg Config, store bloomshipper.Store, logger log.Logger, reg prometheus.Registerer) (*Gateway, error) {
	g := &Gateway{
		cfg:     cfg,
		logger:  logger,
		metrics: newMetrics(reg, constants.Loki, metricsSubsystem),
		workerConfig: workerConfig{
			maxItems: 100,
		},
		pendingTasks: &atomic.Int64{},

		bloomStore: store,
	}

	queueMetrics := queue.NewMetrics(reg, constants.Loki, metricsSubsystem)
	g.queue = queue.NewRequestQueue(cfg.MaxOutstandingPerTenant, time.Minute, &fixedQueueLimits{0}, queueMetrics)
	g.activeUsers = util.NewActiveUsersCleanupWithDefaultValues(queueMetrics.Cleanup)

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
		w := newWorker(id, g.workerConfig, g.queue, g.bloomStore, g.pendingTasks, g.logger, g.metrics.workerMetrics)
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
			inflight := g.pendingTasks.Load()
			g.metrics.inflightRequests.Observe(float64(inflight))
		}
	}
}

func (g *Gateway) stopping(_ error) error {
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

	filters := syntax.ExtractLineFilters(req.Plan.AST)

	// Shortcut if request does not contain filters
	if len(filters) == 0 {
		return &logproto.FilterChunkRefResponse{
			ChunkRefs: req.Refs,
		}, nil
	}

	var numSeries int
	seriesByDay := partitionRequest(req)

	// no tasks --> empty response
	if len(seriesByDay) == 0 {
		return &logproto.FilterChunkRefResponse{
			ChunkRefs: []*logproto.GroupedChunkRefs{},
		}, nil
	}

	tasks := make([]Task, 0, len(seriesByDay))
	for _, seriesForDay := range seriesByDay {
		task, err := NewTask(ctx, tenantID, seriesForDay, filters)
		if err != nil {
			return nil, err
		}
		level.Debug(g.logger).Log(
			"msg", "created task for day",
			"task", task.ID,
			"day", seriesForDay.day,
			"interval", seriesForDay.interval.String(),
			"nSeries", len(seriesForDay.series),
			"filters", JoinFunc(filters, ";", func(e syntax.LineFilterExpr) string { return e.String() }),
		)
		tasks = append(tasks, task)
		numSeries += len(seriesForDay.series)
	}

	g.activeUsers.UpdateUserTimestamp(tenantID, time.Now())

	// Ideally we could use an unbuffered channel here, but since we return the
	// request on the first error, there can be cases where the request context
	// is not done yet and the consumeTask() function wants to send to the
	// tasksCh, but nobody reads from it any more.
	tasksCh := make(chan Task, len(tasks))
	for _, task := range tasks {
		task := task
		task.enqueueTime = time.Now()
		level.Info(logger).Log("msg", "enqueue task", "task", task.ID, "table", task.table, "series", len(task.series))
		g.queue.Enqueue(tenantID, nil, task, func() {
			// When enqueuing, we also add the task to the pending tasks
			_ = g.pendingTasks.Inc()
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
	// TODO(owen-d): there's a bug here because the same fingerprint and chunks can exist over multiple day buckets.
	// If all requested chunks are in both days, the first day could technically remove _all_ chunks from consideration.
	// The sort.Search for the _next_ chunk would return an index where fingerprint is greater than the target fingerprint.
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

	// TODO(owen-d): there's a bug here because the same fingerprint and chunks can exist over multiple day buckets.
	// A later day bucket could happen to request removals with len=remaining, but whose chunk references were
	// partially removed in an earlier round. Just checking the length here could cause us to discard chunks
	// that shouldn't be.
	if len(req.Refs[idx].Refs) == res.Removals.Len() {
		filtered += len(req.Refs[idx].Refs)

		req.Refs[idx] = nil // avoid leaking pointer
		// TODO(owen-d): this is O(n^2);
		// use more specialized data structure that doesn't reslice
		req.Refs = append(req.Refs[:idx], req.Refs[idx+1:]...)
		return
	}

	for i := range res.Removals {
		toRemove := res.Removals[i]
		for j := 0; j < len(req.Refs[idx].Refs); j++ {
			if req.Refs[idx].Refs[j] == nil {
				continue
			}

			// TODO(owen-d): These should check start/end/checksum, not just checksum.
			if logproto.ShortRef(toRemove) == *req.Refs[idx].Refs[j] {
				filtered += 1

				// TODO(owen-d): usually not a problem (n is small), but I've seen some series have
				// many thousands of chunks per day, so would be good to not reslice.
				// See `labels.NewBuilder()` for an example
				req.Refs[idx].Refs[j] = nil // avoid leaking pointer
				req.Refs[idx].Refs = append(req.Refs[idx].Refs[:j], req.Refs[idx].Refs[j+1:]...)
				j-- // since we removed the current item at index, we have to redo the same index
			}
		}
	}
	return
}
