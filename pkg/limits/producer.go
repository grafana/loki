package limits

import (
	"context"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

// KafkaProducer allows mocking of certain [kgo.Client] methods in tests.
type KafkaProducer interface {
	Produce(context.Context, *kgo.Record, func(*kgo.Record, error))
}

// Producer produces records on the metadata topic. It is how state is
// replicated across zones and recovered following a crash or restart.
type Producer struct {
	client KafkaProducer
	// TODO(grobinson): We should remove topic in future, as it should be
	// set in the client.
	topic         string
	numPartitions int
	zone          string
	logger        log.Logger

	produced       prometheus.Counter
	producedFailed prometheus.Counter
}

// NewProducer returns a new Sender.
func NewProducer(client KafkaProducer, topic string, numPartitions int, zone string, logger log.Logger, reg prometheus.Registerer) *Producer {
	return &Producer{
		client:        client,
		topic:         topic,
		numPartitions: numPartitions,
		zone:          zone,
		logger:        logger,
		produced: promauto.With(reg).NewCounter(
			prometheus.CounterOpts{
				Name: "loki_ingest_limits_records_produced_total",
				Help: "The total number of produced records, including failures.",
			},
		),
		producedFailed: promauto.With(reg).NewCounter(
			prometheus.CounterOpts{
				Name: "loki_ingest_limits_records_produced_failed_total",
				Help: "The total number of failed produced records.",
			},
		),
	}
}

// Produce encodes the metadata in a [proto.StreamMetadataRecord] record
// and pushes it to the metadata topic. It does not wait for the push to
// complete.
func (p *Producer) Produce(ctx context.Context, tenant string, metadata *proto.StreamMetadata) error {
	v := proto.StreamMetadataRecord{
		Zone:     p.zone,
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
	partition := int32(metadata.StreamHash % uint64(p.numPartitions))
	r := kgo.Record{
		Key:       []byte(tenant),
		Value:     b,
		Topic:     p.topic,
		Partition: partition,
	}
	p.produced.Inc()
	p.client.Produce(context.WithoutCancel(ctx), &r, p.handleProduceErr)
	return nil
}

func (p *Producer) handleProduceErr(_ *kgo.Record, err error) {
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to produce record", "err", err.Error())
		p.producedFailed.Inc()
	}
}
