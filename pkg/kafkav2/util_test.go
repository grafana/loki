package kafkav2

import (
	"context"
	"sync"
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

type produceNoPromise struct{}

// A recordingProducer records the args of all calls.
type recordingProducer struct {
	calls []any
	mtx   sync.Mutex
}

// While Produce is supposed to be asynchronous, here the promise is called
// synchronously. An optional value produceNoPromise can be passed via ctx
// which tells it to not call the promise.
func (p *recordingProducer) Produce(ctx context.Context, r *kgo.Record, promise func(r *kgo.Record, err error)) {
	p.mtx.Lock()
	p.calls = append(p.calls, []any{"Produce", ctx, r, promise})
	p.mtx.Unlock()
	if promise != nil && ctx.Value(produceNoPromise{}) == nil {
		promise(r, nil)
	}
}

// TryProduce has the same behavior as Produce here.
func (p *recordingProducer) TryProduce(ctx context.Context, r *kgo.Record, promise func(r *kgo.Record, err error)) {
	p.mtx.Lock()
	p.calls = append(p.calls, []any{"TryProduce", ctx, r, promise})
	p.mtx.Unlock()
	if promise != nil && ctx.Value(produceNoPromise{}) == nil {
		promise(r, nil)
	}
}
