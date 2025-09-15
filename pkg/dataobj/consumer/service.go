package consumer

import (
	"context"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/scratch"
)

const (
	RingKey  = "dataobj-consumer"
	RingName = "dataobj-consumer"
)

type Service struct {
	services.Service
	cfg             Config
	metastoreEvents *kgo.Client
	lifecycler      *ring.Lifecycler
	watcher         *services.FailureWatcher
	logger          log.Logger
	reg             prometheus.Registerer
}

func New(kafkaCfg kafka.Config, cfg Config, _ metastore.Config, _ objstore.Bucket, _ scratch.Store, _ string, _ ring.PartitionRingReader, reg prometheus.Registerer, logger log.Logger) (*Service, error) {
	logger = log.With(logger, "component", "dataobj-consumer")

	s := &Service{
		cfg:    cfg,
		logger: logger,
		reg:    reg,
	}

	// Set up the Kafka client that produces events for the metastore. This
	// must be done before we can set up the client that consumes records
	// from distributors, as the code that consumes these records from also
	// needs to be able to produce metastore events.
	metastoreEventsCfg := kafkaCfg
	metastoreEventsCfg.Topic = "loki.metastore-events"
	metastoreEventsCfg.AutoCreateTopicDefaultPartitions = 1
	metastoreEvents, err := client.NewWriterClient("loki.metastore-events", metastoreEventsCfg, 50, logger, reg)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for metastore events topic: %w", err)
	}
	s.metastoreEvents = metastoreEvents

	lifecycler, err := ring.NewLifecycler(
		cfg.LifecyclerConfig,
		s,
		RingName,
		RingKey,
		false,
		logger,
		prometheus.WrapRegistererWithPrefix("dataobj-consumer_", reg),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s lifecycler: %w", RingName, err)
	}
	s.lifecycler = lifecycler

	watcher := services.NewFailureWatcher()
	watcher.WatchService(lifecycler)
	s.watcher = watcher

	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)
	return s, nil
}

// starting implements the Service interface's starting method.
func (s *Service) starting(ctx context.Context) (err error) {
	level.Info(s.logger).Log("msg", "starting")
	if err := services.StartAndAwaitRunning(ctx, s.lifecycler); err != nil {
		return fmt.Errorf("failed to start lifecycler: %w", err)
	}
	return nil
}

// running implements the Service interface's running method.
func (s *Service) running(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// stopping implements the Service interface's stopping method.
func (s *Service) stopping(failureCase error) error {
	level.Info(s.logger).Log("msg", "stopping")
	ctx := context.TODO()
	if err := services.StopAndAwaitTerminated(ctx, s.lifecycler); err != nil {
		level.Warn(s.logger).Log("msg", "failed to stop lifecycler", "err", err)
	}
	s.metastoreEvents.Close()
	level.Info(s.logger).Log("msg", "stopped")
	return failureCase
}

// Flush implements the [ring.FlushTransferer] interface.
func (s *Service) Flush() {}

// TransferOut implements the [ring.FlushTransferer] interface.
func (s *Service) TransferOut(_ context.Context) error {
	return nil
}
