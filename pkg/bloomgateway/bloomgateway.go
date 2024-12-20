/*
The bloom gateway is a component that can be run as a standalone microserivce
target and provides capabilities for filtering ChunkRefs based on a given list
of line filter expressions.
*/
package bloomgateway

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/atomic"

	iter "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/queue"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	utillog "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
)

const (
	metricsSubsystem        = "bloom_gateway"
	querierMetricsSubsystem = "bloom_gateway_querier"
)

var (
	// responsesPool pooling array of v1.Output [64, 128, 256, ..., 65536]
	responsesPool = queue.NewSlicePool[v1.Output](1<<6, 1<<16, 2)
)

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
	utillog.WarnExperimentalUse("Bloom Gateway", logger)
	g := &Gateway{
		cfg:     cfg,
		logger:  logger,
		metrics: newMetrics(reg, constants.Loki, metricsSubsystem),
		workerConfig: workerConfig{
			maxItems:         cfg.NumMultiplexItems,
			queryConcurrency: cfg.BlockQueryConcurrency,
			async:            cfg.FetchBlocksAsync,
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

func (g *Gateway) PrefetchBloomBlocks(_ context.Context, req *logproto.PrefetchBloomBlocksRequest) (*logproto.PrefetchBloomBlocksResponse, error) {
	refs, err := decodeBlockKeys(req.Blocks)
	if err != nil {
		return nil, err
	}

	bqs, err := g.bloomStore.FetchBlocks(
		// We don't use the ctx passed to the handler since its canceled when the handler returns
		context.Background(),
		refs,
		bloomshipper.WithFetchAsync(true),
		bloomshipper.WithIgnoreNotFound(true),
		bloomshipper.WithCacheGetOptions(
			bloomshipper.WithSkipHitMissMetrics(true),
		),
	)
	if err != nil {
		g.metrics.prefetchedBlocks.WithLabelValues(typeError).Add(float64(len(refs)))
		return nil, err
	}

	for _, bq := range bqs {
		if bq == nil {
			// This is the expected case: the blocks is not yet downloaded and the block querier is nil
			continue
		}

		// Close any block querier that were already downloaded
		if err := bq.Close(); err != nil {
			level.Warn(g.logger).Log("msg", "failed to close block querier", "err", err)
		}
	}

	g.metrics.prefetchedBlocks.WithLabelValues(typeSuccess).Add(float64(len(refs)))
	return &logproto.PrefetchBloomBlocksResponse{}, err
}

// FilterChunkRefs implements BloomGatewayServer
func (g *Gateway) FilterChunkRefs(ctx context.Context, req *logproto.FilterChunkRefRequest) (*logproto.FilterChunkRefResponse, error) {
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	sp, ctx := opentracing.StartSpanFromContext(ctx, "bloomgateway.FilterChunkRefs")
	stats, ctx := ContextWithEmptyStats(ctx)
	logger := spanlogger.FromContextWithFallback(
		ctx,
		utillog.WithContext(ctx, g.logger),
	)

	defer func() {
		level.Info(logger).Log(stats.KVArgs()...)
		sp.Finish()
	}()

	// start time == end time --> empty response
	if req.From.Equal(req.Through) {
		stats.Status = labelSuccess
		return &logproto.FilterChunkRefResponse{
			ChunkRefs: []*logproto.GroupedChunkRefs{},
		}, nil
	}

	// start time > end time --> error response
	if req.Through.Before(req.From) {
		stats.Status = labelFailure
		return nil, errors.New("from time must not be after through time")
	}

	matchers := v1.ExtractTestableLabelMatchers(req.Plan.AST)
	stats.NumMatchers = len(matchers)
	g.metrics.receivedMatchers.Observe(float64(len(matchers)))

	// Shortcut if request does not contain filters
	if len(matchers) == 0 {
		stats.Status = labelSuccess
		return &logproto.FilterChunkRefResponse{ChunkRefs: req.Refs}, nil
	}

	blocks, err := decodeBlockKeys(req.Blocks)
	if err != nil {
		stats.Status = labelFailure
		return nil, err
	}

	// Shortcut if request does not contain blocks
	if len(blocks) == 0 {
		stats.Status = labelSuccess
		return &logproto.FilterChunkRefResponse{ChunkRefs: req.Refs}, nil
	}

	seriesByDay := partitionRequest(req)
	stats.NumTasks = len(seriesByDay)

	sp.LogKV(
		"matchers", len(matchers),
		"days", len(seriesByDay),
		"blocks", len(req.Blocks),
		"series_requested", len(req.Refs),
	)

	// len(seriesByDay) should never be 0
	// Not sure how this can happen, but there was a bug report
	// https://github.com/grafana/loki/issues/14623
	if len(seriesByDay) == 0 {
		stats.Status = labelSuccess
		return &logproto.FilterChunkRefResponse{ChunkRefs: req.Refs}, nil
	}

	if len(seriesByDay) > 1 {
		stats.Status = labelFailure
		return nil, errors.New("request time range must span exactly one day")
	}

	series := seriesByDay[0]
	task := newTask(ctx, tenantID, series, matchers, blocks)

	// TODO(owen-d): include capacity in constructor?
	task.responses = responsesPool.Get(len(series.series))
	// free up the responses
	defer responsesPool.Put(task.responses)

	g.activeUsers.UpdateUserTimestamp(tenantID, time.Now())

	var preFilterSeries, preFilterChunks int

	preFilterSeries = len(req.Refs)
	for _, series := range req.Refs {
		preFilterChunks += len(series.Refs)
	}

	tasksCh := make(chan Task, 1)

	// TODO(owen-d): gracefully handle full queues
	task.enqueueTime = time.Now()
	if err := g.queue.Enqueue(tenantID, nil, task, func() {
		// When enqueuing, we also add the task to the pending tasks
		_ = g.pendingTasks.Inc()
	}); err != nil {
		stats.Status = labelFailure
		return nil, errors.Wrap(err, "failed to enqueue task")
	}
	// TODO(owen-d): use `concurrency` lib, bound parallelism
	go g.consumeTask(ctx, task, tasksCh)

	combinedRecorder := v1.NewBloomRecorder(ctx, "combined")

	select {
	case <-ctx.Done():
		stats.Status = "cancel"
		return nil, errors.Wrap(ctx.Err(), "request failed")
	case task = <-tasksCh:
		if task.Err() != nil {
			stats.Status = labelFailure
			return nil, errors.Wrap(task.Err(), "request failed")
		}
		combinedRecorder.Merge(task.recorder)
	}

	combinedRecorder.Report(utillog.WithContext(ctx, g.logger), g.bloomStore.BloomMetrics())

	start := time.Now()
	filtered := filterChunkRefs(req, task.responses)
	duration := time.Since(start)
	stats.AddPostProcessingTime(duration)

	var postFilterSeries, postFilterChunks int
	postFilterSeries = len(filtered)
	for _, group := range filtered {
		postFilterChunks += len(group.Refs)
	}

	g.metrics.requestedSeries.Observe(float64(preFilterSeries))
	g.metrics.filteredSeries.Observe(float64(preFilterSeries - postFilterSeries))
	g.metrics.requestedChunks.Observe(float64(preFilterChunks))
	g.metrics.filteredChunks.Observe(float64(preFilterChunks - postFilterChunks))

	stats.Status = "success"
	stats.SeriesRequested = preFilterSeries
	stats.SeriesFiltered = preFilterSeries - postFilterSeries
	stats.ChunksRequested = preFilterChunks
	stats.ChunksFiltered = preFilterChunks - postFilterChunks

	sp.LogKV("msg", "return filtered chunk refs")

	return &logproto.FilterChunkRefResponse{ChunkRefs: filtered}, nil
}

// consumeTask receives v1.Output yielded from the block querier on the task's
// result channel and stores them on the task.
// In case the context task is done, it drains the remaining items until the
// task is closed by the worker.
// Once the tasks is closed, it will send the task with the results from the
// block querier to the supplied task channel.
func (g *Gateway) consumeTask(ctx context.Context, task Task, tasksCh chan<- Task) {
	for res := range task.resCh {
		select {
		case <-ctx.Done():
			// do nothing
		default:
			// chunks may not always be sorted
			if !slices.IsSortedFunc(res.Removals, func(a, b v1.ChunkRef) int { return a.Cmp(b) }) {
				slices.SortFunc(res.Removals, func(a, b v1.ChunkRef) int { return a.Cmp(b) })
			}
			task.responses = append(task.responses, res)
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

// TODO(owen-d): improve perf. This can be faster with a more specialized impl
// NB(owen-d): `req` is mutated in place for performance, but `responses` is not
// Removals of the outputs must be sorted.
func filterChunkRefs(req *logproto.FilterChunkRefRequest, responses []v1.Output) []*logproto.GroupedChunkRefs {
	// sort responses by fingerprint
	sort.Slice(responses, func(i, j int) bool { return responses[i].Fp < responses[j].Fp })

	res := make([]*logproto.GroupedChunkRefs, 0, len(req.Refs))

	// dedupe outputs, merging the same series.
	// This returns an Iterator[v1.Output]
	dedupedResps := iter.NewDedupingIter(
		// eq
		func(o1, o2 v1.Output) bool {
			return o1.Fp == o2.Fp
		},
		// from
		iter.Identity[v1.Output],
		// merge two removal sets for the same series, deduping
		// requires that the removals of the outputs are sorted
		func(o1, o2 v1.Output) v1.Output {
			res := v1.Output{Fp: o1.Fp}

			var chks v1.ChunkRefs
			var i, j int
			for i < len(o1.Removals) && j < len(o2.Removals) {

				a, b := o1.Removals[i], o2.Removals[j]

				if a == b {
					chks = append(chks, a)
					i++
					j++
					continue
				}

				if a.Less(b) {
					chks = append(chks, a)
					i++
					continue
				}
				chks = append(chks, b)
				j++
			}

			if i < len(o1.Removals) {
				chks = append(chks, o1.Removals[i:]...)
			}
			if j < len(o2.Removals) {
				chks = append(chks, o2.Removals[j:]...)
			}

			res.Removals = chks
			return res
		},
		iter.NewPeekIter(iter.NewSliceIter(responses)),
	)

	// Iterate through the requested and filtered series/chunks,
	// removing chunks that were filtered out.
	var next bool
	var at v1.Output
	if next = dedupedResps.Next(); next {
		at = dedupedResps.At()
	}

	for i := 0; i < len(req.Refs); i++ {
		// we've hit the end of the removals -- append the rest of the
		// requested series and return
		if !next {
			res = append(res, req.Refs[i:]...)
			return res
		}

		// the current series had no removals
		cur := req.Refs[i]
		if cur.Fingerprint < uint64(at.Fp) {
			res = append(res, cur)
			continue
		}

		// the current series had removals. No need to check for equality
		// b/c removals must be present in input
		filterChunkRefsForSeries(cur, at.Removals)
		if len(cur.Refs) > 0 {
			res = append(res, cur)
		}

		// advance removals
		if next = dedupedResps.Next(); next {
			at = dedupedResps.At()
		}
	}

	return res
}

// mutates cur
func filterChunkRefsForSeries(cur *logproto.GroupedChunkRefs, removals v1.ChunkRefs) {
	// use same backing array to avoid allocations
	res := cur.Refs[:0]

	var i, j int
	for i < len(cur.Refs) && j < len(removals) {

		if (*v1.ChunkRef)(cur.Refs[i]).Less(removals[j]) {
			// chunk was not removed
			res = append(res, cur.Refs[i])
			i++
		} else {
			// Since all removals must exist in the series, we can assume that if the removal
			// is not less, it must be equal to the current chunk (a match). Skip this chunk.
			i++
			j++
		}

	}

	if i < len(cur.Refs) {
		res = append(res, cur.Refs[i:]...)
	}

	cur.Refs = cur.Refs[:len(res)]
}

func decodeBlockKeys(keys []string) ([]bloomshipper.BlockRef, error) {
	blocks := make([]bloomshipper.BlockRef, 0, len(keys))
	for _, key := range keys {
		block, err := bloomshipper.BlockRefFromKey(key)
		if err != nil {
			return nil, errors.New("could not parse block key")
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}
