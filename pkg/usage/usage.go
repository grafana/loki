package usage

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	topic      = "ingest.usage"
	windowSize = 1 * time.Minute
)

type service struct {
	client *kgo.Client
}

func newService(kafkaCfg kafka.Config, consumerGroup string, logger log.Logger, registrar prometheus.Registerer) (*service, error) {
	kprom := client.NewReaderClientMetrics("usage-consumer", registrar)
	client, err := client.NewReaderClient(kafkaCfg, kprom, logger,
		kgo.ConsumerGroup(consumerGroup),
		// kgo.Balancers(balancers ...kgo.GroupBalancer)
		kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AfterMilli(time.Now().Add(-windowSize).UnixMilli())),
		kgo.DisableAutoCommit(),
		kgo.OnPartitionsAssigned(func(ctx context.Context, c *kgo.Client, m map[string][]int32) {
		}),
	)
	if err != nil {
		return nil, err
	}

	return &service{client: client}, nil
}

func (s *service) fetch(ctx context.Context) error {
	fetches := s.client.PollRecords(ctx, -1)
	for _, fetch := range fetches {
		for _, topicFetch := range fetch.Topics {
			for _, partitionFetch := range topicFetch.Partitions {
				for _, record := range partitionFetch.Records {
					fmt.Println(record.Key, record.Value, partitionFetch.Partition, partitionFetch)
				}
			}
		}
	}
	return nil
}

func (s *service) Consume(ctx context.Context, records []*kgo.Record) error {
	return nil
}
