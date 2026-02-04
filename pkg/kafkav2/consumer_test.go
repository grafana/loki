package kafkav2

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestSinglePartitionConsumer(t *testing.T) {
	const testTopic = "test-topic"
	ctx := t.Context()
	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(1, testTopic))
	require.NoError(t, err)
	t.Cleanup(cluster.Close)

	// Produce some records to be consumed.
	client := mustKafkaClient(t, cluster.ListenAddrs()[0])
	res1 := client.ProduceSync(ctx, &kgo.Record{Topic: testTopic, Key: []byte("key1"), Value: []byte("value1")})
	require.NoError(t, res1.FirstErr())
	res2 := client.ProduceSync(ctx, &kgo.Record{Topic: testTopic, Key: []byte("key2"), Value: []byte("value2")})
	require.NoError(t, res2.FirstErr())

	// Set up the consumer.
	dst := make(chan *kgo.Record)
	consumer := NewSinglePartitionConsumer(client, testTopic, 0, -2, dst, log.NewNopLogger(), prometheus.NewRegistry())
	cancelCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	go consumer.Run(cancelCtx) //nolint:errcheck

	// Wait for the expected number of records to arrive.
	var records []*kgo.Record
	for len(records) < 2 {
		select {
		case <-cancelCtx.Done():
			t.Fatal("context canceled before all records received")
		case record := <-dst:
			records = append(records, record)
		}
	}

	// Check that the records are as expected.
	require.Len(t, records, 2)
	require.Equal(t, []byte("value1"), records[0].Value)
	require.Equal(t, []byte("value2"), records[1].Value)
}
