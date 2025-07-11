package distributor

import (
	"context"
	"flag"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
)

// MultiTenantTopicConfig configures the MultiTenantTopicWriter.
type MultiTenantTopicConfig struct {
	Enabled            bool          `yaml:"enabled"`
	Topic              string        `yaml:"topic"`
	NumPartitions      int           `yaml:"num_partitions"`
	MaxBufferedBytes   flagext.Bytes `yaml:"max_buffered_bytes"`
	MaxRecordSizeBytes flagext.Bytes `yaml:"max_record_size_bytes"`
}

// RegisterFlags adds the flags required to configure this flag set.
func (cfg *MultiTenantTopicConfig) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, "distributor.multi-tenant-topic-tee.enabled", false, "Enable the multi-tenant topic tee.")
	f.StringVar(&cfg.Topic, "distributor.multi-tenant-topic-tee.topic", "loki.multi-tenant", "The name of the multi-tenant topic.")
	f.IntVar(&cfg.NumPartitions, "distributor.multi-tenant-topic-tee.num-partitions", 64, "The number of partitions")
	cfg.MaxBufferedBytes = 100 << 20 // 100MB
	f.Var(&cfg.MaxBufferedBytes, "distributor.multi-tenant-topic-tee.max-buffered-bytes", "Maximum number of bytes that can be buffered before producing to Kafka")
	cfg.MaxRecordSizeBytes = kafka.MaxProducerRecordDataBytesLimit
	f.Var(&cfg.MaxRecordSizeBytes, "distributor.multi-tenant-topic-tee.max-record-size-bytes", "Maximum size of a single Kafka record in bytes")
}

type MultiTenantTopicWriter struct {
	cfg      MultiTenantTopicConfig
	producer *client.Producer
	logger   log.Logger

	produces        prometheus.Counter
	produceFailures prometheus.Counter
}

// NewMultiTenantTopicWriter creates a new MultiTenantTopicWriter.
func NewMultiTenantTopicWriter(
	cfg MultiTenantTopicConfig,
	kafkaClient *kgo.Client,
	reg prometheus.Registerer,
	logger log.Logger,
) (*MultiTenantTopicWriter, error) {
	return &MultiTenantTopicWriter{
		cfg: cfg,
		producer: client.NewProducer(
			"distributor_multi_tenant_topic_tee",
			kafkaClient,
			int64(cfg.MaxBufferedBytes),
			reg,
		),
		logger: logger,
		produces: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_multi_tenant_topic_tee_produced_total",
			Help: "The total number of produced records, including failures.",
		}),
		produceFailures: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_multi_tenant_topic_tee_produce_failed_total",
			Help: "The total number of failed produced records.",
		}),
	}, nil
}

// Duplicate implements the Tee interface
func (t *MultiTenantTopicWriter) Duplicate(tenant string, streams []KeyedStream) {
	if len(streams) == 0 {
		return
	}
	go t.duplicate(tenant, streams)
}

// Close stops the writer and releases resources
func (t *MultiTenantTopicWriter) Close() error {
	t.producer.Close()
	return nil
}

func (t *MultiTenantTopicWriter) duplicate(tenant string, streams []KeyedStream) {
	records := make([]*kgo.Record, 0, len(streams))
	for _, stream := range streams {
		// The Kafka client assumes manual partitioner is used. To avoid
		// refactoring this just to experiment with mulit-tenant topics,
		// instead we do a simple hash ourselves.
		partition := int32(stream.Stream.Hash % uint64(t.cfg.NumPartitions))
		streamRecords, err := kafka.EncodeWithTopic(t.cfg.Topic, partition, tenant, stream.Stream, int(t.cfg.MaxRecordSizeBytes))
		if err != nil {
			level.Error(t.logger).Log(
				"msg", "failed to encode stream",
				"tenant", tenant,
				"err", err,
			)
			continue
		}
		records = append(records, streamRecords...)
	}
	if len(records) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	t.produces.Inc()
	results := t.producer.ProduceSync(ctx, records)
	if err := results.FirstErr(); err != nil {
		t.produceFailures.Inc()
		level.Error(t.logger).Log(
			"msg", "failed to produce records",
			"tenant", tenant,
			"err", err,
		)
		return
	}
}
