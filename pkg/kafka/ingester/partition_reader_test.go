package ingester

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/testkafka"
	"github.com/grafana/loki/v3/pkg/logproto"
)

type mockConsumer struct {
	mock.Mock
}

func newMockConsumer() *mockConsumer {
	return &mockConsumer{}
}

func (m *mockConsumer) Consume(ctx context.Context, partitionID int32, records []record) error {
	args := m.Called(ctx, partitionID, records)
	return args.Error(0)
}

func (m *mockConsumer) Flush(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func TestPartitionReader_BasicFunctionality(t *testing.T) {
	_, kafkaCfg := testkafka.CreateCluster(t, 1, "test-topic")
	consumer := newMockConsumer()
	partitionReader, err := NewPartitionReader(kafkaCfg, 0, "test-consumer-group", 10*time.Second, consumer, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	producer, err := kafka.NewWriterClient(kafkaCfg, 100, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)

	err = services.StartAndAwaitRunning(context.Background(), partitionReader)
	require.NoError(t, err)

	stream := logproto.Stream{
		Labels:  labels.FromStrings("foo", "bar").String(),
		Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "test"}},
	}
	encoder := kafka.NewEncoder()

	records, err := encoder.Encode(0, "test-tenant", stream, 10<<20)
	require.NoError(t, err)
	require.Len(t, records, 1)

	consumer.On("Consume", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	consumer.On("Flush", mock.Anything).Return(nil)

	producer.ProduceSync(context.Background(), records...)
	producer.ProduceSync(context.Background(), records...)

	require.Eventually(t, func() bool {
		return consumer.AssertNumberOfCalls(t, "Consume", 2)
	}, 10*time.Second, 1*time.Second)

	err = services.StopAndAwaitTerminated(context.Background(), partitionReader)
	require.NoError(t, err)
	consumer.AssertExpectations(t)
}

func TestPartitionReader_ConsumerError(t *testing.T) {
	_, kafkaCfg := testkafka.CreateCluster(t, 1, "test-topic")
	consumer := newMockConsumer()
	partitionReader, err := NewPartitionReader(kafkaCfg, 0, "test-consumer-group", 10*time.Minute, consumer, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	producer, err := kafka.NewWriterClient(kafkaCfg, 100, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)

	err = services.StartAndAwaitRunning(context.Background(), partitionReader)
	require.NoError(t, err)

	stream := logproto.Stream{
		Labels:  labels.FromStrings("foo", "bar").String(),
		Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "test"}},
	}
	encoder := kafka.NewEncoder()

	records, err := encoder.Encode(0, "test-tenant", stream, 10<<20)
	require.NoError(t, err)

	consumerError := errors.New("consumer error")
	consumer.On("Consume", mock.Anything, mock.Anything, mock.Anything).Return(consumerError).Once()
	consumer.On("Consume", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	consumer.On("Flush", mock.Anything).Return(nil)

	producer.ProduceSync(context.Background(), records...)

	require.Eventually(t, func() bool {
		return consumer.AssertNumberOfCalls(t, "Consume", 2)
	}, 10*time.Second, 1*time.Second)

	err = services.StopAndAwaitTerminated(context.Background(), partitionReader)
	require.NoError(t, err)
	consumer.AssertExpectations(t)
}

func TestPartitionReader_FlushAndCommit(t *testing.T) {
	_, kafkaCfg := testkafka.CreateCluster(t, 1, "test-topic")
	consumer := newMockConsumer()
	flushInterval := 200 * time.Millisecond // Set a short flush interval for testing

	reg := prometheus.NewRegistry()
	partitionReader, err := NewPartitionReader(kafkaCfg, 0, "test-consumer-group", flushInterval, consumer, log.NewNopLogger(), reg)
	require.NoError(t, err)

	producer, err := kafka.NewWriterClient(kafkaCfg, 100, log.NewNopLogger(), reg)
	require.NoError(t, err)

	err = services.StartAndAwaitRunning(context.Background(), partitionReader)
	require.NoError(t, err)

	stream := logproto.Stream{
		Labels:  labels.FromStrings("foo", "bar").String(),
		Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "test"}},
	}
	encoder := kafka.NewEncoder()

	records, err := encoder.Encode(0, "test-tenant", stream, 10<<20)
	require.NoError(t, err)

	consumer.On("Consume", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	consumer.On("Flush", mock.Anything).Return(nil)

	_ = producer.ProduceSync(context.Background(), records...)
	_ = producer.ProduceSync(context.Background(), records...)

	require.Eventually(t, func() bool {
		return consumer.AssertNumberOfCalls(t, "Consume", 2)
	}, 10*time.Second, 1*time.Second)

	err = services.StopAndAwaitTerminated(context.Background(), partitionReader)
	require.NoError(t, err)
	// Wait for the message to be consumed and committed
	require.Eventually(t, func() bool {
		metrics, _ := reg.Gather()
		for _, metric := range metrics {
			if metric.GetName() == "loki_ingest_storage_reader_last_committed_offset" {
				return *metric.Metric[0].Gauge.Value == 1
			}
		}
		return false
	}, 5*time.Second, 100*time.Millisecond)

	// Check commit request metrics
	var commitRequestsTotal float64
	var commitFailuresTotal float64
	var lastCommittedOffset float64

	metrics, _ := reg.Gather()
	for _, metric := range metrics {
		switch metric.GetName() {
		case "loki_ingest_storage_reader_offset_commit_requests_total":
			commitRequestsTotal = *metric.Metric[0].Counter.Value
		case "loki_ingest_storage_reader_offset_commit_failures_total":
			commitFailuresTotal = *metric.Metric[0].Counter.Value
		case "loki_ingest_storage_reader_last_committed_offset":
			lastCommittedOffset = *metric.Metric[0].Gauge.Value
		}
	}

	require.Equal(t, commitRequestsTotal, 1.0)
	require.Equal(t, 0.0, commitFailuresTotal)
	require.Equal(t, 1.0, lastCommittedOffset)

	// Check commit latency metric
	var commitLatencyObserved bool
	for _, metric := range metrics {
		if metric.GetName() == "loki_ingest_storage_reader_offset_commit_request_duration_seconds" {
			commitLatencyObserved = true
			break
		}
	}
	require.True(t, commitLatencyObserved)

	consumer.AssertExpectations(t)
}

func TestPartitionReader_MultipleRecords(t *testing.T) {
	_, kafkaCfg := testkafka.CreateCluster(t, 1, "test-topic")
	consumer := newMockConsumer()
	partitionReader, err := NewPartitionReader(kafkaCfg, 0, "test-consumer-group", 10*time.Second, consumer, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	producer, err := kafka.NewWriterClient(kafkaCfg, 100, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)

	err = services.StartAndAwaitRunning(context.Background(), partitionReader)
	require.NoError(t, err)

	stream1 := logproto.Stream{
		Labels:  labels.FromStrings("foo", "bar").String(),
		Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "test1"}},
	}
	stream2 := logproto.Stream{
		Labels:  labels.FromStrings("foo", "baz").String(),
		Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "test2"}},
	}
	encoder := kafka.NewEncoder()

	records1, err := encoder.Encode(0, "test-tenant", stream1, 10<<20)
	require.NoError(t, err)
	records2, err := encoder.Encode(0, "test-tenant", stream2, 10<<20)
	require.NoError(t, err)

	consumer.On("Consume", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	consumer.On("Flush", mock.Anything).Return(nil)

	producer.ProduceSync(context.Background(), records1...)
	producer.ProduceSync(context.Background(), records2...)

	require.Eventually(t, func() bool {
		return partitionReader.lastProcessedOffset == 1
	}, 10*time.Second, 1*time.Second)

	consumer.AssertNumberOfCalls(t, "Consume", 2)

	err = services.StopAndAwaitTerminated(context.Background(), partitionReader)
	require.NoError(t, err)
	consumer.AssertExpectations(t)
}
