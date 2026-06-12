package kafkav2

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestNoCancelProducer_Produce(t *testing.T) {
	t.Run("without promise", func(t *testing.T) {
		testCtx := t.Context()

		// recorded records all calls to [Produce]. We use it to check that records
		// are still produced even when the context is canceled.
		recorded := recordingProducer{}
		producer := NewNoCancelProducer(&recorded)

		// Normal behavior. We expect there to be one call and it should have rec1
		// in its args.
		rec1 := &kgo.Record{Topic: "test", Partition: 0, Key: []byte("key1"), Value: []byte("value1")}
		producer.Produce(testCtx, rec1, nil)
		require.Len(t, recorded.calls, 1)
		args, ok := recorded.calls[0].([]any)
		require.True(t, ok)
		require.Equal(t, "Produce", args[0])
		require.Equal(t, rec1, args[2])

		// Canceled context. We expect there to be a second call, even if the context
		// is canceled.
		cancelCtx, cancel := context.WithCancel(testCtx)
		cancel()
		rec2 := &kgo.Record{Topic: "test", Partition: 0, Key: []byte("key2"), Value: []byte("value2")}
		producer.Produce(cancelCtx, rec2, nil)
		require.Len(t, recorded.calls, 2)
		args, ok = recorded.calls[1].([]any)
		require.True(t, ok)
		require.Equal(t, "Produce", args[0])
		require.Equal(t, rec2, args[2])
	})

	t.Run("with promise", func(t *testing.T) {
		testCtx := t.Context()

		// recorded records all calls to [Produce]. We use it to check that records
		// are still produced even when the context is canceled.
		recorded := recordingProducer{}
		producer := NewNoCancelProducer(&recorded)

		// Normal behavior. We expect there to be one call and it should have rec1
		// in its args.
		rec1 := &kgo.Record{Topic: "test", Partition: 0, Key: []byte("key1"), Value: []byte("value1")}
		producer.Produce(testCtx, rec1, func(r *kgo.Record, err error) {
			require.NotNil(t, r)
			require.NoError(t, err)
		})
		require.Len(t, recorded.calls, 1)
		args, ok := recorded.calls[0].([]any)
		require.True(t, ok)
		require.Equal(t, "Produce", args[0])
		require.Equal(t, rec1, args[2])

		// Canceled context. We expect there to be a second call, and it should have
		// rec2 in its args. We use a special context value produceNoPromise
		// which tells the recorder not to call the promise. This lets us ensure the
		// promise is called because of the canceled ctx instead.
		cancelCtx, cancel := context.WithCancel(context.WithValue(testCtx, produceNoPromise{}, true))
		cancel()
		rec2 := &kgo.Record{Topic: "test", Partition: 0, Key: []byte("key2"), Value: []byte("value2")}
		producer.Produce(cancelCtx, rec2, func(r *kgo.Record, err error) {
			require.NotNil(t, r)
			require.EqualError(t, err, "context canceled")
		})
		require.Len(t, recorded.calls, 2)
		args, ok = recorded.calls[1].([]any)
		require.True(t, ok)
		require.Equal(t, "Produce", args[0])
		require.Equal(t, rec2, args[2])
	})
}

func TestNoCancelProducer_TryProduce(t *testing.T) {
	t.Run("without promise", func(t *testing.T) {
		testCtx := t.Context()

		// recorded records all calls to [Produce]. We use it to check that records
		// are still produced even when the context is canceled.
		recorded := recordingProducer{}
		producer := NewNoCancelProducer(&recorded)

		// Normal behavior. We expect there to be one call and it should have rec1
		// in its args.
		rec1 := &kgo.Record{Topic: "test", Partition: 0, Key: []byte("key1"), Value: []byte("value1")}
		producer.TryProduce(testCtx, rec1, nil)
		require.Len(t, recorded.calls, 1)
		args, ok := recorded.calls[0].([]any)
		require.True(t, ok)
		require.Equal(t, "TryProduce", args[0])
		require.Equal(t, rec1, args[2])

		// Canceled context. We expect there to be a second call, even if the context
		// is canceled.
		cancelCtx, cancel := context.WithCancel(testCtx)
		cancel()
		rec2 := &kgo.Record{Topic: "test", Partition: 0, Key: []byte("key2"), Value: []byte("value2")}
		producer.TryProduce(cancelCtx, rec2, nil)
		require.Len(t, recorded.calls, 2)
		args, ok = recorded.calls[1].([]any)
		require.True(t, ok)
		require.Equal(t, "TryProduce", args[0])
		require.Equal(t, rec2, args[2])
	})

	t.Run("with promise", func(t *testing.T) {
		testCtx := t.Context()

		// recorded records all calls to [Produce]. We use it to check that records
		// are still produced even when the context is canceled.
		recorded := recordingProducer{}
		producer := NewNoCancelProducer(&recorded)

		// Normal behavior. We expect there to be one call and it should have rec1
		// in its args.
		rec1 := &kgo.Record{Topic: "test", Partition: 0, Key: []byte("key1"), Value: []byte("value1")}
		producer.TryProduce(testCtx, rec1, func(r *kgo.Record, err error) {
			require.NotNil(t, r)
			require.NoError(t, err)
		})
		require.Len(t, recorded.calls, 1)
		args, ok := recorded.calls[0].([]any)
		require.True(t, ok)
		require.Equal(t, "TryProduce", args[0])
		require.Equal(t, rec1, args[2])

		// Canceled context. We expect there to be a second call, and it should have
		// rec2 in its args. We use a special context value produceNoPromise
		// which tells the recorder not to call the promise. This lets us ensure the
		// promise is called because of the canceled ctx instead.
		cancelCtx, cancel := context.WithCancel(context.WithValue(testCtx, produceNoPromise{}, true))
		cancel()
		rec2 := &kgo.Record{Topic: "test", Partition: 0, Key: []byte("key2"), Value: []byte("value2")}
		producer.TryProduce(cancelCtx, rec2, func(r *kgo.Record, err error) {
			require.NotNil(t, r)
			require.EqualError(t, err, "context canceled")
		})
		require.Len(t, recorded.calls, 2)
		args, ok = recorded.calls[1].([]any)
		require.True(t, ok)
		require.Equal(t, "TryProduce", args[0])
		require.Equal(t, rec2, args[2])
	})
}

func TestNoCancelProducer_ProduceSync(t *testing.T) {
	testCtx := t.Context()

	// recorded records all calls to [Produce]. We use it to check that records
	// are still produced even when the context is canceled.
	recorded := recordingProducer{}
	producer := NewNoCancelProducer(&recorded)

	// Must be able to produce with no records.
	results := producer.ProduceSync(testCtx)
	require.Len(t, results, 0)

	// Normal behavior. We expect there to be one call and it should have rec1
	// in its args.
	rec1 := &kgo.Record{Topic: "test", Partition: 0, Key: []byte("key1"), Value: []byte("value1")}
	results = producer.ProduceSync(testCtx, rec1)
	require.Len(t, results, 1)
	require.Equal(t, rec1, results[0].Record)
	require.Nil(t, results[0].Err)
	require.Len(t, recorded.calls, 1)
	args, ok := recorded.calls[0].([]any)
	require.True(t, ok)
	require.Equal(t, rec1, args[2])

	// Canceled context. We expect there to be a seond call, and it should have
	// rec2 in its args. We use a special context value produceNoPromise
	// which tells the recorder not to call the promise. This lets us ensure the
	// promise is called because of the canceled ctx instead.
	cancelCtx, cancel := context.WithCancel(context.WithValue(testCtx, produceNoPromise{}, true))
	cancel()
	rec2 := &kgo.Record{Topic: "test", Partition: 0, Key: []byte("key2"), Value: []byte("value2")}
	results = producer.ProduceSync(cancelCtx, rec2)
	require.Len(t, results, 1)
	require.Equal(t, rec2, results[0].Record)
	require.EqualError(t, results[0].Err, "context canceled")
	require.Len(t, recorded.calls, 2)
	args, ok = recorded.calls[1].([]any)
	require.True(t, ok)
	require.Equal(t, rec2, args[2])
}
