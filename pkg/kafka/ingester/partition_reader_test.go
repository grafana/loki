package ingester

import (
	"context"
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

func TestPartitionReader(t *testing.T) {
	_, kafkaCfg := testkafka.CreateCluster(t, 1, "test-topic")
	consumer := newMockConsumer()
	partitionReader, err := NewPartitionReader(kafkaCfg, 0, "test-consumer-group", consumer, log.NewNopLogger(), prometheus.DefaultRegisterer)
	require.NoError(t, err)
	producer, err := kafka.NewWriterClient(kafkaCfg, 100, log.NewNopLogger(), prometheus.DefaultRegisterer)
	require.NoError(t, err)

	services.StartAndAwaitRunning(context.Background(), partitionReader)

	stream := logproto.Stream{
		Labels:  labels.FromStrings("foo", "bar").String(),
		Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "test"}},
	}
	encoder := kafka.NewEncoder()

	records, err := encoder.Encode(0, "test-tenant", stream)
	require.NoError(t, err)
	require.Len(t, records, 1)

	consumer.On("Consume", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	consumer.On("Flush", mock.Anything).Return(nil)

	producer.ProduceSync(context.Background(), records...)
	producer.ProduceSync(context.Background(), records...)

	require.Eventually(t, func() bool {
		return partitionReader.lastFetchOffset == 1
	}, 10*time.Second, 1*time.Second)

	err = services.StopAndAwaitTerminated(context.Background(), partitionReader)
	require.NoError(t, err)
	consumer.AssertExpectations(t)
}
