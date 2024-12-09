package partition

import (
	"context"
	"fmt"
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
	"github.com/twmb/franz-go/pkg/kadm"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/kafka/testkafka"
	"github.com/grafana/loki/v3/pkg/logproto"
)

type mockConsumer struct {
	mock.Mock
	recordsChan chan []Record
	wg          sync.WaitGroup
}

func newMockConsumer() *mockConsumer {
	return &mockConsumer{
		recordsChan: make(chan []Record, 100),
	}
}

func (m *mockConsumer) Start(ctx context.Context, recordsChan <-chan []Record) func() {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case records, ok := <-recordsChan:
				if !ok {
					return
				}
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

func readersFromKafkaCfg(
	t *testing.T,
	kafkaCfg kafka.Config,
	consumerFactory ConsumerFactory,
	partition int32,
) (Reader, *ReaderService) {
	partitionReader, err := NewReaderService(
		kafkaCfg,
		partition,
		"test-consumer-group",
		consumerFactory,
		log.NewNopLogger(),
		nil,
	)
	require.NoError(t, err)

	// Get the underlying reader from the service
	return partitionReader.reader, partitionReader
}

func TestPartitionReader_BasicFunctionality(t *testing.T) {
	_, kafkaCfg := testkafka.CreateCluster(t, 1, "test")
	consumer := newMockConsumer()

	consumerFactory := func(_ Committer, _ log.Logger) (Consumer, error) {
		return consumer, nil
	}

	_, partitionReader := readersFromKafkaCfg(
		t,
		kafkaCfg,
		consumerFactory,
		0,
	)

	producer, err := client.NewWriterClient(kafkaCfg, 100, log.NewNopLogger(), prometheus.NewRegistry())
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

	require.NoError(t, producer.ProduceSync(context.Background(), records...).FirstErr())
	require.NoError(t, producer.ProduceSync(context.Background(), records...).FirstErr())

	// Wait for records to be processed
	assert.Eventually(t, func() bool {
		return len(consumer.recordsChan) == 2
	}, 10*time.Second, 100*time.Millisecond)

	// Verify the records
	for i := 0; i < 2; i++ {
		select {
		case receivedRecords := <-consumer.recordsChan:
			require.Len(t, receivedRecords, 1)
			assert.Equal(t, "test-tenant", receivedRecords[0].TenantID)
			assert.Equal(t, records[0].Value, receivedRecords[0].Content)
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for records")
		}
	}

	err = services.StopAndAwaitTerminated(context.Background(), partitionReader)
	require.NoError(t, err)
}

func TestPartitionReader_ProcessCatchUpAtStartup(t *testing.T) {
	_, kafkaCfg := testkafka.CreateCluster(t, 1, "test-topic")
	var consumerStarting *mockConsumer

	consumerFactory := func(_ Committer, _ log.Logger) (Consumer, error) {
		// Return two consumers to ensure we are processing requests during service `start()` and not during `run()`.
		if consumerStarting == nil {
			consumerStarting = newMockConsumer()
			return consumerStarting, nil
		}
		return newMockConsumer(), nil
	}

	_, partitionReader := readersFromKafkaCfg(
		t,
		kafkaCfg,
		consumerFactory,
		0,
	)

	producer, err := client.NewWriterClient(kafkaCfg, 100, log.NewNopLogger(), prometheus.NewRegistry())
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

	// Enable the catch up logic so starting the reader will read any existing records.
	kafkaCfg.MaxConsumerLagAtStartup = time.Second * 2

	err = services.StartAndAwaitRunning(context.Background(), partitionReader)
	require.NoError(t, err)

	// This message should not be processed by the startingConsumer
	producer.ProduceSync(context.Background(), records...)

	// Wait for records to be processed
	require.Eventually(t, func() bool {
		return len(consumerStarting.recordsChan) == 1 // All pending messages will be received in one batch
	}, 10*time.Second, 10*time.Millisecond)

	receivedRecords := <-consumerStarting.recordsChan
	require.Len(t, receivedRecords, 2)
	assert.Equal(t, "test-tenant", receivedRecords[0].TenantID)
	assert.Equal(t, records[0].Value, receivedRecords[0].Content)
	assert.Equal(t, "test-tenant", receivedRecords[1].TenantID)
	assert.Equal(t, records[0].Value, receivedRecords[1].Content)

	assert.Equal(t, 0, len(consumerStarting.recordsChan))

	err = services.StopAndAwaitTerminated(context.Background(), partitionReader)
	require.NoError(t, err)
}

func TestPartitionReader_ProcessCommits(t *testing.T) {
	_, kafkaCfg := testkafka.CreateCluster(t, 1, "test-topic")
	consumer := newMockConsumer()

	consumerFactory := func(_ Committer, _ log.Logger) (Consumer, error) {
		return consumer, nil
	}

	partitionID := int32(0)
	partitionReader, readerSvc := readersFromKafkaCfg(
		t,
		kafkaCfg,
		consumerFactory,
		partitionID,
	)

	producer, err := client.NewWriterClient(kafkaCfg, 100, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)

	// Init the client: This usually happens in "start" but we want to manage our own lifecycle for this test.
	partitionReader.SetOffsetForConsumption(int64(KafkaStartOffset))

	stream := logproto.Stream{
		Labels:  labels.FromStrings("foo", "bar").String(),
		Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "test"}},
	}

	records, err := kafka.Encode(partitionID, "test-tenant", stream, 10<<20)
	require.NoError(t, err)
	require.Len(t, records, 1)

	ctx, cancel := context.WithDeadlineCause(context.Background(), time.Now().Add(10*time.Second), fmt.Errorf("test unexpectedly deadlocked"))
	recordsChan := make(chan []Record)
	wait := consumer.Start(ctx, recordsChan)

	targetLag := time.Second

	i := -1
	iterations := 5
	producer.ProduceSync(context.Background(), records...)
	// timeSince acts as a hook for when we check if we've honoured the lag or not. We modify it to respond "no" initially, to force a re-loop, and then "yes" after `iterations`.
	// We also inject a new kafka record each time so there is more to consume.
	timeSince := func(time.Time) time.Duration {
		i++
		if i < iterations {
			producer.ProduceSync(context.Background(), records...)
			return targetLag + 1
		}
		return targetLag - 1
	}

	_, err = readerSvc.fetchUntilLagSatisfied(ctx, targetLag, log.NewNopLogger(), recordsChan, timeSince)
	assert.NoError(t, err)

	// Wait to process all the records
	cancel()
	wait()

	close(recordsChan)
	close(consumer.recordsChan)
	recordsCount := 0
	for receivedRecords := range consumer.recordsChan {
		recordsCount += len(receivedRecords)
	}
	// We expect to have processed all the records, including initial + one per iteration.
	assert.Equal(t, iterations+1, recordsCount)
}

func TestPartitionReader_StartsAtNextOffset(t *testing.T) {
	kaf, kafkaCfg := testkafka.CreateCluster(t, 1, "test")
	consumer := newMockConsumer()

	kaf.CurrentNode()
	consumerFactory := func(_ Committer, _ log.Logger) (Consumer, error) {
		return consumer, nil
	}

	// Produce some records
	producer, err := client.NewWriterClient(kafkaCfg, 100, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	stream := logproto.Stream{
		Labels: labels.FromStrings("foo", "bar").String(),
	}
	for i := 0; i < 5; i++ {
		stream.Entries = []logproto.Entry{{Timestamp: time.Now(), Line: fmt.Sprintf("test-%d", i)}}
		records, err := kafka.Encode(0, "test-tenant", stream, 10<<20)
		require.NoError(t, err)
		require.Len(t, records, 1)

		producer.ProduceSync(context.Background(), records...)
	}

	// Set our offset part way through the records we just produced
	offset := int64(1)
	kafkaClient, err := client.NewReaderClient(kafkaCfg, nil, log.NewNopLogger())
	require.NoError(t, err)
	admClient := kadm.NewClient(kafkaClient)
	toCommit := kadm.Offsets{}
	toCommit.AddOffset(kafkaCfg.Topic, 0, offset, -1)
	resp, err := admClient.CommitOffsets(context.Background(), "test-consumer-group", toCommit)
	require.NoError(t, err)
	require.NoError(t, resp.Error())

	// Start reading
	_, partitionReader := readersFromKafkaCfg(
		t,
		kafkaCfg,
		consumerFactory,
		0,
	)
	err = services.StartAndAwaitRunning(context.Background(), partitionReader)
	require.NoError(t, err)

	// Wait for records to be processed
	require.Eventually(t, func() bool {
		return len(consumer.recordsChan) == 1 // All pending messages will be received in one batch
	}, 10*time.Second, 10*time.Millisecond)

	// Check we only received records from the last commit onwards, and the last committed offset is not reprocessed.
	receivedRecords := <-consumer.recordsChan
	require.Len(t, receivedRecords, 3) // Offsets are 0 based, so we should read offsets 2,3,4
	for _, record := range receivedRecords {
		assert.NotContainsf(t, record.Content, "test-0", "record %q should not contain test-0", record.Content)
		assert.NotContainsf(t, record.Content, "test-1", "record %q should not contain test-1", record.Content)
	}

	err = services.StopAndAwaitTerminated(context.Background(), partitionReader)
	require.NoError(t, err)
}

func TestPartitionReader_StartsUpIfNoNewRecordsAreAvailable(t *testing.T) {
	kaf, kafkaCfg := testkafka.CreateCluster(t, 1, "test")
	consumer := newMockConsumer()

	kaf.CurrentNode()
	consumerFactory := func(_ Committer, _ log.Logger) (Consumer, error) {
		return consumer, nil
	}

	// Produce some records
	producer, err := client.NewWriterClient(kafkaCfg, 100, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	stream := logproto.Stream{
		Labels: labels.FromStrings("foo", "bar").String(),
	}
	for i := 0; i < 5; i++ {
		stream.Entries = []logproto.Entry{{Timestamp: time.Now(), Line: fmt.Sprintf("test-%d", i)}}
		records, err := kafka.Encode(0, "test-tenant", stream, 10<<20)
		require.NoError(t, err)
		require.Len(t, records, 1)

		producer.ProduceSync(context.Background(), records...)
	}

	// Set our offset to the last record produced
	offset := int64(4)
	kafkaClient, err := client.NewReaderClient(kafkaCfg, nil, log.NewNopLogger())
	require.NoError(t, err)
	admClient := kadm.NewClient(kafkaClient)
	toCommit := kadm.Offsets{}
	toCommit.AddOffset(kafkaCfg.Topic, 0, offset, -1)
	resp, err := admClient.CommitOffsets(context.Background(), "test-consumer-group", toCommit)
	require.NoError(t, err)
	require.NoError(t, resp.Error())

	// Start reading
	_, partitionReader := readersFromKafkaCfg(
		t,
		kafkaCfg,
		consumerFactory,
		0,
	)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = services.StartAndAwaitRunning(ctx, partitionReader)
	require.NoError(t, err)

	// Check we didn't receive any records: This is a sanity check. We shouldn't get this far if we deadlock during startup.
	require.Len(t, consumer.recordsChan, 0)

	err = services.StopAndAwaitTerminated(context.Background(), partitionReader)
	require.NoError(t, err)
}
