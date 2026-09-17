package pattern

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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
// forwards each one to a pusher.
//
// Partition assignment and revocation are handled entirely by the broker and
// franz-go's internal bookkeeping: OnPartitionsAssigned/OnPartitionsRevoked/
// OnPartitionsLost are only used here to maintain the per partition lag
// metric, not for any push/commit logic.
type kafkaConsumerService struct {
	services.Service

	pusher   pusher
	client   *kgo.Client
	consumer *kafkav2.GroupConsumer
	decoder  *kafka.Decoder
	records  chan *kgo.Record
	logger   log.Logger
	topic    string

	lag *partitionLagTracker
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

	s := &kafkaConsumerService{
		pusher:  p,
		decoder: decoder,
		records: make(chan *kgo.Record),
		logger:  logger,
		topic:   cfg.KafkaConfig.Topic,
		lag:     newPartitionLagTracker(registerer),
	}

	kafkaClient, err := client.NewReaderClient(
		"pattern-ingester", cfg.KafkaConfig, logger, registerer,
		kgo.ConsumerGroup(cfg.KafkaConfig.ConsumerGroup),
		kgo.ConsumeTopics(cfg.KafkaConfig.Topic),
		// franz-go defaults to resetting to the start of the topic when a
		// partition has no committed offset yet (e.g. the very first time
		// this consumer group runs). We want new partitions to start from
		// the tip instead, so we don't replay the topic's entire retained
		// history on first start.
		kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()),
		// Pinned explicitly even though it's franz-go's default, so a
		// future upstream default change can't silently switch us to an
		// eager balancer, which would stop-the-world all partitions on
		// every rebalance instead of only the ones actually moving.
		kgo.Balancers(kgo.CooperativeStickyBalancer()),
		// Offsets are committed automatically on franz-go's default 5s
		// interval rather than after every push, to avoid an OffsetCommit
		// round-trip per record. Pattern-ingester state is already
		// best-effort and non-authoritative, so the wider window of
		// reprocessed records on a crash/rebalance is an acceptable trade
		// for the throughput this saves.
		kgo.OnPartitionsAssigned(s.onPartitionsAssigned),
		kgo.OnPartitionsRevoked(s.onPartitionsRevoked),
		kgo.OnPartitionsLost(s.onPartitionsLost),
	)
	if err != nil {
		return nil, fmt.Errorf("creating pattern ingester kafka consumer client: %w", err)
	}
	s.client = kafkaClient
	s.consumer = kafkav2.NewGroupConsumer(kafkaClient, cfg.KafkaConfig.Topic, s.records, logger, registerer)
	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)
	return s, nil
}

func (s *kafkaConsumerService) onPartitionsAssigned(_ context.Context, _ *kgo.Client, assigned map[string][]int32) {
	s.lag.assign(assigned[s.topic])
}

// onPartitionsRevoked performs the blocking commit of already-polled offsets
// that franz-go's default OnPartitionsRevoked would otherwise have done for
// us before handing the partitions off (installing a custom
// OnPartitionsRevoked replaces that default entirely), then cleans up the
// lag metric for the revoked partitions.
func (s *kafkaConsumerService) onPartitionsRevoked(ctx context.Context, _ *kgo.Client, revoked map[string][]int32) {
	if err := s.client.CommitUncommittedOffsets(ctx); err != nil {
		level.Error(s.logger).Log("msg", "failed to commit offsets on partitions revoked", "err", err)
	}
	s.lag.revoke(revoked[s.topic])
}

// onPartitionsLost cleans up the lag metric for the lost partitions. We
// deliberately do not attempt to commit offsets here: unlike a revoke, a
// commit is unlikely to succeed once partitions are lost (see franz-go's
// OnPartitionsLost documentation).
func (s *kafkaConsumerService) onPartitionsLost(_ context.Context, _ *kgo.Client, lost map[string][]int32) {
	s.lag.revoke(lost[s.topic])
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
	s.lag.observe(record.Partition, record.Timestamp)

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

// partitionLagTracker reports, per partition, how far behind the tip of the
// log we are when we process a record. Entries only exist for partitions we
// are currently assigned: observe skips any partition whose entry has been
// removed by revoke, so a partition's metric can never come back from the
// dead after we've given it up.
type partitionLagTracker struct {
	gauge *prometheus.GaugeVec

	mu         sync.Mutex
	partitions map[int32]prometheus.Gauge
}

func newPartitionLagTracker(registerer prometheus.Registerer) *partitionLagTracker {
	return &partitionLagTracker{
		gauge: promauto.With(registerer).NewGaugeVec(prometheus.GaugeOpts{
			Subsystem: "pattern_ingester",
			Name:      "kafka_consumer_lag_seconds",
			Help:      "The estimated consumption lag in seconds for each partition currently assigned to this consumer, measured as the difference between the current time and the timestamp of the last record processed for that partition.",
		}, []string{"partition"}),
		partitions: make(map[int32]prometheus.Gauge),
	}
}

// assign creates the gauge for each of the given partitions.
func (t *partitionLagTracker) assign(partitions []int32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, partition := range partitions {
		t.partitions[partition] = t.gauge.WithLabelValues(strconv.Itoa(int(partition)))
	}
}

// revoke deletes the gauge for each of the given partitions.
func (t *partitionLagTracker) revoke(partitions []int32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, partition := range partitions {
		t.gauge.DeleteLabelValues(strconv.Itoa(int(partition)))
		delete(t.partitions, partition)
	}
}

// observe records how far behind partition we are as of ts. If partition
// isn't currently assigned to us, the observation is skipped rather than
// recreating its gauge.
func (t *partitionLagTracker) observe(partition int32, ts time.Time) {
	t.mu.Lock()
	gauge, ok := t.partitions[partition]
	t.mu.Unlock()
	if !ok {
		return
	}
	gauge.Set(time.Since(ts).Seconds())
}
