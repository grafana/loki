package pattern

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/instrument"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/grpc/health/grpc_health_v1"

	ring_client "github.com/grafana/dskit/ring/client"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/kafkav2"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/pattern/aggregation"
	"github.com/grafana/loki/v3/pkg/pattern/drain"
	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
)

// A processor receives records and builds data objects from them.
// flushRequest is used to send a flush request to the processor's Run loop.
type flushRequest struct {
	done chan<- error
}

type KafkaIngester struct {
	services.Service
	lifecycler *ring.Lifecycler
	ringClient RingClient

	lifecyclerWatcher *services.FailureWatcher

	cfg        Config
	limits     Limits
	registerer prometheus.Registerer
	logger     log.Logger

	instancesMtx sync.RWMutex
	instances    map[string]*instance

	// One queue per flush thread.  Fingerprint is used to
	// pick a queue.
	flushQueues     []*util.PriorityQueue
	flushQueuesDone sync.WaitGroup
	loopDone        sync.WaitGroup
	loopQuit        chan struct{}

	metrics       *ingesterMetrics
	drainCfg      *drain.Config
	records       chan *kgo.Record
	consumer      *kafkav2.SinglePartitionConsumer
	partitionRing ring.PartitionRingReader
	wg            *sync.WaitGroup
	decoder       *kafka.Decoder
	flushRequests chan flushRequest
	tenantCfgs    *runtime.TenantConfigs
}

func NewKafka(
	cfg Config,
	limits Limits,
	ringClient RingClient,
	tenantCfgs *runtime.TenantConfigs,
	metricsNamespace string,
	registerer prometheus.Registerer,
	logger log.Logger,
	kafkaCfg kafka.Config,
) (*KafkaIngester, error) {
	metrics := newIngesterMetrics(registerer, metricsNamespace)
	registerer = prometheus.WrapRegistererWithPrefix(metricsNamespace+"_", registerer)

	drainCfg := drain.DefaultConfig()
	drainCfg.MaxClusters = cfg.MaxClusters
	drainCfg.MaxEvictionRatio = cfg.MaxEvictionRatio
	drainCfg.MaxChunkAge = cfg.MaxChunkAge
	drainCfg.SampleInterval = cfg.PatternSampleInterval

	i := &KafkaIngester{
		cfg:         cfg,
		limits:      limits,
		ringClient:  ringClient,
		logger:      log.With(logger, "component", "pattern-ingester"),
		registerer:  registerer,
		metrics:     metrics,
		instances:   make(map[string]*instance),
		flushQueues: make([]*util.PriorityQueue, cfg.ConcurrentFlushes),
		loopQuit:    make(chan struct{}),
		wg:          &sync.WaitGroup{},
		drainCfg:    drainCfg,
		tenantCfgs:  tenantCfgs,
	}
	decoder, err := kafka.NewDecoder()
	if err != nil {
		return nil, fmt.Errorf("create kafka decoder: %w", err)
	}
	i.decoder = decoder
	readerCfg := kafkaCfg
	readerCfg.Topic = kafkaCfg.Topic
	consumerGroup := defaultPatternConsumerGroup
	if readerCfg.ConsumerGroup != "" {
		consumerGroup = readerCfg.ConsumerGroup
	}
	readerCfg.ConsumerGroup = consumerGroup
	readerClient, err := client.NewReaderClient("loki.dataobj_consumer", readerCfg, logger, registerer)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for data topic: %w", err)
	}
	i.flushRequests = make(chan flushRequest, 1)

	// need to set up a partition ring before starting the consumer, so we can have a partition ID to use
	records := make(chan *kgo.Record)
	i.records = records
	var partitionID int32 = 0
	i.consumer = kafkav2.NewSinglePartitionConsumer(
		readerClient,
		kafkaCfg.Topic,
		partitionID,
		kafkav2.OffsetStart, // We fetch the real initial offset before starting the service.
		records,
		logger,
		prometheus.WrapRegistererWithPrefix("loki_pattern_consumer_", registerer),
	)
	kvClient, err := kv.NewClient(kv.Config{}, ring.GetPartitionRingCodec(), registerer, logger)
	ringOptions := ring.DefaultPartitionRingOptions()
	i.partitionRing = ring.NewPartitionRingWatcherWithOptions(PartitionRingName+"watcher", PartitionRingKey, kvClient, ringOptions, logger, registerer)

	i.Service = services.NewBasicService(i.starting, i.running, i.stopping)
	i.lifecycler, err = ring.NewLifecycler(cfg.LifecyclerConfig, i, "pattern-ingester", "pattern-ring", true, i.logger, registerer)
	if err != nil {
		return nil, err
	}

	i.lifecyclerWatcher = services.NewFailureWatcher()
	i.lifecyclerWatcher.WatchService(i.lifecycler)

	return i, nil
}

func (i *KafkaIngester) getEffectivePersistenceGranularity(userID string) time.Duration {
	tenantGranularity := i.limits.PersistenceGranularity(userID)
	if tenantGranularity > 0 && tenantGranularity <= i.cfg.MaxChunkAge {
		return tenantGranularity
	}
	return i.cfg.MaxChunkAge
}

// ServeHTTP implements the pattern ring status page.
func (i *KafkaIngester) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	i.lifecycler.ServeHTTP(w, r)
}

func (i *KafkaIngester) starting(ctx context.Context) error {
	// pass new context to lifecycler, so that it doesn't stop automatically when Ingester's service context is done
	level.Debug(i.logger).Log("msg", "Starting Kafka ingester")
	err := i.lifecycler.StartAsync(context.Background())
	if err != nil {
		return err
	}

	err = i.lifecycler.AwaitRunning(ctx)
	if err != nil {
		return err
	}

	// Start all batchSenders. We don't use the Run() context here, because we
	// want the senders to finish sending any currently in-flight data and the
	// remining batches in the queue before the service fully stops.
	//
	// Still, we have a maximum amount of time we will wait after the service
	// is stopped, see cfg.StopFlushTimeout below.
	senderCtx, senderCancel := context.WithCancel(context.Background())

	sendersWg := &sync.WaitGroup{}
	sendersWg.Add(i.cfg.FlushWorkerCount)
	for j := 0; j < i.cfg.FlushWorkerCount; j++ {
		go func() {
			i.sender(senderCtx)
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
		<-ctx.Done()

		// The senders either stop normally in the allotted time, or we hit the
		// timeout and cancel thir context. In either case, we wait for them to
		// finish before we consider the service to be done.
		select {
		case <-time.After(i.cfg.StopFlushTimeout):
			senderCancel() // Cancel any remaining senders
			<-sendersDone  // Wait for them to be done
		case <-sendersDone:
		}
		i.wg.Done()
	}()
	i.initFlushQueues()
	// start our loop
	i.loopDone.Add(1)
	go i.loop()
	return nil
}

func (i *KafkaIngester) running(ctx context.Context) error {
	var serviceError error
	select {
	// wait until service is asked to stop
	case <-ctx.Done():
	// stop
	case err := <-i.lifecyclerWatcher.Chan():
		serviceError = fmt.Errorf("lifecycler failed: %w", err)
	}

	close(i.loopQuit)
	i.loopDone.Wait()
	return serviceError
}

func (i *KafkaIngester) stopping(_ error) error {
	err := services.StopAndAwaitTerminated(context.Background(), i.lifecycler)
	for _, flushQueue := range i.flushQueues {
		flushQueue.Close()
	}
	i.flushQueuesDone.Wait()

	// Flush all patterns before stopping writers to ensure patterns are persisted
	i.flushPatterns()

	i.stopWriters()
	return err
}

func (s *KafkaIngester) sender(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			level.Info(s.logger).Log("msg", "context canceled")
			// We don't return ctx.Err() here as it manifests as a service failure
			// when stopping the service.
			return nil
		case rec, ok := <-s.records:
			if !ok {
				level.Info(s.logger).Log("msg", "channel closed")
				return nil
			}
			tenant := string(rec.Key)
			stream, err := s.decoder.DecodeWithoutLabels(rec.Value)
			if err != nil {
				// This is an unrecoverable error and no amount of retries will fix it.
				return fmt.Errorf("failed to decode stream: %w", err)
			}
			ingesterAddr, err := s.ingesterForTenant(tenant, stream)
			if err != nil {
				return fmt.Errorf("failed to get ingester for tenant: %w", err)
			}
			req := clientRequest{ingesterAddr: ingesterAddr, tenant: tenant, reqs: []*logproto.PushRequest{{Streams: []logproto.Stream{stream}}}, size: stream.Size()}
			s.sendReq(ctx, req)
		case req := <-s.flushRequests:
			// Drain any records that are already in the channel before flushing
			// so that all pending data is included in the flush.
		drain:
			for {
				select {
				case rec, ok := <-s.records:
					if !ok {
						break drain
					}
					tenant := string(rec.Key)
					stream, err := s.decoder.DecodeWithoutLabels(rec.Value)
					if err != nil {
						level.Error(s.logger).Log("msg", "failed to process record during flush drain", "err", err)
					}
					req := clientRequest{ingesterAddr: "", tenant: tenant, reqs: []*logproto.PushRequest{{Streams: []logproto.Stream{stream}}}, size: stream.Size()}
					s.sendReq(ctx, req)
				default:
					break drain
				}
			}
			req.done <- nil
		}
	}
}

func (s *KafkaIngester) ingesterForTenant(tenant string, stream logproto.Stream) (string, error) {
	var descs [1]ring.InstanceDesc
	replicationSet, err := s.ringClient.Ring().Get(uint32(stream.Hash), ring.WriteNoExtend, descs[:0], nil, nil)
	if err != nil {
		return "", err
	}
	return replicationSet.Instances[0].Addr, nil
}

func (s *KafkaIngester) sendReq(ctx context.Context, clientRequest clientRequest) {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.ConnectionTimeout)
	defer cancel()

	req := clientRequest.reqs[0]

	if len(req.Streams) == 0 {
		return
	}

	// Nothing to do with this error. It's recorded in the metrics that
	// are gathered by this request
	_ = instrument.CollectedRequest(
		ctx,
		"FlushTeedLogsToPatternIngester",
		s.metrics.sendDuration,
		instrument.ErrorCode,
		func(ctx context.Context) error {
			sp := spanlogger.FromContext(ctx, s.logger)
			ctx, cancel := context.WithTimeout(
				user.InjectOrgID(ctx, clientRequest.tenant),
				s.cfg.ClientConfig.RemoteTimeout,
			)

			// First try to send the request to the correct pattern ingester instance
			defer cancel()
			_, err := s.Push(ctx, req)
			if err == nil {
				// Success here means the stream will be processed for both metrics and patterns
				s.metrics.ingesterAppends.WithLabelValues(clientRequest.ingesterAddr, "success").Inc()
				s.metrics.ingesterMetricAppends.WithLabelValues("success").Inc()

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
				if s.tenantCfgs.LogPushRequestStreams(clientRequest.tenant) {
					level.Debug(s.logger).
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
			s.metrics.ingesterAppends.WithLabelValues(clientRequest.ingesterAddr, "fail").Inc()
			level.Error(s.logger).Log("msg", "failed to send patterns to pattern ingester", "err", err)

			// Pattern ingesters serve 2 functions, processing patterns and aggregating metrics.
			// Only owned streams are processed for patterns, however any pattern ingester can
			// aggregate metrics for any stream. Therefore, if we can't send the owned stream,
			// try to forward request to any pattern ingester so we at least capture the metrics.

			if !s.limits.MetricAggregationEnabled(clientRequest.tenant) {
				return err
			}

			replicationSet, err := s.ringClient.Ring().
				GetReplicationSetForOperation(ring.WriteNoExtend)
			if err != nil || len(replicationSet.Instances) == 0 {
				s.metrics.ingesterMetricAppends.WithLabelValues("fail").Inc()
				level.Error(s.logger).Log(
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
				client, err = s.ringClient.GetClientFor(addr)
				if err == nil {
					ctx, cancel := context.WithTimeout(
						user.InjectOrgID(ctx, clientRequest.tenant),
						s.cfg.ClientConfig.RemoteTimeout,
					)
					defer cancel()

					_, err = client.(logproto.PatternClient).Push(ctx, req)
					if err != nil {
						continue
					}

					s.metrics.ingesterMetricAppends.WithLabelValues("success").Inc()
					// bail after any success to prevent sending more than one
					return nil
				}
			}

			s.metrics.ingesterMetricAppends.WithLabelValues("fail").Inc()
			level.Error(s.logger).Log(
				"msg", "failed to send metrics to fallback pattern ingesters. exhausted all fallback instances",
				"addresses", strings.Join(fallbackAddrs, ", "),
				"err", err,
			)
			return err
		})
}

func (i *KafkaIngester) loop() {
	defer i.loopDone.Done()

	// Delay the first flush operation by up to 0.8x the flush time period.
	// This will ensure that multiple ingesters started at the same time do not
	// flush at the same time. Flushing at the same time can cause concurrently
	// writing the same chunk to object storage, which in AWS S3 leads to being
	// rate limited.
	jitter := time.Duration(rand.Int63n(int64(float64(i.cfg.FlushCheckPeriod.Nanoseconds()) * 0.8))) //#nosec G404 -- Jitter does not require a CSPRNG -- nosemgrep: math-random-used
	initialDelay := time.NewTimer(jitter)
	defer initialDelay.Stop()

	level.Info(i.logger).Log("msg", "sleeping for initial delay before starting periodic flushing", "delay", jitter)

	select {
	case <-initialDelay.C:
		// do nothing and continue with flush loop
	case <-i.loopQuit:
		// ingester stopped while waiting for initial delay
		return
	}

	// Add +/- 20% of flush interval as jitter.
	// The default flush check period is 30s so max jitter will be 6s.
	j := i.cfg.FlushCheckPeriod / 5
	flushTicker := util.NewTickerWithJitter(i.cfg.FlushCheckPeriod, j)
	defer flushTicker.Stop()

	downsampleTicker := time.NewTimer(i.cfg.MetricAggregation.SamplePeriod)
	defer downsampleTicker.Stop()
	for {
		select {
		case <-flushTicker.C:
			i.sweepUsers(false, true)
		case t := <-downsampleTicker.C:
			downsampleTicker.Reset(i.cfg.MetricAggregation.SamplePeriod)
			now := model.TimeFromUnixNano(t.UnixNano())
			i.downsampleMetrics(now)
		case <-i.loopQuit:
			return
		}
	}
}

// Watch implements grpc_health_v1.HealthCheck.
func (*KafkaIngester) Watch(*grpc_health_v1.HealthCheckRequest, grpc_health_v1.Health_WatchServer) error {
	return nil
}

// ReadinessHandler is used to indicate to k8s when the ingesters are ready for
// the addition removal of another ingester. Returns 204 when the ingester is
// ready, 500 otherwise.
func (i *KafkaIngester) CheckReady(ctx context.Context) error {
	if s := i.State(); s != services.Running && s != services.Stopping {
		return fmt.Errorf("ingester not ready: %v", s)
	}
	return i.lifecycler.CheckReady(ctx)
}

func (i *KafkaIngester) TransferOut(_ context.Context) error {
	// todo may be.
	return ring.ErrTransferDisabled
}

func (i *KafkaIngester) Push(ctx context.Context, req *logproto.PushRequest) (*logproto.PushResponse, error) {
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	instance, err := i.GetOrCreateInstance(instanceID)
	if err != nil {
		return &logproto.PushResponse{}, err
	}
	return &logproto.PushResponse{}, instance.Push(ctx, req)
}

func (i *KafkaIngester) Query(req *logproto.QueryPatternsRequest, stream logproto.Pattern_QueryServer) error {
	ctx := stream.Context()
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return err
	}
	instance, err := i.GetOrCreateInstance(instanceID)
	if err != nil {
		return err
	}
	iterator, err := instance.Iterator(ctx, req)
	if err != nil {
		return err
	}
	defer util.LogErrorWithContext(ctx, "closing iterator", iterator.Close)
	return sendPatternSample(ctx, iterator, stream)
}

func (i *KafkaIngester) GetOrCreateInstance(instanceID string) (*instance, error) { //nolint:revive
	inst, ok := i.getInstanceByID(instanceID)
	if ok {
		return inst, nil
	}

	i.instancesMtx.Lock()
	defer i.instancesMtx.Unlock()
	inst, ok = i.instances[instanceID]
	if !ok {
		var err error
		aggregationMetrics := aggregation.NewMetrics(i.registerer)

		var metricWriter aggregation.EntryWriter
		aggCfg := i.cfg.MetricAggregation
		if i.limits.MetricAggregationEnabled(instanceID) {
			metricWriter, err = aggregation.NewPush(
				aggCfg.LokiAddr,
				instanceID,
				aggCfg.WriteTimeout,
				aggCfg.PushPeriod,
				aggCfg.HTTPClientConfig,
				aggCfg.BasicAuth.Username,
				string(aggCfg.BasicAuth.Password),
				aggCfg.UseTLS,
				&aggCfg.BackoffConfig,
				log.With(i.logger, "writer", "metric-aggregation"),
				aggregationMetrics,
			)
			if err != nil {
				return nil, err
			}
		}

		var patternWriter aggregation.EntryWriter
		patternCfg := i.cfg.PatternPersistence
		if i.limits.PatternPersistenceEnabled(instanceID) {
			patternWriter, err = aggregation.NewPush(
				patternCfg.LokiAddr,
				instanceID,
				patternCfg.WriteTimeout,
				patternCfg.PushPeriod,
				patternCfg.HTTPClientConfig,
				patternCfg.BasicAuth.Username,
				string(patternCfg.BasicAuth.Password),
				patternCfg.UseTLS,
				&patternCfg.BackoffConfig,
				log.With(i.logger, "writer", "pattern"),
				aggregationMetrics,
			)
			if err != nil {
				return nil, err
			}
		}

		inst, err = newInstance(
			instanceID,
			i.logger,
			i.metrics,
			i.drainCfg,
			i.limits,
			i.ringClient,
			i.lifecycler.ID,
			metricWriter,
			patternWriter,
			aggregationMetrics,
			i.cfg.VolumeThreshold,
		)
		if err != nil {
			return nil, err
		}
		i.instances[instanceID] = inst
	}
	return inst, nil
}

func (i *KafkaIngester) getInstanceByID(id string) (*instance, bool) {
	i.instancesMtx.RLock()
	defer i.instancesMtx.RUnlock()

	inst, ok := i.instances[id]
	return inst, ok
}

func (i *KafkaIngester) getInstances() []*instance {
	i.instancesMtx.RLock()
	defer i.instancesMtx.RUnlock()

	instances := make([]*instance, 0, len(i.instances))
	for _, instance := range i.instances {
		instances = append(instances, instance)
	}
	return instances
}

func (i *KafkaIngester) stopWriters() {
	instances := i.getInstances()

	for _, instance := range instances {
		if instance.metricWriter != nil {
			instance.metricWriter.Stop()
		}
		if instance.patternWriter != nil {
			instance.patternWriter.Stop()
		}
	}
}

// flushPatterns flushes all patterns from all instances on shutdown.
func (i *KafkaIngester) flushPatterns() {
	level.Info(i.logger).Log("msg", "flushing patterns on shutdown")
	instances := i.getInstances()

	for _, instance := range instances {
		if i.limits.PatternPersistenceEnabled(instance.instanceID) {
			instance.flushPatterns()
		}
	}
}

func (i *KafkaIngester) downsampleMetrics(ts model.Time) {
	instances := i.getInstances()

	for _, instance := range instances {
		if i.limits.MetricAggregationEnabled(instance.instanceID) {
			instance.Downsample(ts)
		}
	}
}

// waitForPartitions polls partitionRing until it reports at least one
// partition or the deadline expires.
func waitForPartitions(ctx context.Context, r ring.PartitionRingReader, timeout time.Duration, logger log.Logger) error {
	if r.PartitionRing().PartitionsCount() > 0 {
		return nil
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()

	_ = level.Info(logger).Log("msg", "waiting for partition ring to be populated", "timeout", timeout)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("partition ring did not become populated within %s "+
				"(check ring.key, ring.memberlist.cluster_label, and ring.memberlist.join_members)", timeout)
		case <-tick.C:
			if c := r.PartitionRing().PartitionsCount(); c > 0 {
				_ = level.Info(logger).Log("msg", "partition ring populated", "partitions", c)
				return nil
			}
		}
	}
}
