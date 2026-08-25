package consumer

import (
	"context"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

// A producer allows mocking of certain [kgo.Client] methods in tests.
type producer interface {
	ProduceSync(ctx context.Context, records ...*kgo.Record) kgo.ProduceResults
}

// metastoreEvents emits events to the metastore Kafka topic.
type metastoreEvents struct {
	producer       producer
	partition      int32
	partitionRatio int32
}

// newMetastoreEvents returns a new metastore events.
func newMetastoreEvents(partition int32, partitionRatio int32, producer producer) *metastoreEvents {
	return &metastoreEvents{
		producer:       producer,
		partition:      partition,
		partitionRatio: partitionRatio,
	}
}

// Emit an event to the metastore Kafka topic.
func (m *metastoreEvents) Emit(ctx context.Context, objectPath string, earliestRecordTime time.Time) error {
	event := &metastore.ObjectWrittenEvent{
		ObjectPath:         objectPath,
		WriteTime:          time.Now().Format(time.RFC3339),
		EarliestRecordTime: earliestRecordTime.Format(time.RFC3339),
	}
	b, err := event.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal event proto: %w", err)
	}
	// The metastore events partition is calculated based on the consumed partition
	// and the partition ratio. This has the effect of concentrating events within
	// fewer metastore partitions.
	partition := m.partition / m.partitionRatio
	res := m.producer.ProduceSync(ctx, &kgo.Record{
		Value:     b,
		Partition: partition,
	})
	return res.FirstErr()
}
