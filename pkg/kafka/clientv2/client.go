package clientv2

import (
	"context"
	"math"
	"time"

	"github.com/go-kit/log"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/plugin/kotel"
	"github.com/twmb/franz-go/plugin/kprom"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/loki/v3/pkg/kafka"
)

// New returns a new Kafka client or an error. Refer to client_test.go for
// examples of how to create different clients.
func New(cfg kafka.Config, metrics *kprom.Metrics, logger log.Logger, opts ...kgo.Opt) (*kgo.Client, error) {
	opts = append(newCommonOpts(cfg, metrics, logger), opts...)
	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// An OptsBuilder is used to build Kafka options using [OptsBuilder.With].
// The zero value is ready to use.
type OptsBuilder struct {
	opts []kgo.Opt
}

// With appends the options to b.
func (b *OptsBuilder) With(opts ...kgo.Opt) {
	b.opts = append(b.opts, opts...)
}

// Opts returns all appended options.
func (b *OptsBuilder) Opts() []kgo.Opt {
	return b.opts
}

// NewConsumerOpts returns the opts for a client that consumes from Kafka.
func NewConsumerOpts() []kgo.Opt {
	fetchMaxBytes := int32(100_000_000)
	return []kgo.Opt{
		kgo.FetchMinBytes(1),
		kgo.FetchMaxBytes(fetchMaxBytes),
		kgo.FetchMaxWait(5 * time.Second),
		kgo.FetchMaxPartitionBytes(50_000_000),
		kgo.BrokerMaxReadBytes(2 * fetchMaxBytes),
	}
}

// NewProducerOpts returns the opts for a client that produces to Kafka.
func NewProducerOpts(cfg kafka.Config, maxInflightProduceRequests int) []kgo.Opt {
	return []kgo.Opt{
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.DefaultProduceTopic(cfg.Topic),

		// We set the partition field in each record.
		kgo.RecordPartitioner(kgo.ManualPartitioner()),

		// Set the upper bounds the size of a record batch.
		kgo.ProducerBatchMaxBytes(kafka.ProducerBatchMaxBytes),

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
		kgo.ProducerLinger(50 * time.Millisecond),
		kgo.MaxProduceRequestsInflightPerBroker(maxInflightProduceRequests),

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
		kgo.RecordDeliveryTimeout(cfg.WriteTimeout),
		kgo.ProduceRequestTimeout(cfg.WriteTimeout),
		kgo.RequestTimeoutOverhead(2 * time.Second),

		// Unlimited number of buffered records because we limit on bytes in Writer. The reason why we don't use
		// kgo.MaxBufferedBytes() is because it suffers a deadlock issue:
		// https://github.com/twmb/franz-go/issues/777
		kgo.MaxBufferedRecords(math.MaxInt), // Use a high value to set it as unlimited, because the client doesn't support "0 as unlimited".
		kgo.MaxBufferedBytes(0),
	}
}

// newCommonOpts returns the common opts for both produce and consume clients.
func newCommonOpts(cfg kafka.Config, metrics *kprom.Metrics, logger log.Logger) []kgo.Opt {
	opts := []kgo.Opt{
		kgo.ClientID(cfg.ClientID),
		kgo.SeedBrokers(cfg.Address),
		kgo.DialTimeout(cfg.DialTimeout),

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
		//   1. The "metadata" request may be expensive to run on the Kafka
		//      backend.
		//   2. If the backend returns each time a different authoritative
		//      owner for a partition, then each time the cluster metadata is
		//      updated the Kafka client will create a new connection for each
		//      partition, leading to a high connections churn rate.
		//
		// We currently set min and max age to the same value to have constant
		// load on the Kafka backend: regardless there are errors or not, the
		// metadata requests frequency doesn't change.
		kgo.MetadataMinAge(10 * time.Second),
		kgo.MetadataMaxAge(10 * time.Second),
		kgo.WithLogger(newLogger(logger)),
		kgo.RetryTimeoutFn(func(key int16) time.Duration {
			switch key {
			case ((*kmsg.ListOffsetsRequest)(nil)).Key():
				return cfg.LastProducedOffsetRetryTimeout
			}
			return 30 * time.Second // 30s is the default timeout.
		}),
	}
	// SASL plain auth.
	if cfg.SASLUsername != "" && cfg.SASLPassword.String() != "" {
		opts = append(opts, kgo.SASL(plain.Plain(func(_ context.Context) (plain.Auth, error) {
			return plain.Auth{
				User: cfg.SASLUsername,
				Pass: cfg.SASLPassword.String(),
			}, nil
		})))
	}
	if cfg.AutoCreateTopicEnabled {
		opts = append(opts, kgo.AllowAutoTopicCreation())
	}
	tracer := kotel.NewTracer(
		kotel.TracerPropagator(
			propagation.NewCompositeTextMapPropagator(
				onlySampledTraces{propagation.TraceContext{}},
			),
		),
	)
	opts = append(opts, kgo.WithHooks(kotel.NewKotel(kotel.WithTracer(tracer)).Hooks()...))
	if metrics != nil {
		opts = append(opts, kgo.WithHooks(metrics))
	}
	return opts
}

type onlySampledTraces struct {
	propagation.TextMapPropagator
}

func (o onlySampledTraces) Inject(ctx context.Context, carrier propagation.TextMapCarrier) {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsSampled() {
		return
	}
	o.TextMapPropagator.Inject(ctx, carrier)
}
