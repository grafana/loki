package usage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
)

const (
	topic      = "ingest.usage"
	windowSize = 1 * time.Minute
)

type service struct {
	client *kgo.Client
	services.Service

	logger log.Logger
}

type partitionTenants struct {
	tenants map[string]struct{}
}

type partitionStreams struct {
	streams map[string]struct{}
}

func newService(kafkaCfg kafka.Config, consumerGroup string, logger log.Logger, registrar prometheus.Registerer) (*service, error) {
	kprom := client.NewReaderClientMetrics("usage-consumer", registrar)
	client, err := client.NewReaderClient(kafkaCfg, kprom, logger,
		kgo.ConsumerGroup(consumerGroup),
		// kgo.Balancers(balancers ...kgo.GroupBalancer)
		kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AfterMilli(time.Now().Add(-windowSize).UnixMilli())),
		kgo.DisableAutoCommit(),
		// kgo.OnPartitionsAssigned(func(ctx context.Context, c *kgo.Client, m map[string][]int32) {
		// }),
	)
	if err != nil {
		return nil, err
	}
	s := &service{
		client: client,
		logger: logger,
	}
	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)
	return s, nil
}

func (s *service) starting(_ context.Context) error {
	return nil
}

func (s *service) running(ctx context.Context) error {
	for {
		fetches := s.client.PollRecords(ctx, -1)
		for _, fetch := range fetches {
			for _, topicFetch := range fetch.Topics {
				for _, partitionFetch := range topicFetch.Partitions {
					if partitionFetch.Err != nil {
						level.Error(s.logger).Log("msg", "error polling records", "err", partitionFetch.Err)
						return partitionFetch.Err
					}
					for _, record := range partitionFetch.Records {
						fmt.Println(record.Key, record.Value, partitionFetch.Partition, partitionFetch)
					}
				}
			}
		}
	}
}

func (s *service) stopping(failureCase error) error {
	if errors.Is(failureCase, context.Canceled) || errors.Is(failureCase, kgo.ErrClientClosed) {
		return nil
	}
	s.client.Close()
	return failureCase
}
