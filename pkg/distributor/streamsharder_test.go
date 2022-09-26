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

		shards, ok := sharder.ShardCountFor(stream)
		require.Equal(t, shards, 0)
		require.False(t, ok)
	})

	t.Run("it keeps track of multiple streams", func(t *testing.T) {
		sharder := NewStreamSharder()
		sharder.IncreaseShardsFor(stream)
		sharder.IncreaseShardsFor(stream)
		sharder.IncreaseShardsFor(stream2)

		shards, ok := sharder.ShardCountFor(stream)
		require.True(t, ok)

		require.Equal(t, 4, shards)

		shards, ok = sharder.ShardCountFor(stream2)
		require.True(t, ok)

		require.Equal(t, 2, shards)
	})
}

type StreamSharderMock struct {
	calls map[string]int

	wantShards int
}

func NewStreamSharderMock(shards int) *StreamSharderMock {
	return &StreamSharderMock{
		calls:      make(map[string]int),
		wantShards: shards,
	}
}

func (s *StreamSharderMock) IncreaseShardsFor(stream logproto.Stream) {
	s.increaseCallsFor("IncreaseShardsFor")
}

func (s *StreamSharderMock) ShardCountFor(stream logproto.Stream) (int, bool) {
	s.increaseCallsFor("ShardCountFor")
	if s.wantShards < 0 {
		return 0, false
	}
	return s.wantShards, true
}

func (s *StreamSharderMock) increaseCallsFor(funcName string) {
	if _, ok := s.calls[funcName]; ok {
		s.calls[funcName]++
		return
	}

	s.calls[funcName] = 1
}
