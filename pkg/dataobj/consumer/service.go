package consumer

import (
	"context"
	"time"

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
	kafka_consumer "github.com/grafana/loki/v3/pkg/kafka/partitionring/consumer"
	"github.com/grafana/loki/v3/pkg/scratch"
)

type Service struct {
	services.Service
	cfg                 Config
	consumer            *consumer
	consumerClient      *kafka_consumer.Client
	partitionLifecycler *partitionLifecycler
	metastoreEvents     *kgo.Client
	logger              log.Logger
	reg                 prometheus.Registerer
}

func New(kafkaCfg kafka.Config, cfg Config, mCfg metastore.Config, bucket objstore.Bucket, scratchStore scratch.Store, instanceID string, partitionRing ring.PartitionRingReader, reg prometheus.Registerer, logger log.Logger) *Service {
	logger = log.With(logger, "component", "dataobj-consumer")

	s := &Service{
		logger: logger,
		cfg:    cfg,
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
		level.Error(logger).Log("msg", "failed to create producer", "err", err)
		return nil
	}
	s.metastoreEvents = metastoreEvents

	// Set up the Kafka client and partition processors which consume records.
	// We use a factory to create a processor per partition as and when
	// partitions are assigned. This keeps the dependencies for processors
	// out of the lifecycler which makes it easier to test.
	processorFactory := newPartitionProcessorFactory(
		cfg,
		mCfg,
		metastoreEvents,
		bucket,
		scratchStore,
		logger,
		reg,
	)
	processorLifecycler := newPartitionProcessorLifecycler(
		processorFactory,
		logger,
		reg,
	)
	partitionLifecycler := newPartitionLifecycler(processorLifecycler, logger)
	// The client calls the lifecycler whenever partitions are assigned or
	// revoked. This is how we register and unregister processors.
	consumerClient, err := kafka_consumer.NewGroupClient(
		kafkaCfg,
		partitionRing,
		"dataobj-consumer",
		logger,
		reg,
		kgo.InstanceID(instanceID),
		kgo.SessionTimeout(3*time.Minute),
		kgo.RebalanceTimeout(5*time.Minute),
		kgo.OnPartitionsAssigned(partitionLifecycler.Assign),
		kgo.OnPartitionsRevoked(partitionLifecycler.Revoke),
	)
	if err != nil {
		level.Error(logger).Log("msg", "failed to create consumer", "err", err)
		return nil
	}
	s.consumerClient = consumerClient
	// The consumer is what polls records from Kafka. It is responsible for
	// fetching records for each assigned partition and passing them to their
	// respective partition processor. We guarantee that the consumer will
	// not fetch records for partitions before the lifecycler has registered
	// its processor because [kgo.onPartitionsAssigned] guarantees this
	// callback returns before the client starts polling records for any
	// newly assigned partitions.
	consumer := newConsumer(consumerClient.Client, logger)
	processorLifecycler.AddListener(consumer)

	s.partitionLifecycler = partitionLifecycler
	s.consumer = consumer
	s.Service = services.NewBasicService(nil, s.running, s.stopping)
	return s
}

// running implements the Service interface's running method.
func (s *Service) running(ctx context.Context) error {
	return s.consumer.Run(ctx)
}

// stopping implements the Service interface's stopping method.
func (s *Service) stopping(failureCase error) error {
	level.Info(s.logger).Log("msg", "stopping")
	s.partitionLifecycler.Stop(context.TODO())
	s.consumerClient.Close()
	s.metastoreEvents.Close()
	level.Info(s.logger).Log("msg", "stopped")
	return failureCase
}
