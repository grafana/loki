package pattern

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/dskit/instrument"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/user"

	"github.com/grafana/loki/v3/pkg/distributor"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"

	ring_client "github.com/grafana/dskit/ring/client"
)

// teeMetrics contains the metrics for [TeeService].
type teeMetrics struct {
	bufferedBytes         prometheus.Summary
	ingesterAppends       *prometheus.CounterVec
	ingesterMetricAppends *prometheus.CounterVec
	teedStreams           *prometheus.CounterVec
	teedRequests          *prometheus.CounterVec
	sendDuration          *instrument.HistogramCollector
}

// newTeeMetrics returns new teeMetrics.
func newTeeMetrics(reg prometheus.Registerer) *teeMetrics {
	m := teeMetrics{
		bufferedBytes: promauto.With(reg).NewSummary(prometheus.SummaryOpts{
			Name:       "pattern_ingester_tee_buffered_bytes_high_watermark",
			Help:       "The max buffered bytes in the last 1 minute.",
			Objectives: map[float64]float64{1.0: 0.1},
			MaxAge:     time.Minute,
		}),
		ingesterAppends: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "pattern_ingester_appends_total",
			Help: "The total number of batch appends sent to pattern ingesters.",
		}, []string{"ingester", "status"}),
		ingesterMetricAppends: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "pattern_ingester_metric_appends_total",
			Help: "The total number of metric only batch appends sent to pattern ingesters. These requests will not be processed for patterns.",
		}, []string{"status"}),
		teedStreams: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "pattern_ingester_teed_streams_total",
			Help: "The total number of streams teed to the pattern ingester.",
		}, []string{"status"}),
		teedRequests: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "pattern_ingester_teed_requests_total",
			Help: "The total number of batch appends sent to fallback pattern ingesters, for not owned streams.",
		}, []string{"status"}),
		sendDuration: instrument.NewHistogramCollector(
			promauto.With(reg).NewHistogramVec(
				prometheus.HistogramOpts{
					Name:    "pattern_ingester_tee_send_duration_seconds",
					Help:    "Time spent sending batches from the tee to the pattern ingester",
					Buckets: prometheus.DefBuckets,
				}, instrument.HistogramCollectorBuckets,
			),
		),
	}
	return &m
}

type TeeService struct {
	cfg        Config
	limits     Limits
	tenantCfgs *runtime.TenantConfigs
	logger     log.Logger
	ringClient RingClient
	wg         *sync.WaitGroup

	flushQueue chan clientRequest

	bufMtx *sync.Mutex
	buf    map[string][]distributor.KeyedStream

	// bufferedBytes is a count of the total number of bytes in buf, and all
	// client requests in the flushQueue.
	bufferedBytes    int64
	bufferedBytesMtx sync.Mutex

	metrics *teeMetrics
}

func NewTeeService(
	cfg Config,
	limits Limits,
	ringClient RingClient,
	tenantCfgs *runtime.TenantConfigs,
	metricsNamespace string,
	reg prometheus.Registerer,
	logger log.Logger,
) (*TeeService, error) {
	reg = prometheus.WrapRegistererWithPrefix(metricsNamespace+"_", reg)
	t := &TeeService{
		logger:     log.With(logger, "component", "pattern-tee"),
		cfg:        cfg,
		limits:     limits,
		tenantCfgs: tenantCfgs,
		ringClient: ringClient,
		wg:         &sync.WaitGroup{},
		bufMtx:     &sync.Mutex{},
		buf:        make(map[string][]distributor.KeyedStream),
		flushQueue: make(chan clientRequest, cfg.TeeConfig.FlushQueueSize),
		metrics:    newTeeMetrics(reg),
	}
	return t, nil
}

func (ts *TeeService) Start(runCtx context.Context) error {
	ts.wg.Add(1)

	// Start all batchSenders. We don't use the Run() context here, because we
	// want the senders to finish sending any currently in-flight data and the
	// remining batches in the queue before the TeeService fully stops.
	//
	// Still, we have a maximum amount of time we will wait after the TeeService
	// is stopped, see cfg.StopFlushTimeout below.
	senderCtx, senderCancel := context.WithCancel(context.Background())

	sendersWg := &sync.WaitGroup{}
	sendersWg.Add(ts.cfg.TeeConfig.FlushWorkerCount)
	for i := 0; i < ts.cfg.TeeConfig.FlushWorkerCount; i++ {
		go func() {
			ts.batchSender(senderCtx)
			sendersWg.Done()
		}()
	}

	// We need this to implement the select with StopFlushTimeout below
	sendersDone := make(chan struct{})
	go func() {
		sendersWg.Wait()
		close(sendersDone)
	}()

	go func() {
		// We wait for the Run() context to be done, so we know we are stopping
		<-runCtx.Done()

		// The senders euther stop normally in the allotted time, or we hit the
		// timeout and cancel thir context. In either case, we wait for them to
		// finish before we consider the service to be done.
		select {
		case <-time.After(ts.cfg.TeeConfig.StopFlushTimeout):
			senderCancel() // Cancel any remaining senders
			<-sendersDone  // Wait for them to be done
		case <-sendersDone:
		}
		ts.wg.Done()
	}()

	go func() {
		t := time.NewTicker(ts.cfg.TeeConfig.BatchFlushInterval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				ts.flush()
			case <-runCtx.Done():
				// Final flush to send anything currently buffered
				ts.flush()
				close(ts.flushQueue) // nothing will write to it anymore
				return
			}
		}
	}()

	return nil
}

func (ts *TeeService) WaitUntilDone() {
	ts.wg.Wait()
}

func (ts *TeeService) flush() {
	ts.bufMtx.Lock()
	if len(ts.buf) == 0 {
		ts.bufMtx.Unlock()
		return
	}

	buffered := ts.buf
	ts.buf = make(map[string][]distributor.KeyedStream)
	ts.bufMtx.Unlock()

	batches := make([]map[string]map[string]*logproto.PushRequest, 0, len(buffered))
	for tenant, streams := range buffered {
		batches = append(batches, ts.batchesForTenant(tenant, streams))
	}

	byTenantAndPatternIngester := make(map[string]map[string][]*logproto.PushRequest)
	for _, b := range batches {
		for tenant, requests := range b {
			for addr, req := range requests {
				byTenant, ok := byTenantAndPatternIngester[tenant]
				if !ok {
					byTenant = make(map[string][]*logproto.PushRequest)
				}

				byTenant[addr] = append(
					byTenant[addr],
					req,
				)

				byTenantAndPatternIngester[tenant] = byTenant
			}
		}
	}

	for tenant, requests := range byTenantAndPatternIngester {
		for addr, reqs := range requests {
			// Calculate the total size of all streams.
			var totalSize int
			for _, req := range reqs {
				for _, stream := range req.Streams {
					totalSize += stream.Size()
				}
			}
			select {
			case ts.flushQueue <- clientRequest{
				ingesterAddr: addr,
				tenant:       tenant,
				reqs:         reqs,
				size:         totalSize,
			}:
				ts.metrics.teedRequests.WithLabelValues("queued").Inc()
			default:
				ts.metrics.teedRequests.WithLabelValues("dropped").Inc()
				ts.releaseBufferedBytes(totalSize)
			}
		}
	}
}

func (ts *TeeService) batchesForTenant(
	tenant string,
	streams []distributor.KeyedStream,
) map[string]map[string]*logproto.PushRequest {
	batches := map[string]map[string]*logproto.PushRequest{
		tenant: make(map[string]*logproto.PushRequest),
	}

	if len(streams) == 0 {
		return batches
	}

	for _, stream := range streams {
		var descs [1]ring.InstanceDesc
		replicationSet, err := ts.ringClient.Ring().
			Get(stream.HashKey, ring.WriteNoExtend, descs[:0], nil, nil)
		if err != nil || len(replicationSet.Instances) == 0 {
			ts.releaseBufferedBytes(stream.Stream.Size())
			ts.metrics.teedStreams.WithLabelValues("dropped").Inc()
			continue
		}

		addr := replicationSet.Instances[0].Addr
		batch, ok := batches[tenant][addr]
		if !ok {
			batch = &logproto.PushRequest{}
			batches[tenant][addr] = batch
		}

		batch.Streams = append(batch.Streams, stream.Stream)
		ts.metrics.teedStreams.WithLabelValues("batched").Inc()
	}

	streamCount := uint64(len(streams))
	level.Debug(ts.logger).Log(
		"msg", "prepared pattern Tee batches for tenant",
		"tenant", tenant,
		"stream_count", streamCount,
	)

	return batches
}

type clientRequest struct {
	ingesterAddr string
	tenant       string
	reqs         []*logproto.PushRequest
	// size is the total size of all streams in reqs.
	size int
}

func (ts *TeeService) batchSender(ctx context.Context) {
	for {
		select {
		case clientReq, ok := <-ts.flushQueue:
			if !ok {
				return // we are done, the queue was closed by Run()
			}
			ts.sendBatch(ctx, clientReq)
		case <-ctx.Done():
			return
		}
	}
}

func (ts *TeeService) sendBatch(ctx context.Context, clientRequest clientRequest) {
	defer ts.releaseBufferedBytes(clientRequest.size)

	ctx, cancel := context.WithTimeout(ctx, ts.cfg.ConnectionTimeout)
	defer cancel()

	for i := 0; i < len(clientRequest.reqs); i++ {
		req := clientRequest.reqs[i]

		if len(req.Streams) == 0 {
			continue
		}

		// Nothing to do with this error. It's recorded in the metrics that
		// are gathered by this request
		_ = instrument.CollectedRequest(
			ctx,
			"FlushTeedLogsToPatternIngester",
			ts.metrics.sendDuration,
			instrument.ErrorCode,
			func(ctx context.Context) error {
				sp := spanlogger.FromContext(ctx, ts.logger)
				client, err := ts.ringClient.GetClientFor(clientRequest.ingesterAddr)
				if err != nil {
					return err
				}
				ctx, cancel := context.WithTimeout(
					user.InjectOrgID(ctx, clientRequest.tenant),
					ts.cfg.ClientConfig.RemoteTimeout,
				)

				// First try to send the request to the correct pattern ingester
				defer cancel()
				_, err = client.(logproto.PatternClient).Push(ctx, req)
				if err == nil {
					// Success here means the stream will be processed for both metrics and patterns
					ts.metrics.ingesterAppends.WithLabelValues(clientRequest.ingesterAddr, "success").Inc()
					ts.metrics.ingesterMetricAppends.WithLabelValues("success").Inc()

					// limit logged labels to 1000
					labelsLimit := len(req.Streams)
					if labelsLimit > 1000 {
						labelsLimit = 1000
					}

					labels := make([]string, 0, labelsLimit)
					for _, stream := range req.Streams {
						if len(labels) >= 1000 {
							break
						}

						labels = append(labels, stream.Labels)
					}

					sp.LogKV(
						"event", "forwarded push request to pattern ingester",
						"num_streams", len(req.Streams),
						"first_1k_labels", strings.Join(labels, ", "),
						"tenant", clientRequest.tenant,
					)

					// this is basically the same as logging push request streams,
					// so put it behind the same flag
					if ts.tenantCfgs.LogPushRequestStreams(clientRequest.tenant) {
						level.Debug(ts.logger).
							Log(
								"msg", "forwarded push request to pattern ingester",
								"num_streams", len(req.Streams),
								"first_1k_labels", strings.Join(labels, ", "),
								"tenant", clientRequest.tenant,
							)
					}

					return nil
				}

				// The pattern ingester appends failed, but we can retry the metric append
				ts.metrics.ingesterAppends.WithLabelValues(clientRequest.ingesterAddr, "fail").Inc()
				level.Error(ts.logger).Log("msg", "failed to send patterns to pattern ingester", "err", err)

				// Pattern ingesters serve 2 functions, processing patterns and aggregating metrics.
				// Only owned streams are processed for patterns, however any pattern ingester can
				// aggregate metrics for any stream. Therefore, if we can't send the owned stream,
				// try to forward request to any pattern ingester so we at least capture the metrics.

				if !ts.limits.MetricAggregationEnabled(clientRequest.tenant) {
					return err
				}

				replicationSet, err := ts.ringClient.Ring().
					GetReplicationSetForOperation(ring.WriteNoExtend)
				if err != nil || len(replicationSet.Instances) == 0 {
					ts.metrics.ingesterMetricAppends.WithLabelValues("fail").Inc()
					level.Error(ts.logger).Log(
						"msg", "failed to send metrics to fallback pattern ingesters",
						"num_instances", len(replicationSet.Instances),
						"err", err,
					)
					return errors.New("no instances found for fallback")
				}

				fallbackAddrs := make([]string, 0, len(replicationSet.Instances))
				for _, instance := range replicationSet.Instances {
					addr := instance.Addr
					fallbackAddrs = append(fallbackAddrs, addr)

					var client ring_client.PoolClient
					client, err = ts.ringClient.GetClientFor(addr)
					if err != nil {
						ctx, cancel := context.WithTimeout(
							user.InjectOrgID(ctx, clientRequest.tenant),
							ts.cfg.ClientConfig.RemoteTimeout,
						)
						defer cancel()

						_, err = client.(logproto.PatternClient).Push(ctx, req)
						if err != nil {
							continue
						}

						ts.metrics.ingesterMetricAppends.WithLabelValues("success").Inc()
						// bail after any success to prevent sending more than one
						return nil
					}
				}

				ts.metrics.ingesterMetricAppends.WithLabelValues("fail").Inc()
				level.Error(ts.logger).Log(
					"msg", "failed to send metrics to fallback pattern ingesters. exhausted all fallback instances",
					"addresses", strings.Join(fallbackAddrs, ", "),
					"err", err,
				)
				return err
			})
	}
}

// Duplicate Implements distributor.Tee which is used to tee distributor requests to pattern ingesters.
func (ts *TeeService) Duplicate(_ context.Context, tenant string, streams []distributor.KeyedStream, _ *distributor.PushTracker) {
	if !ts.cfg.Enabled || len(streams) == 0 {
		return
	}

	for _, stream := range streams {
		// Skip streams with no entries.
		if len(stream.Stream.Entries) == 0 {
			continue
		}

		lbls, err := syntax.ParseLabels(stream.Stream.Labels)
		if err != nil {
			level.Error(ts.logger).Log("msg", "error parsing stream labels", "labels", stream.Stream.Labels, "err", err)
			continue
		}

		// Skip streams that are already aggregated metrics or patterns to avoid loops
		if lbls.Has(constants.AggregatedMetricLabel) || lbls.Has(constants.PatternLabel) {
			continue
		}

		// Check that the stream is allowed within the current limit.
		size := stream.Stream.Size()
		if !ts.reserveBufferedBytes(size) {
			ts.metrics.teedStreams.WithLabelValues("dropped").Inc()
			continue
		}

		ts.bufMtx.Lock()
		ts.buf[tenant] = append(ts.buf[tenant], stream)
		ts.bufMtx.Unlock()
	}
}

// reserveBufferedBytes attempts to reserve size bytes of capacity in the tee.
// It returns true on success, otherwise false. A reservation is unsuccessful
// if size would cause the tee to exceed [TeeConfig.MaxBufferedBytes]. When
// [TeeConfig.MaxBufferedBytes] is zero, the limit is disabled.
//
// All reserved sizes must be returned by calling releaseBufferedBytes with
// the same value.
//
// It is safe for concurrent use.
func (ts *TeeService) reserveBufferedBytes(size int) bool {
	maxBufferedBytes := int64(ts.cfg.TeeConfig.MaxBufferedBytes)

	ts.bufferedBytesMtx.Lock()
	newVal := ts.bufferedBytes + int64(size)
	if maxBufferedBytes > 0 && newVal > maxBufferedBytes {
		ts.bufferedBytesMtx.Unlock()
		return false
	}

	ts.bufferedBytes = newVal
	ts.bufferedBytesMtx.Unlock()

	ts.metrics.bufferedBytes.Observe(float64(newVal))
	return true
}

// releaseBufferedBytes returns size bytes of reserved capacity to the tee.
// It must be called whenever previously reserved capacity is no longer
// needed.
//
// It is safe for concurrent use.
func (ts *TeeService) releaseBufferedBytes(size int) {
	ts.bufferedBytesMtx.Lock()
	newVal := ts.bufferedBytes - int64(size)
	if newVal < 0 {
		ts.bufferedBytesMtx.Unlock()
		panic("bufferedBytes: released more than reserved")
	}

	ts.bufferedBytes = newVal
	ts.bufferedBytesMtx.Unlock()
}

func (ts *TeeService) Register(_ context.Context, _ string, _ []distributor.KeyedStream, _ *distributor.PushTracker) {
	// we don't register pending streams to avoid blocking the distributor due to pattern ingesters.
}
