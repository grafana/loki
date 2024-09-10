package ingester

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/testkafka"
	"github.com/grafana/loki/v3/pkg/logproto"
)

type mockConsumer struct {
	mock.Mock
	recordsChan chan []record
	wg          sync.WaitGroup
}

func newMockConsumer() *mockConsumer {
	return &mockConsumer{
		recordsChan: make(chan []record, 100),
	}
}

func (m *mockConsumer) Start(ctx context.Context, recordsChan <-chan []record) func() {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case records := <-recordsChan:
				m.recordsChan <- records
			}
		}
	}()
	return m.wg.Wait
}

func (m *mockConsumer) Flush(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func TestPartitionReader_BasicFunctionality(t *testing.T) {
	_, kafkaCfg := testkafka.CreateCluster(t, 1, "test-topic")
	consumer := newMockConsumer()

	consumerFactory := func(committer Committer) (Consumer, error) {
		return consumer, nil
	}

	partitionReader, err := NewPartitionReader(kafkaCfg, 0, "test-consumer-group", consumerFactory, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	producer, err := kafka.NewWriterClient(kafkaCfg, 100, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)

	err = services.StartAndAwaitRunning(context.Background(), partitionReader)
	require.NoError(t, err)

	stream := logproto.Stream{
		Labels:  labels.FromStrings("foo", "bar").String(),
		Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "test"}},
	}

	records, err := kafka.Encode(0, "test-tenant", stream, 10<<20)
	require.NoError(t, err)
	require.Len(t, records, 1)

	producer.ProduceSync(context.Background(), records...)
	producer.ProduceSync(context.Background(), records...)

	// Wait for records to be processed
	assert.Eventually(t, func() bool {
		return len(consumer.recordsChan) == 2
	}, 10*time.Second, 100*time.Millisecond)

	// Verify the records
	for i := 0; i < 2; i++ {
		select {
		case receivedRecords := <-consumer.recordsChan:
			require.Len(t, receivedRecords, 1)
			assert.Equal(t, "test-tenant", receivedRecords[0].tenantID)
			assert.Equal(t, records[0].Value, receivedRecords[0].content)
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for records")
		}
	}

	err = services.StopAndAwaitTerminated(context.Background(), partitionReader)
	require.NoError(t, err)
}
