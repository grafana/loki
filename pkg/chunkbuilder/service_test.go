package chunkbuilder

import (
	"context"
	"errors"
	"flag"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/testkafka"
	"github.com/grafana/loki/v3/pkg/kafkav2"
	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/pkg/push"
)

const testTopic = "replay"

func TestNew(t *testing.T) {
	t.Run("returns an error when the config is not valid", func(t *testing.T) {
		cfg := testConfig(0)
		cfg.Topic = ""

		_, err := New(kafka.Config{}, cfg, prometheus.NewRegistry(), log.NewNopLogger())
		require.EqualError(t, err, "invalid chunk-builder config: the topic is required")
	})

	t.Run("returns an error when the kafka config is not valid", func(t *testing.T) {
		_, err := New(kafka.Config{}, testConfig(0), prometheus.NewRegistry(), log.NewNopLogger())
		require.ErrorIs(t, err, kafka.ErrMissingKafkaAddress)
	})
}

func TestService_ConsumesAndDecodesRecords(t *testing.T) {
	ctx := testContext(t)
	_, kafkaCfg := testkafka.CreateCluster(t, 2, testTopic)
	// The chunk-builder consumes its own topic, not the topic of the ingesters.
	kafkaCfg.Topic = "ingest"
	producer := newTestProducer(t, kafkaCfg)

	// The chunk-builder consumes partition 1, so it must skip the record in
	// partition 0.
	other := flatRecord(t, 0, "tenant-a", `{app="other"}`, "skipped")
	flat := flatRecord(t, 1, "tenant-a", `{app="flat"}`, "a flat line")
	nested := nestedRecord(t, 1, "tenant-b", `{app="nested"}`, "a nested line")
	garbage := &kgo.Record{Topic: testTopic, Partition: 1, Key: []byte("tenant-a"), Value: []byte("invalid data")}
	noTenant := flatRecord(t, 1, "", `{app="flat"}`, "a line without a tenant")
	produce(t, producer, other, flat, nested, garbage, noTenant)

	reg := prometheus.NewRegistry()
	s, err := New(kafkaCfg, testConfig(1), reg, log.NewNopLogger())
	require.NoError(t, err)
	require.NoError(t, services.StartAndAwaitRunning(ctx, s))
	t.Cleanup(func() {
		require.NoError(t, services.StopAndAwaitTerminated(context.Background(), s))
	})

	m := s.processor.metrics
	// The processor counts a record before it decodes it, so wait for the
	// decode failure of the last record too.
	require.Eventually(t, func() bool {
		return testutil.ToFloat64(m.records) == 4 && testutil.ToFloat64(m.decodeFailures) == 2
	}, 10*time.Second, 10*time.Millisecond, "the invalid record and the record without a tenant fail to decode")
	require.Equal(t, float64(noTenant.Offset), testutil.ToFloat64(m.lastOffset))
	require.Equal(t, float64(len(flat.Value)+len(nested.Value)+len(garbage.Value)+len(noTenant.Value)), testutil.ToFloat64(m.receivedBytes))
	require.GreaterOrEqual(t, testutil.ToFloat64(m.consumptionLag), float64(0))

	// TODO(chunk-builders): Expect a commit once checkpoints commit offsets.
	committed, err := kafkav2.NewOffsetReader(producer, testTopic).LastCommittedOffset(ctx, DefaultConsumerGroup, 1)
	require.NoError(t, err)
	require.Equal(t, int64(-1), committed, "the chunk-builder must not commit offsets before checkpoints exist")
}

func TestService_FailsToStartWithoutThePartition(t *testing.T) {
	for _, tc := range []struct {
		name      string
		topic     string
		partition int
		err       string
	}{
		{
			name:      "a topic that does not exist",
			topic:     "does-not-exist",
			partition: 0,
			err:       "failed to list the partitions of the topic",
		},
		{
			name:      "a partition beyond the partitions of the topic",
			topic:     testTopic,
			partition: 5,
			err:       "the topic has no partition 5, only 2 partitions",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := testContext(t)
			_, kafkaCfg := testkafka.CreateCluster(t, 2, testTopic)

			cfg := testConfig(tc.partition)
			cfg.Topic = tc.topic
			s, err := New(kafkaCfg, cfg, prometheus.NewRegistry(), log.NewNopLogger())
			require.NoError(t, err)

			require.Error(t, services.StartAndAwaitRunning(ctx, s))
			require.ErrorContains(t, s.FailureCase(), tc.err)

			// The chunk-builder must not create the topic, since it only consumes.
			topics, err := kadm.NewClient(newTestProducer(t, kafkaCfg)).ListTopics(ctx)
			require.NoError(t, err)
			require.Equal(t, []string{testTopic}, topics.Names())
		})
	}
}

func TestService_ResumesFromTheCommittedOffset(t *testing.T) {
	ctx := testContext(t)
	_, kafkaCfg := testkafka.CreateCluster(t, 1, testTopic)
	producer := newTestProducer(t, kafkaCfg)

	recs := make([]*kgo.Record, 0, 5)
	for range 5 {
		recs = append(recs, flatRecord(t, 0, "tenant-a", `{app="test"}`, "line"))
	}
	produce(t, producer, recs...)

	// The consumer group already holds the records up to offset 2, so the
	// chunk-builder consumes only the records at offsets 3 and 4.
	const group = "test-group"
	require.NoError(t, kafkav2.NewCommitter(kadm.NewClient(producer)).Commit(ctx, testTopic, 0, group, recs[2].Offset))

	cfg := testConfig(0)
	cfg.ConsumerGroup = group
	s, err := New(kafkaCfg, cfg, prometheus.NewRegistry(), log.NewNopLogger())
	require.NoError(t, err)
	require.NoError(t, services.StartAndAwaitRunning(ctx, s))
	t.Cleanup(func() {
		require.NoError(t, services.StopAndAwaitTerminated(context.Background(), s))
	})

	m := s.processor.metrics
	require.Eventually(t, func() bool {
		return testutil.ToFloat64(m.lastOffset) == float64(recs[4].Offset)
	}, 10*time.Second, 10*time.Millisecond)
	require.Equal(t, float64(2), testutil.ToFloat64(m.records))
}

func TestService(t *testing.T) {
	t.Run("starts the consumer from the resume offset", func(t *testing.T) {
		consumer := newFakeConsumer()
		s := newTestService(consumer, newFakeOffsetReader(42, nil))
		ctx := testContext(t)
		require.NoError(t, services.StartAndAwaitRunning(ctx, s))

		require.Equal(t, int64(42), consumer.initialOffset)
		require.Equal(t, services.Running, consumer.State())

		require.NoError(t, services.StopAndAwaitTerminated(ctx, s))
		require.NoError(t, s.FailureCase())
		require.Equal(t, services.Terminated, consumer.State())
	})

	t.Run("fails to start when it cannot read the resume offset", func(t *testing.T) {
		errOffset := errors.New("offset fetch failed")
		consumer := newFakeConsumer()
		s := newTestService(consumer, newFakeOffsetReader(0, errOffset))
		ctx := testContext(t)

		err := services.StartAndAwaitRunning(ctx, s)
		require.Error(t, err)
		require.ErrorIs(t, s.FailureCase(), errOffset)
		require.Equal(t, services.New, consumer.State(), "the consumer must not start without a resume offset")
	})

	t.Run("fails with the error of the consumer", func(t *testing.T) {
		consumer := newFakeConsumer()
		s := newTestService(consumer, fakeOffsetReader{})
		ctx := testContext(t)
		require.NoError(t, services.StartAndAwaitRunning(ctx, s))

		errConsumer := errors.New("consumer failed")
		consumer.stop <- errConsumer

		require.Error(t, s.AwaitTerminated(ctx))
		require.ErrorIs(t, s.FailureCase(), errConsumer)
		// Whichever of the records channel and the watcher running sees first,
		// the error comes from the watcher, which names the consumer.
		require.ErrorContains(t, s.FailureCase(), "service fake-consumer failed")
	})

	t.Run("fails when the consumer stops on its own", func(t *testing.T) {
		consumer := newFakeConsumer()
		s := newTestService(consumer, fakeOffsetReader{})
		ctx := testContext(t)
		require.NoError(t, services.StartAndAwaitRunning(ctx, s))

		consumer.stop <- nil

		require.Error(t, s.AwaitTerminated(ctx))
		require.EqualError(t, s.FailureCase(), "the consumer stopped unexpectedly")
	})
}

// testConfig returns a valid config for the partition.
func testConfig(partition int) Config {
	var cfg Config
	cfg.RegisterFlags(flag.NewFlagSet("test", flag.PanicOnError))
	cfg.Enabled = true
	cfg.Topic = testTopic
	cfg.PartitionID = partition
	return cfg
}

func testContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func newTestProducer(t *testing.T, kafkaCfg kafka.Config) *kgo.Client {
	producer, err := kgo.NewClient(
		kgo.SeedBrokers(kafkaCfg.WriterConfig.Address),
		kgo.RecordPartitioner(kgo.ManualPartitioner()),
	)
	require.NoError(t, err)
	t.Cleanup(producer.Close)
	return producer
}

// produce produces recs in order. It sets the offset of each record.
func produce(t *testing.T, producer *kgo.Client, recs ...*kgo.Record) {
	for _, rec := range recs {
		require.NoError(t, producer.ProduceSync(t.Context(), rec).FirstErr())
	}
}

// flatRecord returns a record in the flat encoding that holds one entry.
func flatRecord(t *testing.T, partition int32, tenant, labels, line string) *kgo.Record {
	recs, err := kafka.EncodeWithTopic(testTopic, partition, tenant, logproto.Stream{
		Labels:  labels,
		Entries: []push.Entry{{Timestamp: time.Unix(0, 1), Line: line}},
	}, 1<<20)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	return recs[0]
}

// nestedRecord returns a record in the nested encoding that holds one entry.
func nestedRecord(t *testing.T, partition int32, tenant, labels, line string) *kgo.Record {
	recs, err := kafka.EncodeInternalWithTopic(testTopic, partition, tenant, logproto.InternalStreamAdapter{
		Labels: labels,
		ResourceLogs: []logproto.ResourceLogs{{
			Attrs: []push.LabelAdapter{{Name: "host", Value: "host-1"}},
			ScopeLogs: []logproto.ScopeLogs{{
				Entries: []push.Entry{{Timestamp: time.Unix(0, 1), Line: line}},
			}},
		}},
	}, 1<<20)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	return recs[0]
}

func newTestService(consumer *fakeConsumer, offsetReader resumeOffsetReader) *Service {
	return newService(
		consumer,
		offsetReader,
		consumer.records,
		newProcessor(newMetrics(prometheus.NewRegistry()), log.NewNopLogger()),
		0,
		DefaultConsumerGroup,
		log.NewNopLogger(),
	)
}

// A fakeConsumer runs until it is stopped or receives from stop. It closes
// records when it stops, as [kafkav2.SinglePartitionConsumer] does.
type fakeConsumer struct {
	*services.BasicService
	records       chan *kgo.Record
	stop          chan error
	initialOffset int64
}

func newFakeConsumer() *fakeConsumer {
	c := &fakeConsumer{
		records: make(chan *kgo.Record),
		stop:    make(chan error, 1),
	}
	c.BasicService = services.NewBasicService(nil, func(ctx context.Context) error {
		defer close(c.records)
		select {
		case <-ctx.Done():
			return nil
		case err := <-c.stop:
			return err
		}
	}, nil).WithName("fake-consumer")
	return c
}

func (c *fakeConsumer) SetInitialOffset(offset int64) error {
	c.initialOffset = offset
	return nil
}

// A fakeOffsetReader serves one partition, 0, and fails every ResumeOffset
// with err if it is not nil.
type fakeOffsetReader struct {
	offset int64
	err    error
}

func newFakeOffsetReader(offset int64, err error) fakeOffsetReader {
	return fakeOffsetReader{offset: offset, err: err}
}

func (r fakeOffsetReader) ResumeOffset(_ context.Context, _ string, _ int32) (int64, error) {
	return r.offset, r.err
}

func (r fakeOffsetReader) EndOffsets(_ context.Context) (map[int32]int64, error) {
	return map[int32]int64{0: r.offset}, nil
}
