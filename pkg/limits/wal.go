package limits

import (
	"context"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

type WAL interface {
	// Append writes the metadata record to the end of the WAL.
	Append(ctx context.Context, tenant string, metadata *proto.StreamMetadata) error

	// Close closes the WAL.
	Close() error
}

// KafkaWAL is a write-ahead log on top of Kafka.
type KafkaWAL struct {
	client     *kgo.Client
	topic      string
	partitions uint64
	logger     log.Logger
}

func NewKafkaWAL(client *kgo.Client, topic string, partitions uint64, logger log.Logger) *KafkaWAL {
	return &KafkaWAL{
		client:     client,
		topic:      topic,
		partitions: partitions,
		logger:     logger,
	}
}

// Append implements the WAL interface.
func (w *KafkaWAL) Append(ctx context.Context, tenant string, metadata *proto.StreamMetadata) error {
	v := proto.StreamMetadataRecord{
		Tenant:   tenant,
		Metadata: metadata,
	}
	b, err := v.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal proto: %w", err)
	}
	// The stream metadata topic expects a fixed number of partitions,
	// the size of which is determined ahead of time. Streams are
	// sharded over partitions using a simple mod.
	partition := int32(metadata.StreamHash % w.partitions)
	r := kgo.Record{
		Key:       []byte(tenant),
		Value:     b,
		Partition: partition,
		Topic:     w.topic,
	}
	w.client.Produce(ctx, &r, w.logProduceErr)
	return nil
}

// Close implements the WAL interface.
func (w *KafkaWAL) Close() error {
	w.Close()
	return nil
}

func (w *KafkaWAL) logProduceErr(_ *kgo.Record, err error) {
	if err != nil {
		level.Error(w.logger).Log("msg", "failed to produce record", "err", err.Error())
	}
}
