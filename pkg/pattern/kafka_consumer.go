package pattern

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/kafkav2"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// initKafka creates the Kafka client and group consumer. The consumer is
// started and stopped with the ingester.
func (i *Ingester) initKafka() error {
	cfg := i.cfg.KafkaIngestion
	decoder, err := kafka.NewDecoder()
	if err != nil {
		return fmt.Errorf("failed to create kafka decoder: %w", err)
	}
	// Offsets are committed automatically by the client. The patterns are only
	// kept in memory, so there is no need to replay records after a restart.
	// The instance ID makes the group membership static, so a restarted ingester
	// gets its partitions back without a rebalance, as long as it rejoins within
	// the session timeout.
	i.kafkaClient, err = client.NewReaderClient(
		"pattern-ingester",
		cfg.KafkaConfig,
		i.logger,
		i.registerer,
		kgo.ConsumerGroup(cfg.ConsumerGroup),
		kgo.InstanceID(i.cfg.LifecyclerConfig.ID),
		kgo.SessionTimeout(cfg.SessionTimeout),
		kgo.AutoCommitInterval(cfg.KafkaConfig.ConsumerGroupOffsetCommitInterval),
		kgo.Balancers(kgo.RoundRobinBalancer()),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()),
	)
	if err != nil {
		return fmt.Errorf("failed to create kafka client: %w", err)
	}
	i.kafkaDecoder = decoder
	i.kafkaRecords = make(chan *kgo.Record)
	i.kafkaConsumer = kafkav2.NewGroupConsumer(
		i.kafkaClient,
		cfg.KafkaConfig.Topic,
		i.kafkaRecords,
		i.logger,
		prometheus.WrapRegistererWithPrefix("pattern_ingester_kafka_", i.registerer),
	)
	i.lifecyclerWatcher.WatchService(i.kafkaConsumer)
	return nil
}

// consumeRecords pushes the records from Kafka to the instance of the tenant
// until the channel is closed.
func (i *Ingester) consumeRecords(ctx context.Context, records <-chan *kgo.Record) {
	for rec := range records {
		i.metrics.kafkaRecordsTotal.Inc()
		i.metrics.kafkaConsumerLagHighWatermark.Observe(time.Since(rec.Timestamp).Seconds())
		stream, err := i.kafkaDecoder.DecodeWithoutLabels(rec.Value)
		if err != nil {
			level.Error(i.logger).Log("msg", "failed to decode record", "offset", rec.Offset, "err", err)
			i.metrics.kafkaRecordsFailed.Inc()
			continue
		}
		instanceID := string(rec.Key)
		instance, err := i.GetOrCreateInstance(instanceID)
		if err != nil {
			level.Error(i.logger).Log("msg", "failed to get instance", "tenant", instanceID, "offset", rec.Offset, "err", err)
			i.metrics.kafkaRecordsFailed.Inc()
			continue
		}
		req := &logproto.PushRequest{Streams: []logproto.Stream{stream}}
		if err := instance.Push(ctx, req); err != nil {
			level.Warn(i.logger).Log("msg", "failed to push record", "tenant", instanceID, "offset", rec.Offset, "err", err)
			i.metrics.kafkaRecordsFailed.Inc()
			continue
		}
	}
}

// stopKafka stops consuming from Kafka and waits for the records that were
// already received to be pushed. It is safe to call more than once.
func (i *Ingester) stopKafka() {
	if i.kafkaConsumer == nil {
		return
	}
	i.kafkaStopOnce.Do(func() {
		stopCtx := context.Background()
		if err := services.StopAndAwaitTerminated(stopCtx, i.kafkaConsumer); err != nil {
			level.Warn(i.logger).Log("msg", "failed to stop kafka consumer", "err", err)
		}
		// Wait for goroutine running consumeRecords to also stop.
		i.kafkaDone.Wait()
		// Close the franz-go client.
		i.kafkaClient.Close()
	})
}
