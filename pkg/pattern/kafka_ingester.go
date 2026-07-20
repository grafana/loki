package pattern

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/kafka/partitionring"
	"github.com/grafana/loki/v3/pkg/kafkav2"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/pattern/aggregation"
	"github.com/grafana/loki/v3/pkg/pattern/drain"
	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/util"
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
	loopQuit        chan struct{}

	metrics  *ingesterMetrics
	drainCfg *drain.Config
	records  chan *kgo.Record
	consumer *kafkav2.GroupConsumer

	decoder       *kafka.Decoder
	flushRequests chan flushRequest
	tenantCfgs    *runtime.TenantConfigs

	ingestPartitionID       int32
	partitionRingLifecycler *ring.PartitionInstanceLifecycler
}

func NewKafka(
	cfg Config,
	limits Limits,
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
		logger:      log.With(logger, "component", "pattern-ingester"),
		registerer:  registerer,
		metrics:     metrics,
		instances:   make(map[string]*instance),
		flushQueues: make([]*util.PriorityQueue, cfg.ConcurrentFlushes),
		loopQuit:    make(chan struct{}),
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

	records := make(chan *kgo.Record)
	i.records = records
	i.consumer = kafkav2.NewGroupConsumer(
		readerClient,
		kafkaCfg.Topic,
		records,
		logger,
		prometheus.WrapRegistererWithPrefix("loki_pattern_consumer_", registerer),
	)

	i.Service = services.NewBasicService(i.starting, i.running, i.stopping)
	i.lifecycler, err = ring.NewLifecycler(cfg.LifecyclerConfig, i, "pattern-ingester", "pattern-ring", true, i.logger, registerer)
	if err != nil {
		return nil, err
	}

	i.lifecyclerWatcher = services.NewFailureWatcher()
	i.lifecyclerWatcher.WatchService(i.lifecycler)

	i.ingestPartitionID, err = partitionring.ExtractPartitionID(cfg.LifecyclerConfig.ID)

	partitionRingKV := cfg.KafkaPartitionRingConfig.KVStore.Mock
	if partitionRingKV == nil {
		partitionRingKV, err = kv.NewClient(cfg.KafkaPartitionRingConfig.KVStore, ring.GetPartitionRingCodec(), kv.RegistererWithKVName(registerer, PartitionRingName+"-lifecycler"), logger)
		if err != nil {
			return nil, fmt.Errorf("creating KV store for pattern ingester partition ring: %w", err)
		}
	}
	i.partitionRingLifecycler = ring.NewPartitionInstanceLifecycler(
		i.cfg.KafkaPartitionRingConfig.ToLifecyclerConfig(i.ingestPartitionID, cfg.LifecyclerConfig.ID),
		PartitionRingName,
		PartitionRingKey,
		partitionRingKV,
		logger,
		prometheus.WrapRegistererWithPrefix("loki_", registerer))

	if err != nil {
		return nil, err
	}
	i.lifecyclerWatcher.WatchService(i.partitionRingLifecycler)

	return i, nil
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
	if i.partitionRingLifecycler != nil {
		if err := services.StartAndAwaitRunning(ctx, i.partitionRingLifecycler); err != nil {
			return fmt.Errorf("failed to start partition ring lifecycler: %w", err)
		}
	}

	if err := services.StartAndAwaitRunning(ctx, i.consumer); err != nil {
		return fmt.Errorf("start kafka consumer: %w", err)
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
			if err := i.sender(senderCtx); err != nil {
				level.Error(i.logger).Log("msg", "kafka sender exited with error", "err", err)
			}
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
	}()
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

	return serviceError
}

func (i *KafkaIngester) stopping(_ error) error {
	if err := services.StopAndAwaitTerminated(context.Background(), i.consumer); err != nil {
		level.Warn(i.logger).Log("msg", "failed to stop kafka consumer", "err", err)
	}
	err := services.StopAndAwaitTerminated(context.Background(), i.lifecycler)
	for _, flushQueue := range i.flushQueues {
		if flushQueue != nil {
			flushQueue.Close()
		}
	}
	i.flushQueuesDone.Wait()

	// Flush all patterns before stopping writers to ensure patterns are persisted
	i.flushPatterns()

	i.stopWriters()
	return err
}

func (i *KafkaIngester) sender(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			level.Info(i.logger).Log("msg", "context canceled")
			// We don't return ctx.Err() here as it manifests as a service failure
			// when stopping the service.
			return nil
		case rec, ok := <-i.records:
			if rec.Partition != i.ingestPartitionID {
				continue
			}
			if !ok {
				level.Info(i.logger).Log("msg", "channel closed")
				return nil
			}
			// tenant := string(rec.Key)
			stream, err := i.decoder.DecodeWithoutLabels(rec.Value)
			if err != nil {
				// This is an unrecoverable error and no amount of retries will fix it.
				return fmt.Errorf("failed to decode stream: %w", err)
			}
			_, err = i.Push(ctx, &logproto.PushRequest{Streams: []logproto.Stream{stream}})
			if err != nil {
				return fmt.Errorf("failed to push stream: %w", err)
			} else {
				i.metrics.ingesterAppends.WithLabelValues(i.lifecycler.Addr, "success").Inc()
				i.metrics.ingesterMetricAppends.WithLabelValues("success").Inc()
			}
		case req := <-i.flushRequests:
			// Drain any records that are already in the channel before flushing
			// so that all pending data is included in the flush.
		drain:
			for {
				select {
				case rec, ok := <-i.records:
					if !ok {
						break drain
					}
					// tenant := string(rec.Key)
					stream, err := i.decoder.DecodeWithoutLabels(rec.Value)
					if err != nil {
						level.Error(i.logger).Log("msg", "failed to process record during flush drain", "err", err)
						continue
					}
					_, err = i.Push(ctx, &logproto.PushRequest{Streams: []logproto.Stream{stream}})
					if err != nil {
						return fmt.Errorf("failed to push stream: %w", err)
					} else {
						i.metrics.ingesterAppends.WithLabelValues(i.lifecycler.Addr, "success").Inc()
						i.metrics.ingesterMetricAppends.WithLabelValues("success").Inc()
					}

				default:
					break drain
				}
			}
			req.done <- nil
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

func (i *KafkaIngester) Flush() {
	i.flush(true)
}

func (i *KafkaIngester) flush(mayRemoveStreams bool) {
	i.sweepUsers(true, mayRemoveStreams)

	// Close the flush queues, to unblock waiting workers.
	for _, flushQueue := range i.flushQueues {
		flushQueue.Close()
	}

	i.flushQueuesDone.Wait()
	level.Debug(i.logger).Log("msg", "flush queues have drained")
}

// sweepUsers periodically schedules series for flushing and garbage collects users with no series
func (i *KafkaIngester) sweepUsers(immediate, mayRemoveStreams bool) {
	instances := i.getInstances()

	for _, instance := range instances {
		i.sweepInstance(instance, immediate, mayRemoveStreams)
	}
}

func (i *KafkaIngester) sweepInstance(instance *instance, _, mayRemoveStreams bool) {
	level.Debug(i.logger).Log("msg", "sweeping instance", "instance", instance.instanceID)
	_ = instance.streams.ForEach(func(s *stream) (bool, error) {
		if mayRemoveStreams {
			instance.streams.WithLock(func() {
				if s.prune(i.cfg.RetainFor) {
					instance.removeStream(s)
				}
			})
		}
		return true, nil
	})
}
