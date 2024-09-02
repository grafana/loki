package kafka

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/twmb/franz-go/plugin/kprom"

	"github.com/grafana/loki/v3/pkg/distributor"
	"github.com/grafana/loki/v3/pkg/logproto"
)

const writeTimeout = time.Minute

type Config struct {
	Address string `yaml:"address" docs:"the kafka endpoint to connect to"`
	Topic   string `yaml:"topic" docs:"the kafka topic to write to"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.Address, prefix+"address", "localhost:9092", "the kafka endpoint to connect to")
	f.StringVar(&cfg.Topic, prefix+".topic", "loki.push", "The Kafka topic name.")
}

type Tee struct {
	logger        log.Logger
	kafkaClient   *kgo.Client
	partitionRing *ring.PartitionInstanceRing

	ingesterAppends *prometheus.CounterVec
}

func NewTee(
	cfg Config,
	metricsNamespace string,
	registerer prometheus.Registerer,
	logger log.Logger,
	partitionRing *ring.PartitionInstanceRing,
) (*Tee, error) {
	registerer = prometheus.WrapRegistererWithPrefix(metricsNamespace+"_", registerer)

	metrics := kprom.NewMetrics(
		"", // No prefix. We expect the input prometheus.Registered to be wrapped with a prefix.
		kprom.Registerer(registerer),
		kprom.FetchAndProduceDetail(kprom.Batches, kprom.Records, kprom.CompressedBytes, kprom.UncompressedBytes))

	opts := append([]kgo.Opt{},
		kgo.SeedBrokers(cfg.Address),

		kgo.WithHooks(metrics),
		// commonKafkaClientOptions(kafkaCfg, metrics, logger),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.DefaultProduceTopic(cfg.Topic),

		kgo.AllowAutoTopicCreation(),
		// We set the partition field in each record.
		kgo.RecordPartitioner(kgo.ManualPartitioner()),

		// Set the upper bounds the size of a record batch.
		kgo.ProducerBatchMaxBytes(1024*1024*1),

		// By default, the Kafka client allows 1 Produce in-flight request per broker. Disabling write idempotency
		// (which we don't need), we can increase the max number of in-flight Produce requests per broker. A higher
		// number of in-flight requests, in addition to short buffering ("linger") in client side before firing the
		// next Produce request allows us to reduce the end-to-end latency.
		//
		// The result of the multiplication of producer linger and max in-flight requests should match the maximum
		// Produce latency expected by the Kafka backend in a steady state. For example, 50ms * 20 requests = 1s,
		// which means the Kafka client will keep issuing a Produce request every 50ms as far as the Kafka backend
		// doesn't take longer than 1s to process them (if it takes longer, the client will buffer data and stop
		// issuing new Produce requests until some previous ones complete).
		kgo.DisableIdempotentWrite(),
		kgo.ProducerLinger(50*time.Millisecond),
		kgo.MaxProduceRequestsInflightPerBroker(20),

		// Unlimited number of Produce retries but a deadline on the max time a record can take to be delivered.
		// With the default config it would retry infinitely.
		//
		// Details of the involved timeouts:
		// - RecordDeliveryTimeout: how long a Kafka client Produce() call can take for a given record. The overhead
		//   timeout is NOT applied.
		// - ProduceRequestTimeout: how long to wait for the response to the Produce request (the Kafka protocol message)
		//   after being sent on the network. The actual timeout is increased by the configured overhead.
		//
		// When a Produce request to Kafka fail, the client will retry up until the RecordDeliveryTimeout is reached.
		// Once the timeout is reached, the Produce request will fail and all other buffered requests in the client
		// (for the same partition) will fail too. See kgo.RecordDeliveryTimeout() documentation for more info.
		kgo.RecordRetries(math.MaxInt),
		kgo.RecordDeliveryTimeout(time.Minute),
		kgo.ProduceRequestTimeout(time.Minute),
		kgo.RequestTimeoutOverhead(time.Minute),

		// Unlimited number of buffered records because we limit on bytes in Writer. The reason why we don't use
		// kgo.MaxBufferedBytes() is because it suffers a deadlock issue:
		// https://github.com/twmb/franz-go/issues/777
		kgo.MaxBufferedRecords(math.MaxInt), // Use a high value to set it as unlimited, because the client doesn't support "0 as unlimited".
		kgo.MaxBufferedBytes(0),
	)

	kafkaClient, err := kgo.NewClient(opts...)
	if err != nil {
		panic("failed to start kafka client")
	}

	t := &Tee{
		logger: log.With(logger, "component", "kafka-tee"),
		ingesterAppends: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "kafka_ingester_appends_total",
			Help: "The total number of appends sent to kafka ingest path.",
		}, []string{"partition", "status"}),
		kafkaClient:   kafkaClient,
		partitionRing: partitionRing,
	}

	return t, nil
}

// Duplicate Implements distributor.Tee which is used to tee distributor requests to pattern ingesters.
func (t *Tee) Duplicate(tenant string, streams []distributor.KeyedStream) {
	for idx := range streams {
		go func(stream distributor.KeyedStream) {
			if err := t.sendStream(tenant, stream); err != nil {
				level.Error(t.logger).Log("msg", "failed to send stream to kafka", "err", err)
			}
		}(streams[idx])
	}
}

func (t *Tee) sendStream(tenant string, stream distributor.KeyedStream) error {
	partitionID, err := t.partitionRing.PartitionRing().ActivePartitionForKey(stream.HashKey)
	if err != nil {
		t.ingesterAppends.WithLabelValues("partition_unknown", "fail").Inc()
		return fmt.Errorf("failed to find active partition for stream: %w", err)
	}
	records, err := marshalWriteRequestToRecords(partitionID, tenant, stream.Stream, 1024*1024)

	ctx, cancel := context.WithTimeout(user.InjectOrgID(context.Background(), tenant), writeTimeout)
	defer cancel()
	produceResults := t.kafkaClient.ProduceSync(ctx, records...)

	var finalErr error
	for _, result := range produceResults {
		if result.Err != nil {
			t.ingesterAppends.WithLabelValues(fmt.Sprintf("partition_%d", partitionID), "fail").Inc()
			finalErr = err
		} else {
			t.ingesterAppends.WithLabelValues(fmt.Sprintf("partition_%d", partitionID), "success").Inc()
		}
	}

	return finalErr
}

// marshalWriteRequestToRecords marshals a mimirpb.WriteRequest to one or more Kafka records.
// The request may be split to multiple records to get that each single Kafka record
// data size is not bigger than maxSize.
//
// This function is a best-effort. The returned Kafka records are not strictly guaranteed to
// have their data size limited to maxSize. The reason is that the WriteRequest is split
// by each individual Timeseries and Metadata: if a single Timeseries or Metadata is bigger than
// maxSize, than the resulting record will be bigger than the limit as well.
func marshalWriteRequestToRecords(partitionID int32, tenantID string, stream logproto.Stream, maxSize int) ([]*kgo.Record, error) {
	reqSize := stream.Size()

	if reqSize <= maxSize {
		// No need to split the request. We can take a fast path.
		rec, err := marshalWriteRequestToRecord(partitionID, tenantID, stream, reqSize)
		if err != nil {
			return nil, err
		}

		return []*kgo.Record{rec}, nil
	}
	return nil, errors.New("large write requests are not supported yet")

	// return marshalWriteRequestsToRecords(partitionID, tenantID, mimirpb.SplitWriteRequestByMaxMarshalSize(req, reqSize, maxSize))
}

func marshalWriteRequestToRecord(partitionID int32, tenantID string, stream logproto.Stream, reqSize int) (*kgo.Record, error) {
	// Marshal the request.
	data := make([]byte, reqSize)
	n, err := stream.MarshalToSizedBuffer(data)
	if err != nil {
		return nil, fmt.Errorf("failed to serialise write request: %w", err)
	}
	data = data[:n]

	return &kgo.Record{
		Key:       []byte(tenantID), // We don't partition based on the key, so the value here doesn't make any difference.
		Value:     data,
		Partition: partitionID,
	}, nil
}
