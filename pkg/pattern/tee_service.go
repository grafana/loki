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
	"github.com/prometheus/prometheus/util/pool"

	"github.com/grafana/dskit/instrument"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/user"

	"github.com/grafana/loki/v3/pkg/distributor"
	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util/constants"

	ring_client "github.com/grafana/dskit/ring/client"
)

type TeeService struct {
	cfg        Config
	logger     log.Logger
	ringClient RingClient
	wg         *sync.WaitGroup

	ingesterAppends       *prometheus.CounterVec
	ingesterMetricAppends *prometheus.CounterVec

	teedStreams  *prometheus.CounterVec
	teedRequests *prometheus.CounterVec

	sendDuration *instrument.HistogramCollector

	flushQueue chan clientRequest

	bufferPool   *pool.Pool
	buffersMutex *sync.Mutex
	buffers      map[string][]distributor.KeyedStream
}

func NewTeeService(
	cfg Config,
	ringClient RingClient,
	metricsNamespace string,
	registerer prometheus.Registerer,
	logger log.Logger,
) (*TeeService, error) {
	registerer = prometheus.WrapRegistererWithPrefix(metricsNamespace+"_", registerer)

	t := &TeeService{
		logger: log.With(logger, "component", "pattern-tee"),
		ingesterAppends: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "pattern_ingester_appends_total",
			Help: "The total number of batch appends sent to pattern ingesters.",
		}, []string{"ingester", "status"}),
		ingesterMetricAppends: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "pattern_ingester_metric_appends_total",
			Help: "The total number of metric only batch appends sent to pattern ingesters. These requests will not be processed for patterns.",
		}, []string{"status"}),
		teedStreams: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "pattern_ingester_teed_streams_total",
			Help: "The total number of streams teed to the pattern ingester.",
		}, []string{"status"}),
		teedRequests: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "pattern_ingester_teed_requests_total",
			Help: "The total number of batch appends sent to fallback pattern ingesters, for not owned streams.",
		}, []string{"status"}),
		sendDuration: instrument.NewHistogramCollector(
			promauto.With(registerer).NewHistogramVec(
				prometheus.HistogramOpts{
					Namespace: constants.Loki,
					Name:      "pattern_ingester_tee_send_duration_seconds",
					Help:      "Time spent sending batches from the tee to the pattern ingester",
					Buckets:   prometheus.DefBuckets,
				}, instrument.HistogramCollectorBuckets,
			),
		),
		cfg:        cfg,
		ringClient: ringClient,

		wg:           &sync.WaitGroup{},
		buffersMutex: &sync.Mutex{},
		buffers:      make(map[string][]distributor.KeyedStream),
		flushQueue:   make(chan clientRequest, cfg.TeeConfig.FlushQueueSize),
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
	ts.buffersMutex.Lock()
	if len(ts.buffers) == 0 {
		ts.buffersMutex.Unlock()
		return
	}

	buffered := ts.buffers
	ts.buffers = make(map[string][]distributor.KeyedStream)
	ts.buffersMutex.Unlock()

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
			select {
			case ts.flushQueue <- clientRequest{
				ingesterAddr: addr,
				tenant:       tenant,
				reqs:         reqs,
			}:
				ts.teedRequests.WithLabelValues("queued").Inc()
			default:
				ts.teedRequests.WithLabelValues("dropped").Inc()
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
			ts.teedStreams.WithLabelValues("dropped").Inc()
			continue
		}

		addr := replicationSet.Instances[0].Addr
		batch, ok := batches[tenant][addr]
		if !ok {
			batch = &logproto.PushRequest{}
			batches[tenant][addr] = batch
		}

		if len(stream.Stream.Entries) > 0 {
			batch.Streams = append(batch.Streams, stream.Stream)
			ts.teedStreams.WithLabelValues("batched").Inc()
		}
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
			"FlushTeedLogsToPatternIngested",
			ts.sendDuration,
			instrument.ErrorCode,
			func(ctx context.Context) error {
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
					ts.ingesterAppends.WithLabelValues(clientRequest.ingesterAddr, "success").Inc()
					ts.ingesterMetricAppends.WithLabelValues("success").Inc()
					return nil
				}

				// The pattern ingester appends failed, but we can retry the metric append
				ts.ingesterAppends.WithLabelValues(clientRequest.ingesterAddr, "fail").Inc()
				level.Error(ts.logger).Log("msg", "failed to send patterns to pattern ingester", "err", err)

				if !ts.cfg.MetricAggregation.Enabled {
					return err
				}

				// Pattern ingesters serve 2 functions, processing patterns and aggregating metrics.
				// Only owned streams are processed for patterns, however any pattern ingester can
				// aggregate metrics for any stream. Therefore, if we can't send the owned stream,
				// try to forward request to any pattern ingester so we at least capture the metrics.
				replicationSet, err := ts.ringClient.Ring().
					GetReplicationSetForOperation(ring.WriteNoExtend)
				if err != nil || len(replicationSet.Instances) == 0 {
					ts.ingesterMetricAppends.WithLabelValues("fail").Inc()
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

						ts.ingesterMetricAppends.WithLabelValues("success").Inc()
						// bail after any success to prevent sending more than one
						return nil
					}
				}

				ts.ingesterMetricAppends.WithLabelValues("fail").Inc()
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
func (ts *TeeService) Duplicate(tenant string, streams []distributor.KeyedStream) {
	if !ts.cfg.Enabled {
		return
	}

	if len(streams) == 0 {
		return
	}

	for _, stream := range streams {
		lbls, err := syntax.ParseLabels(stream.Stream.Labels)
		if err != nil {
			level.Error(ts.logger).
				Log("msg", "error parsing stream labels", "labels", stream.Stream.Labels, "err", err)

			continue
		}

		if lbls.Has(push.AggregatedMetricLabel) {
			continue
		}

		ts.buffersMutex.Lock()
		ts.buffers[tenant] = append(ts.buffers[tenant], stream)
		ts.buffersMutex.Unlock()
	}
}
