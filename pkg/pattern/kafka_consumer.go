package pattern

import (
	"context"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/kafkav2"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// pusher is the subset of Ingester used by kafkaConsumerService. It exists
// to keep kafkaConsumerService testable without a full Ingester.
type pusher interface {
	Push(ctx context.Context, req *logproto.PushRequest) (*logproto.PushResponse, error)
}

// kafkaConsumerService group-consumes push requests from a Kafka topic and
// forwards each one to a pusher, committing the offset after each push.
//
// Partition assignment and revocation are handled entirely by the broker
// and franz-go's internal bookkeeping: this service does not register any
// OnPartitionsAssigned/OnPartitionsRevoked/OnPartitionsLost callbacks, so
// there is nothing extra to do when they occur.
type kafkaConsumerService struct {
	services.Service

	pusher   pusher
	client   *kgo.Client
	consumer *kafkav2.GroupConsumer
	decoder  *kafka.Decoder
	records  chan *kgo.Record
	logger   log.Logger
}

func newKafkaConsumerService(
	cfg KafkaConfig,
	p pusher,
	logger log.Logger,
	registerer prometheus.Registerer,
) (*kafkaConsumerService, error) {
	logger = log.With(logger, "component", "pattern-ingester-kafka-consumer")

	decoder, err := kafka.NewDecoder()
	if err != nil {
		return nil, fmt.Errorf("creating kafka decoder: %w", err)
	}

	kafkaClient, err := client.NewReaderClient(
		"pattern-ingester", cfg.KafkaConfig, logger, registerer,
		kgo.ConsumerGroup(cfg.KafkaConfig.ConsumerGroup),
		kgo.ConsumeTopics(cfg.KafkaConfig.Topic),
		// Offsets are committed automatically on franz-go's default 5s
		// interval rather than after every push, to avoid an OffsetCommit
		// round-trip per record. Pattern-ingester state is already
		// best-effort and non-authoritative, so the wider window of
		// reprocessed records on a crash/rebalance is an acceptable trade
		// for the throughput this saves.
	)
	if err != nil {
		return nil, fmt.Errorf("creating pattern ingester kafka consumer client: %w", err)
	}

	records := make(chan *kgo.Record)
	s := &kafkaConsumerService{
		pusher:  p,
		client:  kafkaClient,
		decoder: decoder,
		records: records,
		logger:  logger,
	}
	s.consumer = kafkav2.NewGroupConsumer(kafkaClient, cfg.KafkaConfig.Topic, records, logger, registerer)
	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)
	return s, nil
}

func (s *kafkaConsumerService) starting(ctx context.Context) error {
	return services.StartAndAwaitRunning(ctx, s.consumer)
}

func (s *kafkaConsumerService) running(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case record, ok := <-s.records:
			if !ok {
				return nil
			}
			s.consume(ctx, record)
		}
	}
}

func (s *kafkaConsumerService) stopping(_ error) error {
	err := services.StopAndAwaitTerminated(context.Background(), s.consumer)
	s.client.Close()
	return err
}

func (s *kafkaConsumerService) consume(ctx context.Context, record *kgo.Record) {
	stream, err := s.decoder.DecodeWithoutLabels(record.Value)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to decode kafka record", "err", err)
		return
	}

	pushCtx := user.InjectOrgID(ctx, string(record.Key))
	req := &logproto.PushRequest{Streams: []logproto.Stream{stream}}
	if _, err := s.pusher.Push(pushCtx, req); err != nil {
		level.Error(s.logger).Log("msg", "failed to push record", "err", err)
	}
}
