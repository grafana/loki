package consumer

import (
	"context"

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

type Service struct {
	services.Service
	cfg             Config
	metastoreEvents *kgo.Client
	logger          log.Logger
	reg             prometheus.Registerer
}

func New(kafkaCfg kafka.Config, cfg Config, _ metastore.Config, _ objstore.Bucket, _ scratch.Store, _ string, _ ring.PartitionRingReader, reg prometheus.Registerer, logger log.Logger) *Service {
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
	s.Service = services.NewBasicService(nil, s.running, s.stopping)
	return s
}

// running implements the Service interface's running method.
func (s *Service) running(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// stopping implements the Service interface's stopping method.
func (s *Service) stopping(failureCase error) error {
	level.Info(s.logger).Log("msg", "stopping")
	s.metastoreEvents.Close()
	level.Info(s.logger).Log("msg", "stopped")
	return failureCase
}
