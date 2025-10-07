package client

import (
	"context"
	"math"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/plugin/kotel"
	"github.com/twmb/franz-go/plugin/kprom"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// NewClient returns a new [kgo.Client] from the opts.
func NewClient(cfg Config, namespace string, logger log.Logger, reg prometheus.Registerer, opts ...kgo.Opt) (*kgo.Client, error) {
	reg = prometheus.WrapRegistererWith(prometheus.Labels{"client": namespace}, reg)

	metrics := kprom.NewMetrics("loki_kafkav2",
		kprom.Registerer(reg),
		kprom.FetchAndProduceDetail(
			kprom.Batches,
			kprom.Records,
			kprom.CompressedBytes,
			kprom.UncompressedBytes,
		),
	)
	clientOpts := commonOpts(cfg, logger, metrics)
	clientOpts = append(clientOpts, opts...)
	return kgo.NewClient(clientOpts...)
}

// ConsumerOpts returns the common options for all clients that consume records.
func ConsumerOpts(cfg Config) []kgo.Opt {
	opts := []kgo.Opt{
		kgo.FetchMinBytes(int32(cfg.FetchMinBytes)),
		kgo.FetchMaxBytes(int32(cfg.FetchMaxBytes)),
		kgo.FetchMaxWait(cfg.FetchMaxWait),
		kgo.FetchMaxPartitionBytes(int32(cfg.FetchMaxPartitionBytes)),
		// This is a safety measure to avoid OOMs on invalid responses.
		// The recommendation is to set it twice FetchMaxBytes.
		kgo.BrokerMaxReadBytes(int32(2 * cfg.FetchMaxBytes)),
	}
	if cfg.Topic != "" {
		opts = append(opts, kgo.ConsumeTopics(cfg.Topic))
	}
	if cfg.TopicRegex {
		opts = append(opts, kgo.ConsumeRegex())
	}
	if cfg.ConsumerGroup != "" {
		opts = append(opts, kgo.ConsumerGroup(cfg.ConsumerGroup))
	}
	return opts
}

// ConsumerOpts returns the common options for all clients that produce records.
func ProducerOpts(cfg Config) []kgo.Opt {
	return []kgo.Opt{
		kgo.RequiredAcks(kgo.AllISRAcks()),

		// By default, the Kafka client allows 1 Produce in-flight request
		// per broker. Disabling write idempotency (which we don't need), we
		// can increase the max number of in-flight Produce requests per
		// broker. A higher number of in-flight requests, in addition to short
		// buffering ("linger") in client side before firing the next Produce
		// request allows us to reduce the end-to-end latency.
		//
		// The result of the multiplication of producer linger and max
		// in-flight requests should match the maximum Produce latency
		// expected by the Kafka backend in a steady state. For example,
		// 50ms * 20 requests = 1s, which means the Kafka client will keep
		// issuing a Produce request every 50ms as far as the Kafka backend
		// doesn't take longer than 1s to process them (if it takes longer,
		// the client will buffer data and stop issuing new Produce requests
		// ntil some previous ones complete).
		kgo.DisableIdempotentWrite(),
		kgo.ProducerLinger(50 * time.Millisecond),
		kgo.MaxProduceRequestsInflightPerBroker(cfg.MaxInFlightRequestsPerBroker),

		// Unlimited number of Produce retries but a deadline on the max time
		// a record can take to be delivered. With the default config it would
		// retry infinitely.
		//
		// Details of the involved timeouts:
		// - RecordDeliveryTimeout: how long a Kafka client Produce() call can
		//   take for a given record. The overhead timeout is NOT applied.
		// - ProduceRequestTimeout: how long to wait for the response to the
		//   Produce request (the Kafka protocol message) after being sent on
		//   the network. The actual timeout is increased by the configured
		//   overhead.
		//
		// When a Produce request to Kafka fail, the client will retry up
		// until the RecordDeliveryTimeout is reached. Once the timeout is
		// reached, the Produce request will fail and all other buffered
		// requests in the client (for the same partition) will fail too.
		// See [kgo.RecordDeliveryTimeout] documentation for more info.
		kgo.RecordRetries(math.MaxInt),
		// Hard-code these for now while I understand how they should be used.
		kgo.RecordDeliveryTimeout(10 * time.Second),
		kgo.ProduceRequestTimeout(10 * time.Second),
		kgo.RequestTimeoutOverhead(2 * time.Second),
		kgo.ProducerBatchMaxBytes(kafka.ProducerBatchMaxBytes),
	}
}

// commonOpts returns the common options for all clients.
func commonOpts(cfg Config, logger log.Logger, metrics *kprom.Metrics) []kgo.Opt {
	opts := []kgo.Opt{
		kgo.ClientID(cfg.ClientID),
		kgo.SeedBrokers(cfg.Address),

		// A cluster metadata update is a request sent to a broker and getting
		// back the map of partitions and the leader broker for each partition.
		// The cluster metadata can be updated (a) periodically or (b) when
		// some events occur (e.g. backoff due to errors).
		//
		// MetadataMinAge() sets the minimum time between two cluster metadata
		// updates due to events. MetadataMaxAge() sets how frequently the
		// periodic update should occur.
		//
		// It's important to note that the periodic update is also used to
		// discover new brokers (e.g. during a rolling update or after a scale
		// up). For this reason, it's important to run the update frequently.
		//
		// The other two side effects of frequently updating the cluster
		// metadata:
		//
		// 1. The "metadata" request may be expensive to run on the Kafka
		//    backend.
		// 2. If the backend returns each time a different authoritative owner
		//    for a partition, then each time the cluster metadata is updated
		//    the Kafka client will create a new connection for each partition,
		//    leading to a high connections churn rate.
		//
		// We currently set min and max age to the same value to have constant
		// load on the Kafka backend: regardless there are errors or not, the
		// metadata requests frequency doesn't change.
		kgo.MetadataMinAge(10 * time.Second),
		kgo.MetadataMaxAge(10 * time.Second),
		kgo.RetryTimeoutFn(func(key int16) time.Duration {
			switch key {
			case ((*kmsg.ListOffsetsRequest)(nil)).Key():
				return 10 * time.Second
			}
			return 30 * time.Second
		}),
		kgo.DialTimeout(cfg.DialTimeout),
		kgo.WithLogger(newLogger(logger)),
	}

	// SASL plain auth.
	if cfg.SASLUsername != "" && cfg.SASLPassword.String() != "" {
		opts = append(opts, kgo.SASL(
			plain.Plain(func(_ context.Context) (plain.Auth, error) {
				return plain.Auth{
					User: cfg.SASLUsername,
					Pass: cfg.SASLPassword.String(),
				}, nil
			}),
		))
	}

	tracer := kotel.NewTracer(
		kotel.TracerPropagator(
			propagation.NewCompositeTextMapPropagator(
				sampledTraces{propagation.TraceContext{}})),
	)
	opts = append(opts, kgo.WithHooks(kotel.NewKotel(kotel.WithTracer(tracer)).Hooks()...))

	if metrics != nil {
		opts = append(opts, kgo.WithHooks(metrics))
	}

	return opts
}

type sampledTraces struct {
	propagation.TextMapPropagator
}

func (o sampledTraces) Inject(ctx context.Context, carrier propagation.TextMapCarrier) {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsSampled() {
		return
	}
	o.TextMapPropagator.Inject(ctx, carrier)
}
