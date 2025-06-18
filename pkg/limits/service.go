package limits

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/kafka/partition"
	"github.com/grafana/loki/v3/pkg/limits/proto"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

const (
	// Ring
	RingKey  = "ingest-limits"
	RingName = "ingest-limits"

	// Readiness check
	maxPartitionReadinessAttempts int32 = 10
)

// Service is a service that manages stream metadata limits.
type Service struct {
	services.Service
	cfg                 Config
	kafkaReader         *kgo.Client
	kafkaWriter         *kgo.Client
	lifecycler          *ring.Lifecycler
	lifecyclerWatcher   *services.FailureWatcher
	limits              Limits
	limitsChecker       *limitsChecker
	partitionManager    *partitionManager
	partitionLifecycler *partitionLifecycler
	consumer            *consumer
	producer            *producer
	usage               *usageStore
	logger              log.Logger

	// Metrics.
	streamEvictionsTotal *prometheus.CounterVec

	// Readiness check
	partitionReadinessAttempts int
	partitionReadinessPassed   bool
	partitionReadinessMtx      sync.Mutex

	// Used for tests.
	clock quartz.Clock
}

// New creates a new IngestLimits service. It initializes the metadata map and sets up a Kafka client
// The client is configured to consume stream metadata from a dedicated topic with the metadata suffix.
func New(cfg Config, limits Limits, logger log.Logger, reg prometheus.Registerer) (*Service, error) {
	var err error
	s := &Service{
		cfg:    cfg,
		limits: limits,
		logger: logger,
		streamEvictionsTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "ingest_limits_stream_evictions_total",
			Help:      "The total number of streams evicted due to age per tenant. This is not a global total, as tenants can be sharded over multiple pods.",
		}, []string{"tenant"}),
		clock: quartz.NewReal(),
	}
	s.partitionManager, err = newPartitionManager(reg)
	if err != nil {
		return nil, fmt.Errorf("failed to create partition manager: %w", err)
	}
	s.usage, err = newUsageStore(cfg.ActiveWindow, cfg.RateWindow, cfg.BucketSize, cfg.NumPartitions, reg)
	if err != nil {
		return nil, fmt.Errorf("failed to create usage store: %w", err)
	}
	// Initialize lifecycler
	s.lifecycler, err = ring.NewLifecycler(cfg.LifecyclerConfig, s, RingName, RingKey, true, logger, reg)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s lifecycler: %w", RingName, err)
	}
	// Watch the lifecycler
	s.lifecyclerWatcher = services.NewFailureWatcher()
	s.lifecyclerWatcher.WatchService(s.lifecycler)
	// Create a copy of the config to modify the topic
	kCfg := cfg.KafkaConfig
	kCfg.Topic = cfg.Topic
	kCfg.AutoCreateTopicEnabled = true
	kCfg.AutoCreateTopicDefaultPartitions = cfg.NumPartitions
	offsetManager, err := partition.NewKafkaOffsetManager(
		kCfg,
		cfg.ConsumerGroup,
		logger,
		prometheus.NewRegistry(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create offset manager: %w", err)
	}
	s.partitionLifecycler = newPartitionLifecycler(
		s.partitionManager,
		offsetManager,
		s.usage,
		cfg.ActiveWindow,
		logger,
	)
	s.kafkaReader, err = client.NewReaderClient("ingest-limits-reader", kCfg, logger, reg,
		kgo.ConsumerGroup(cfg.ConsumerGroup),
		kgo.ConsumeTopics(cfg.Topic),
		kgo.Balancers(kgo.CooperativeStickyBalancer()),
		kgo.ConsumeResetOffset(kgo.NewOffset().AfterMilli(s.clock.Now().Add(-s.cfg.ActiveWindow).UnixMilli())),
		kgo.DisableAutoCommit(),
		kgo.OnPartitionsAssigned(s.partitionLifecycler.Assign),
		kgo.OnPartitionsRevoked(s.partitionLifecycler.Revoke),
		// For now these are hardcoded, but we might choose to make them
		// configurable in the future. We allow up to 100MB to be buffered
		// in total, and up to 1.5MB per partition. If an instance consumes
		// all partitions, then it can buffer 1.5MB * 64 partitions = 96MB.
		// This allows us to run with less than 512MB of allocated heap using
		// a GOMEMLIMIT of 512MiB.
		kgo.FetchMaxBytes(100_000_000),        // 100MB
		kgo.FetchMaxPartitionBytes(1_500_000), // 1.5MB
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka client: %w", err)
	}
	s.kafkaWriter, err = client.NewWriterClient("ingest-limits-writer", kCfg, 20, logger, reg)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka client: %w", err)
	}
	s.consumer = newConsumer(
		s.kafkaReader,
		s.partitionManager,
		s.usage,
		newOffsetReadinessCheck(s.partitionManager),
		cfg.LifecyclerConfig.Zone,
		logger,
		reg,
	)
	s.producer = newProducer(
		s.kafkaWriter,
		kCfg.Topic,
		s.cfg.NumPartitions,
		cfg.LifecyclerConfig.Zone,
		logger,
		reg,
	)
	s.limitsChecker = newLimitsChecker(
		limits,
		s.usage,
		s.producer,
		s.partitionManager,
		s.cfg.NumPartitions,
		logger,
		reg,
	)
	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)
	return s, nil
}

// GetAssignedPartitions implements the [proto.IngestLimitsServer] interface.
func (s *Service) GetAssignedPartitions(
	_ context.Context,
	_ *proto.GetAssignedPartitionsRequest,
) (*proto.GetAssignedPartitionsResponse, error) {
	resp := proto.GetAssignedPartitionsResponse{
		AssignedPartitions: s.partitionManager.ListByState(partitionReady),
	}
	return &resp, nil
}

// ExceedsLimits implements the proto.IngestLimitsServer interface.
func (s *Service) ExceedsLimits(
	ctx context.Context,
	req *proto.ExceedsLimitsRequest,
) (*proto.ExceedsLimitsResponse, error) {
	return s.limitsChecker.ExceedsLimits(ctx, req)
}

func (s *Service) CheckReady(ctx context.Context) error {
	if s.State() != services.Running {
		return fmt.Errorf("service is not running: %v", s.State())
	}
	if err := s.lifecycler.CheckReady(ctx); err != nil {
		return fmt.Errorf("lifecycler not ready: %w", err)
	}
	// Check if the partitions assignment and replay
	// are complete on the service startup only.
	s.partitionReadinessMtx.Lock()
	defer s.partitionReadinessMtx.Unlock()
	if !s.partitionReadinessPassed {
		if len(s.partitionManager.List()) == 0 {
			if s.partitionReadinessAttempts >= maxPartitionReadinessAttempts {
				// If no partition assigment on startup,
				// declare the service initialized.
				s.partitionReadinessPassed = true
				level.Warn(s.logger).Log("msg", "no partitions assigned after max retries, going ready")
				return nil
			}
			s.partitionReadinessAttempts++
			return fmt.Errorf("no partitions assigned, retrying")
		}
		if !s.partitionManager.CheckReady() {
			return fmt.Errorf("partitions not ready")
		}
		// If the partitions are assigned, and the replay is complete,
		// declare the service initialized.
		s.partitionReadinessPassed = true
	}
	return nil
}

// starting implements the Service interface's starting method.
// It is called when the service starts and performs any necessary initialization.
func (s *Service) starting(ctx context.Context) (err error) {
	defer func() {
		if err != nil {
			// if starting() fails for any reason (e.g., context canceled),
			// the lifecycler must be stopped.
			_ = services.StopAndAwaitTerminated(context.Background(), s.lifecycler)
		}
	}()
	// pass new context to lifecycler, so that it doesn't stop automatically when IngestLimits's service context is done
	err = s.lifecycler.StartAsync(context.Background())
	if err != nil {
		return err
	}
	err = s.lifecycler.AwaitRunning(ctx)
	if err != nil {
		return err
	}
	return nil
}

// running implements the Service interface's running method.
// It runs the main service loop that consumes stream metadata from Kafka and manages
// the metadata map. The method also starts a goroutine to periodically evict old streams from the metadata map.
func (s *Service) running(ctx context.Context) error {
	// Start the eviction goroutine
	go s.evictOldStreamsPeriodic(ctx)
	go s.consumer.Run(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		// stop
		case err := <-s.lifecyclerWatcher.Chan():
			return fmt.Errorf("lifecycler failed: %w", err)
		}
	}
}

// evictOldStreamsPeriodic runs a periodic job that evicts old streams.
// It runs two evictions per window size.
func (s *Service) evictOldStreamsPeriodic(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.EvictionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			evicted := s.usage.Evict()
			for tenant, numEvicted := range evicted {
				s.streamEvictionsTotal.WithLabelValues(tenant).Add(float64(numEvicted))
			}
		}
	}
}

// stopping implements the Service interface's stopping method.
// It performs cleanup when the service is stopping, including closing the Kafka client.
// It returns nil for expected termination cases (context cancellation or client closure)
// and returns the original error for other failure cases.
func (s *Service) stopping(failureCase error) error {
	if s.kafkaReader != nil {
		s.kafkaReader.Close()
	}
	if s.kafkaWriter != nil {
		s.kafkaWriter.Close()
	}
	if errors.Is(failureCase, context.Canceled) || errors.Is(failureCase, kgo.ErrClientClosed) {
		return nil
	}
	var allErrs util.MultiError
	allErrs.Add(services.StopAndAwaitTerminated(context.Background(), s.lifecycler))
	allErrs.Add(failureCase)

	return allErrs.Err()
}

// Flush implements ring.FlushTransferer. It transfers state to another ingest limits instance.
func (s *Service) Flush() {}

// TransferOut implements ring.FlushTransferer. It transfers state to another ingest limits instance.
func (s *Service) TransferOut(_ context.Context) error {
	return nil
}
