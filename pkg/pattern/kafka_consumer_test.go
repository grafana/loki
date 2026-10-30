package pattern

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v4/pkg/kafka"
	"github.com/grafana/loki/v4/pkg/kafka/testkafka"
	"github.com/grafana/loki/v4/pkg/logproto"
)

func newKafkaTestIngester(t *testing.T, kafkaCfg kafka.Config) *Ingester {
	t.Helper()
	cfg := defaultIngesterTestConfig(t)
	cfg.KafkaIngestion.Enabled = true
	cfg.KafkaIngestion.KafkaConfig = kafkaCfg
	// The ring must not be used when consuming from Kafka.
	ing, err := New(cfg, &fakeLimits{}, &fakeRingClient{ring: &fakeRing{}}, "test", prometheus.NewRegistry(), log.NewNopLogger())
	require.NoError(t, err)
	return ing
}

func TestIngesterKafkaOwner(t *testing.T) {
	_, kafkaCfg := testkafka.CreateCluster(t, 1, "topic")
	ing := newKafkaTestIngester(t, kafkaCfg)
	require.IsType(t, allStreamsOwner{}, ing.owner)

	ing, err := New(defaultIngesterTestConfig(t), &fakeLimits{}, &fakeRingClient{}, "test", prometheus.NewRegistry(), log.NewNopLogger())
	require.NoError(t, err)
	require.IsType(t, &ringOwner{}, ing.owner)
	require.Nil(t, ing.kafkaConsumer)
}

func TestConfigValidateKafkaIngestion(t *testing.T) {
	cfg := defaultIngesterTestConfig(t)
	cfg.LifecyclerConfig.RingConfig.ReplicationFactor = 1
	require.NoError(t, cfg.Validate())

	cfg.KafkaIngestion.Enabled = true
	cfg.KafkaIngestion.ConsumerGroup = ""
	require.EqualError(t, cfg.Validate(), "pattern ingester kafka consumer group must be set")

	cfg.KafkaIngestion.ConsumerGroup = "pattern-ingester"
	require.Equal(t, 3*time.Minute, cfg.KafkaIngestion.SessionTimeout)
	require.NoError(t, cfg.Validate())
}

func TestIngesterConsumesFromKafka(t *testing.T) {
	const topic = "topic"
	_, kafkaCfg := testkafka.CreateCluster(t, 1, topic)

	// The ring is never consulted. Any call on it fails the test.
	fakeRing := &fakeRing{}
	fakeRing.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(ring.ReplicationSet{}, nil).Maybe()

	cfg := defaultIngesterTestConfig(t)
	cfg.KafkaIngestion.Enabled = true
	cfg.KafkaIngestion.KafkaConfig = kafkaCfg
	ing, err := New(cfg, &fakeLimits{}, &fakeRingClient{ring: fakeRing}, "test", prometheus.NewRegistry(), log.NewNopLogger())
	require.NoError(t, err)
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), ing))
	t.Cleanup(func() { _ = services.StopAndAwaitTerminated(context.Background(), ing) })

	producer, err := kgo.NewClient(kgo.SeedBrokers(kafkaCfg.ReaderConfig.Address), kgo.DefaultProduceTopic(topic))
	require.NoError(t, err)
	t.Cleanup(producer.Close)

	stream := logproto.Stream{
		Labels:  `{service_name="svc"}`,
		Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "ts=1 msg=hello"}},
	}
	records, err := kafka.EncodeWithTopic(topic, 0, "tenant-1", stream, 1<<20)
	require.NoError(t, err)

	// The consumer starts from the end of the topic, so produce until it has
	// joined the group and consumed a record.
	require.Eventually(t, func() bool {
		require.NoError(t, producer.ProduceSync(context.Background(), records...).FirstErr())
		inst, ok := ing.getInstanceByID("tenant-1")
		return ok && inst.streams.Len() == 1
	}, 10*time.Second, 100*time.Millisecond)

	// A record that cannot be decoded is skipped, and the records after it are
	// still consumed.
	totalBefore := testutil.ToFloat64(ing.metrics.kafkaRecordsTotal)
	require.Zero(t, testutil.ToFloat64(ing.metrics.kafkaRecordsFailed))
	require.NoError(t, producer.ProduceSync(context.Background(), &kgo.Record{Key: []byte("tenant-1"), Value: []byte("not a stream")}).FirstErr())
	require.NoError(t, producer.ProduceSync(context.Background(), records...).FirstErr())
	require.Eventually(t, func() bool {
		return testutil.ToFloat64(ing.metrics.kafkaRecordsFailed) == 1 &&
			testutil.ToFloat64(ing.metrics.kafkaRecordsTotal) >= totalBefore+2
	}, 10*time.Second, 50*time.Millisecond)
}
