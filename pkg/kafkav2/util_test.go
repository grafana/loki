package kafkav2

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

// mustKafkaClient returns a new Kafka client for tests. It fails the test
// if an error occurs.
func mustKafkaClient(t *testing.T, seed string, opts ...kgo.Opt) *kgo.Client {
	clientOpts := []kgo.Opt{
		kgo.SeedBrokers(seed),
		kgo.AllowAutoTopicCreation(),
		// We will choose the partition of each record.
		kgo.RecordPartitioner(kgo.ManualPartitioner()),
	}
	clientOpts = append(clientOpts, opts...)
	client, err := kgo.NewClient(clientOpts...)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	return client
}
