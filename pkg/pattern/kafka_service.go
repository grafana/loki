package pattern

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"go.uber.org/atomic"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/instrument"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/plain"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/kafka/partitionring/consumer"
	"github.com/grafana/loki/v3/pkg/kafkav2"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
)

const (
	reasonShutdown    = "shutdown"
	reasonPartialDisk = "partial_disk"
	reasonMaxAge      = "max_age"
	reasonIdleTimeout = "idle_timeout"
	reasonNone        = "none"

	shutdownTimeout = 5 * time.Minute
)

// A processor receives records and builds data objects from them.
// flushRequest is used to send a flush request to the processor's Run loop.
type flushRequest struct {
	done chan<- error
}

// kafkaMetrics contains the metrics for [KafkaService].
type kafkaMetrics struct {
	ingesterAppends       *prometheus.CounterVec
	ingesterMetricAppends *prometheus.CounterVec
	sendDuration          *instrument.HistogramCollector
}

type KafkaService struct {
	services.Service
	cfg        Config
	limits     Limits
	tenantCfgs *runtime.TenantConfigs
	logger     log.Logger
	ringClient RingClient
	wg         *sync.WaitGroup
	decoder    *kafka.Decoder
	consumer   *kafkav2.SinglePartitionConsumer
	records    chan *kgo.Record

	flushQueue chan clientRequest
	// flushRequests is used to safely trigger a flush from outside the Run loop.
	flushRequests chan flushRequest

	// builderMtx guards activeBuilder, lastConsumedOffsets, and pendingFlush.
	// It serialises record processing (processRecordBatch) and builder swaps
	// (swapBuilder) on the poll goroutine with shutdown (stopping) on the
	// dskit service goroutine.
	builderMtx sync.Mutex

	metrics *kafkaMetrics

	// ownedPartitions is the current consumer-group partition assignment for
	// this member. Updated only from kgo's rebalance callbacks
	// (onPartitionsAssigned / onPartitionsLostOrRevoked), which kgo
	// serialises on its poll goroutine; read lock-free from the running
	// loop and flush goroutine via snapshotOwnedPartitions. atomic.Pointer
	// gives us a consistent snapshot without locking the hot path.
	ownedPartitions atomic.Pointer[[]int32]

	partitionRing ring.PartitionRingReader

	pendingFlush chan struct{} // closed by flush goroutine on completion; nil when idle
}

// rawRecord holds a Kafka record's raw bytes and metadata, pending decode.
type rawRecord struct {
	value     []byte
	timestamp time.Time
	partition int32
	offset    int64
	tenantID  string
}

// newKafkaMetrics returns new kafkaMetrics.
func newKafkaMetrics(reg prometheus.Registerer) *kafkaMetrics {
	m := kafkaMetrics{
		ingesterAppends: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "pattern_ingester_kafka_appends_total",
			Help: "The total number of batch appends sent to pattern ingesters.",
		}, []string{"ingester", "status"}),
		ingesterMetricAppends: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "pattern_ingester_kafka_metric_appends_total",
			Help: "The total number of metric only batch appends sent to pattern ingesters. These requests will not be processed for patterns.",
		}, []string{"status"}),
		sendDuration: instrument.NewHistogramCollector(
			promauto.With(reg).NewHistogramVec(
				prometheus.HistogramOpts{
					Name:    "pattern_ingester_kafka_send_duration_seconds",
					Help:    "Time spent sending batches from the tee to the pattern ingester",
					Buckets: prometheus.DefBuckets,
				}, instrument.HistogramCollectorBuckets,
			),
		),
	}
	return &m
}

func NewKafkaService(
	cfg Config,
	limits Limits,
	ringClient RingClient,
	tenantCfgs *runtime.TenantConfigs,
	metricsNamespace string,
	reg prometheus.Registerer,
	logger log.Logger,
	kafkaCfg kafka.Config,
) (*KafkaService, error) {
	reg = prometheus.WrapRegistererWithPrefix(metricsNamespace+"_", reg)
	cfg.TeeConfig.KafkaConfig = kafkaCfg
	s := &KafkaService{
		logger:     log.With(logger, "component", "pattern-ingester-kafka"),
		cfg:        cfg,
		limits:     limits,
		tenantCfgs: tenantCfgs,
		ringClient: ringClient,
		wg:         &sync.WaitGroup{},
		flushQueue: make(chan clientRequest, cfg.TeeConfig.FlushQueueSize),
		metrics:    newKafkaMetrics(reg),
	}

	decoder, err := kafka.NewDecoder()
	if err != nil {
		return nil, fmt.Errorf("create kafka decoder: %w", err)
	}
	s.decoder = decoder
	readerCfg := kafkaCfg
	readerCfg.Topic = kafkaCfg.Topic
	consumerGroup := defaultPatternConsumerGroup
	if readerCfg.ConsumerGroup != "" {
		consumerGroup = readerCfg.ConsumerGroup
	}
	readerCfg.ConsumerGroup = consumerGroup
	readerClient, err := client.NewReaderClient("loki.dataobj_consumer", readerCfg, logger, reg)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for data topic: %w", err)
	}
	// need to set up a partition ring before starting the consumer, so we can have a partition ID to use
	records := make(chan *kgo.Record)
	s.records = records
	var partitionID int32 = 0
	s.consumer = kafkav2.NewSinglePartitionConsumer(
		readerClient,
		kafkaCfg.Topic,
		partitionID,
		kafkav2.OffsetStart, // We fetch the real initial offset before starting the service.
		records,
		logger,
		prometheus.WrapRegistererWithPrefix("loki_dataobj_consumer_", reg),
	)
	s.flushRequests = make(chan flushRequest, 1)
	// Initialise to an empty slice so snapshotOwnedPartitions never returns
	// nil — callers iterate the result without a nil check.
	empty := []int32{}
	s.ownedPartitions.Store(&empty)
	s.partitionRing = (NewPartitionRingWatcher(cfg.TeeConfig.RingConfig, logger, reg))
	s.Service = services.NewBasicService(s.starting, s.running, nil).WithName("pattern-ingester-kafka")

	return s, nil
}

func (s *KafkaService) starting(ctx context.Context) error {
	// Block until the partition ring has at least one partition before
	// we transition to Running. Without this gate, the first JoinGroup
	// could land while the ring is still empty and the active-sticky balancer
	// would refuse to assign anything silently stranding the consumer group.
	if err := waitForPartitions(ctx, s.partitionRing, s.cfg.TeeConfig.RingConfig.StartupTimeout, s.logger); err != nil {
		return fmt.Errorf("partition ring not ready: %w", err)
	}
	s.wg.Add(1)
	if err := services.StartAndAwaitRunning(ctx, s.consumer); err != nil {
		return fmt.Errorf("failed to start consumer: %w", err)
	}

	// Start all batchSenders. We don't use the Run() context here, because we
	// want the senders to finish sending any currently in-flight data and the
	// remining batches in the queue before the TeeService fully stops.
	//
	// Still, we have a maximum amount of time we will wait after the TeeService
	// is stopped, see cfg.StopFlushTimeout below.
	senderCtx, senderCancel := context.WithCancel(context.Background())

	sendersWg := &sync.WaitGroup{}
	sendersWg.Add(s.cfg.TeeConfig.FlushWorkerCount)
	for i := 0; i < s.cfg.TeeConfig.FlushWorkerCount; i++ {
		go func() {
			s.batchSender(senderCtx)
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

		// The senders euther stop normally in the allotted time, or we hit the
		// timeout and cancel thir context. In either case, we wait for them to
		// finish before we consider the service to be done.
		select {
		case <-time.After(s.cfg.TeeConfig.StopFlushTimeout):
			senderCancel() // Cancel any remaining senders
			<-sendersDone  // Wait for them to be done
		case <-sendersDone:
		}
		s.wg.Done()
	}()

	return nil
}

func (s *KafkaService) running(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

func (s *KafkaService) WaitUntilDone() {
	s.wg.Wait()
}

func (s *KafkaService) batchSender(ctx context.Context) error {
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
			s.sendBatch(ctx, req)
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
					s.sendBatch(ctx, req)
				default:
					break drain
				}
			}
			req.done <- nil
		}
	}
}

func (s *KafkaService) ingesterForTenant(tenant string, stream logproto.Stream) (string, error) {
	var descs [1]ring.InstanceDesc
	replicationSet, err := s.ringClient.Ring().Get(uint32(stream.Hash), ring.WriteNoExtend, descs[:0], nil, nil)
	if err != nil {
		return "", err
	}
	return replicationSet.Instances[0].Addr, nil
}

func (s *KafkaService) sendBatch(ctx context.Context, clientRequest clientRequest) {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.ConnectionTimeout)
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
			s.metrics.sendDuration,
			instrument.ErrorCode,
			func(ctx context.Context) error {
				sp := spanlogger.FromContext(ctx, s.logger)
				client, err := s.ringClient.GetClientFor(clientRequest.ingesterAddr)
				if err != nil {
					return err
				}
				ctx, cancel := context.WithTimeout(
					user.InjectOrgID(ctx, clientRequest.tenant),
					s.cfg.ClientConfig.RemoteTimeout,
				)

				// First try to send the request to the correct pattern ingester
				defer cancel()
				_, err = client.(logproto.PatternClient).Push(ctx, req)
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
}

// onPartitionsAssigned merges the coordinator's assignment delta into the
// owned-partition set. With CooperativeStickyBalancer, kgo delivers only
// the partitions *newly* assigned in this rebalance, so we must merge
// rather than replace — otherwise partitions retained across the
// rebalance would be silently dropped.
func (s *KafkaService) onPartitionsAssigned(_ context.Context, _ *kgo.Client, assigned map[string][]int32) {
	parts, ok := assigned[s.cfg.TeeConfig.KafkaConfig.Topic]
	if !ok || len(parts) == 0 {
		return
	}
	current := s.snapshotOwnedPartitions()
	set := make(map[int32]struct{}, len(current)+len(parts))
	for _, p := range current {
		set[p] = struct{}{}
	}
	for _, p := range parts {
		set[p] = struct{}{}
	}
	next := setToSortedSlice(set)
	s.storeOwnedPartitions(next)

	level.Info(s.logger).Log(
		"msg", "partitions assigned",
		"added", fmt.Sprintf("%v", parts),
		"owned", fmt.Sprintf("%v", next),
		"owned_count", len(next),
	)
}

// onPartitionsLostOrRevoked removes the lost/revoked partitions from the
// owned set and deletes the matching per-partition Prometheus label series
// for consumptionLagSeconds and bytesReceivedTotal. Without the label
// deletion, dashboards would keep reporting frozen lag/bytes values for
// partitions we no longer consume — exactly the silent-producer
// false-positive lag we saw in ops-eu-south-0/loki-ops-002.
//
// Wired to both kgo.OnPartitionsRevoked (cooperative rebalance: we are
// asked to give partitions up cleanly) and kgo.OnPartitionsLost (broker
// took them away without a clean revoke). The cleanup is identical in
// both cases, so we share a single callback.
func (s *KafkaService) onPartitionsLostOrRevoked(_ context.Context, _ *kgo.Client, lost map[string][]int32) {
	parts, ok := lost[s.cfg.TeeConfig.KafkaConfig.Topic]
	if !ok || len(parts) == 0 {
		return
	}
	current := s.snapshotOwnedPartitions()
	set := make(map[int32]struct{}, len(current))
	for _, p := range current {
		set[p] = struct{}{}
	}
	for _, p := range parts {
		delete(set, p)
	}
	next := setToSortedSlice(set)
	s.storeOwnedPartitions(next)

	level.Info(s.logger).Log(
		"msg", "partitions revoked",
		"removed", fmt.Sprintf("%v", parts),
		"owned", fmt.Sprintf("%v", next),
		"owned_count", len(next),
	)
}

// snapshotOwnedPartitions returns the current consumer-group assignment for
// this member as a sorted, immutable slice. Callers must not mutate the
// returned slice; the rebalance callbacks replace the underlying pointer
// rather than editing in place.
func (s *KafkaService) snapshotOwnedPartitions() []int32 {
	p := s.ownedPartitions.Load()
	if p == nil {
		return nil
	}
	return *p
}

// storeOwnedPartitions atomically replaces the assignment snapshot. Only
// called from the rebalance callbacks (serialised on the kgo poll goroutine).
func (s *KafkaService) storeOwnedPartitions(parts []int32) {
	s.ownedPartitions.Store(&parts)
}

// setToSortedSlice returns the keys of set as an ascending-sorted slice.
// Used to keep the owned-partition snapshot canonical so callers (and
// tests) can rely on a stable order.
func setToSortedSlice(set map[int32]struct{}) []int32 {
	out := make([]int32, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
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

// createKafkaClient builds the consumer-group Kafka client.
//
// Key design decisions:
//
//   - kgo.ConsumerGroup + kgo.ConsumeTopics: partitions are assigned by the
//     group coordinator instead of derived from the pod's StatefulSet ordinal.
//     Scaling the replica count is safe — partition ownership rebalances
//     automatically.
//
//   - kgo.InstanceID(cfg.InstanceID): static membership. A pod that restarts
//     within cfg.KafkaSessionTimeout rejoins with the same identity and keeps
//     its partitions, so rolling deploys and crash-restart loops do not cause
//     rebalances.
//
//   - kgo.Balancers(newCooperativeActiveStickyBalancer(...)): partition-ring-
//     aware balancer that balances ACTIVE partitions (per the partition ring)
//     evenly across members using cooperative-sticky semantics, while still
//     assigning inactive partitions round-robin so they are monitored and
//     can activate quickly. Cooperative-sticky on the active set means only
//     the partitions that must move are revoked; the rest stay put, avoiding
//     stop-the-world rebalances.
//
//   - kgo.DisableAutoCommit: offsets are committed by the flush goroutine
//     *after* the corresponding .lidx files are uploaded. This is what
//     enforces at-least-once. kgo's own auto-commit would commit positions
//     of records that have only been fetched, not yet uploaded.
//
//   - kgo.ConsumeResetOffset(AtStart): for partitions with no committed
//     offset yet, start at the earliest available record rather than the
//     end. Without this, a fresh consumer group would silently skip every
//     record produced before the first member joined.
func (s *KafkaService) createKafkaClient() (*kgo.Client, error) {
	address := s.cfg.TeeConfig.KafkaConfig.ReaderConfig.Address
	if address == "" {
		address = s.cfg.TeeConfig.KafkaConfig.Address
	}

	seedBrokers := strings.Split(address, ",")
	for i := range seedBrokers {
		seedBrokers[i] = strings.TrimSpace(seedBrokers[i])
	}

	clientID := s.cfg.TeeConfig.KafkaConfig.ReaderConfig.ClientID
	if clientID == "" {
		clientID = s.cfg.TeeConfig.KafkaConfig.ClientID
	}
	if clientID == "" {
		clientID = "pattern-ingester"
	}

	opts := []kgo.Opt{
		kgo.WithLogger(newKgoLogger(log.With(s.logger, "component", "kgo"))),
		kgo.SeedBrokers(seedBrokers...),
		kgo.ClientID(clientID),
		kgo.DialTimeout(s.cfg.TeeConfig.KafkaConfig.DialTimeout),
		kgo.MetadataMinAge(10 * time.Second),
		kgo.MetadataMaxAge(10 * time.Second),
		kgo.FetchMinBytes(1 * 1024 * 1024),
		kgo.FetchMaxBytes(100 * 1024 * 1024),
		kgo.FetchMaxPartitionBytes(50 * 1024 * 1024),
		kgo.FetchMaxWait(1 * time.Second),

		// Consumer-group membership.
		kgo.ConsumerGroup(s.cfg.TeeConfig.KafkaConfig.ConsumerGroup),
		kgo.ConsumeTopics(s.cfg.TeeConfig.KafkaConfig.Topic),
		kgo.SessionTimeout(s.cfg.TeeConfig.KafkaSessionTimeout),
		kgo.DisableAutoCommit(),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.OnPartitionsAssigned(s.onPartitionsAssigned),
		kgo.OnPartitionsRevoked(s.onPartitionsLostOrRevoked),
		kgo.OnPartitionsLost(s.onPartitionsLostOrRevoked),
	}

	// The partition ring is a required dependency: it tells the balancer
	// which partitions are active and must be balanced evenly. There is no
	// fallback — newService wires it unconditionally.
	balancer := consumer.NewCooperativeActiveStickyBalancer(s.partitionRing, s.logger)
	opts = append(opts, kgo.Balancers(balancer))

	// Static membership is what keeps pod restarts from triggering rebalances.
	// kfake rejects InstanceID, so unit tests opt out via the unexported flag.
	if !s.cfg.TeeConfig.disableStaticMembership {
		opts = append(opts, kgo.InstanceID(s.cfg.TeeConfig.KafkaInstanceId))
	}

	if s.cfg.TeeConfig.KafkaConfig.SASLUsername != "" && s.cfg.TeeConfig.KafkaConfig.SASLPassword.String() != "" {
		level.Info(s.logger).Log("msg", "enabling SASL PLAIN authentication", "username", s.cfg.TeeConfig.KafkaConfig.SASLUsername)
		opts = append(opts, kgo.SASL(plain.Plain(func(ctx context.Context) (plain.Auth, error) {
			return plain.Auth{
				User: s.cfg.TeeConfig.KafkaConfig.SASLUsername,
				Pass: s.cfg.TeeConfig.KafkaConfig.SASLPassword.String(),
			}, nil
		})))
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka client: %w", err)
	}

	level.Info(s.logger).Log(
		"msg", "Kafka consumer-group client initialized",
		"brokers", strings.Join(seedBrokers, ","),
		"topic", s.cfg.TeeConfig.KafkaConfig.Topic,
		"consumer_group", s.cfg.TeeConfig.KafkaConfig.ConsumerGroup,
		"instance_id", s.cfg.TeeConfig.KafkaInstanceId,
		"session_timeout", s.cfg.TeeConfig.KafkaSessionTimeout,
	)
	return client, nil
}

// hasPendingFlush reports whether a background flush is still in progress.
// If the flush has completed, it clears pendingFlush and returns false.
// Caller must hold builderMtx.
func (s *KafkaService) hasPendingFlush() bool {
	if s.pendingFlush == nil {
		return false
	}
	select {
	case <-s.pendingFlush:
		s.pendingFlush = nil
		return false
	default:
		return true
	}
}

// kgoLogger adapts go-kit/log to franz-go's kgo.Logger interface so that
// internal kafka client errors (e.g. retryable fetch failures) are visible.
type kgoLogger struct {
	logger log.Logger
}

func newKgoLogger(logger log.Logger) *kgoLogger {
	return &kgoLogger{logger: logger}
}

func (l *kgoLogger) Level() kgo.LogLevel { return kgo.LogLevelWarn }

func (l *kgoLogger) Log(lvl kgo.LogLevel, msg string, keyvals ...any) {
	merged := make([]any, 0, 2+len(keyvals))
	merged = append(merged, "msg", msg)
	merged = append(merged, keyvals...)
	switch lvl {
	case kgo.LogLevelError:
		level.Error(l.logger).Log(merged...)
	case kgo.LogLevelWarn:
		level.Warn(l.logger).Log(merged...)
	case kgo.LogLevelInfo:
		level.Info(l.logger).Log(merged...)
	case kgo.LogLevelDebug:
		level.Debug(l.logger).Log(merged...)
	}
}
