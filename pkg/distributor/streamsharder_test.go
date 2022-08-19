package distributor

import (
	"testing"

	"github.com/grafana/loki/pkg/logproto"

	"github.com/stretchr/testify/require"
)

func TestStreamSharder(t *testing.T) {
	stream := logproto.Stream{Entries: make([]logproto.Entry, 11), Labels: "test-stream"}
	stream2 := logproto.Stream{Entries: make([]logproto.Entry, 11), Labels: "test-stream-2"}

	t.Run("it returns not ok when a stream should not be sharded", func(t *testing.T) {
		sharder := NewStreamSharder()

		iter, ok := sharder.ShardsFor(stream)
		require.Nil(t, iter)
		require.False(t, ok)
	})

	t.Run("it keeps track of multiple streams", func(t *testing.T) {
		sharder := NewStreamSharder()
		sharder.IncreaseShardsFor(stream)
		sharder.IncreaseShardsFor(stream)
		sharder.IncreaseShardsFor(stream2)

		iter, ok := sharder.ShardsFor(stream)
		require.True(t, ok)

		require.Equal(t, 4, iter.NumShards())

		iter, ok = sharder.ShardsFor(stream2)
		require.True(t, ok)

		require.Equal(t, 2, iter.NumShards())
	})
}

func TestShardIter(t *testing.T) {
	stream := logproto.Stream{Entries: make([]logproto.Entry, 11)}

	t.Run("it returns indices for batching entries in a stream", func(t *testing.T) {
		iter := NewShardIter(stream, 3)

		var indices []int
		for idx, cont := iter.NextShardID(); cont; idx, cont = iter.NextShardID() {
			indices = append(indices, idx)
		}

		require.Equal(t, []int{0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2}, indices)
	})

	t.Run("it returns the same index when numShards is 0", func(t *testing.T) {
		iter := NewShardIter(stream, 0)

		var indices []int
		for idx, cont := iter.NextShardID(); cont; idx, cont = iter.NextShardID() {
			indices = append(indices, idx)
		}

		require.Equal(t, []int{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, indices)
	})
}
