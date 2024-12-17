package usage

import (
	"context"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
)

type service struct {
	client *kgo.Client
}

func newService(kafkaCfg kafka.Config, consumerGroup string, logger log.Logger, registrar prometheus.Registerer) (*service, error) {
	windowsSize := 1 * time.Minute
	kprom := client.NewReaderClientMetrics("usage-consumer", registrar)
	client, err := client.NewReaderClient(kafkaCfg, kprom, logger,
		kgo.ConsumerGroup(consumerGroup),
		kgo.ConsumeTopics("ingest.usage"),
		kgo.ConsumeResetOffset(kgo.NewOffset().AfterMilli(time.Now().Add(-windowsSize).UnixMilli())),
	)
	if err != nil {
		return nil, err
	}
	return &service{client: client}, nil
}

func (s *service) Consume(ctx context.Context, records []*kgo.Record) error {
	return nil
}
