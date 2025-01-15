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

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/kafka/partitionring/consumer"
)

const (
	groupName = "dataobj-consumer"
	// For now, assume a single tenant
	tenantID = "foo"
)

type Service struct {
	services.Service

	logger log.Logger
	client *consumer.Client
}

func New(kafkaCfg kafka.Config, builderCfg dataobj.BuilderConfig, instanceID string, bucket objstore.Bucket, partitionRing ring.PartitionRingReader, reg prometheus.Registerer, logger log.Logger) *Service {
	client, err := consumer.NewGroupClient(
		kafkaCfg,
		partitionRing,
		groupName,
		client.NewReaderClientMetrics(groupName, reg),
		logger,
		kgo.InstanceID(instanceID),
		kgo.SessionTimeout(3*time.Minute),
	)
	if err != nil {
		level.Error(logger).Log("msg", "failed to create consumer", "err", err)
		return nil
	}
	s := &Service{
		logger: log.With(logger, "component", groupName),
		client: client,
	}
	s.Service = services.NewBasicService(s.starting, s.run, s.stopping)
	return s
}

func (s *Service) starting(ctx context.Context) (err error) {
	return nil
}

func (s *Service) run(ctx context.Context) error {
	return nil
}

func (s *Service) stopping(failureCase error) error {
	return nil
}
