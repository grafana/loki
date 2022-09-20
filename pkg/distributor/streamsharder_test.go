package distributor

import (
	"fmt"
	"testing"

	"github.com/grafana/loki/pkg/logproto"

	"github.com/stretchr/testify/require"
)

func TestStreamSharder(t *testing.T) {
	stream := logproto.Stream{Entries: make([]logproto.Entry, 11), Labels: "test-stream"}
	stream2 := logproto.Stream{Entries: make([]logproto.Entry, 11), Labels: "test-stream-2"}

	t.Run("it returns not ok when a stream should not be sharded", func(t *testing.T) {
		sharder := NewStreamSharder(3)

		shards, err := sharder.ShardCountFor(stream)
		require.NoError(t, err)
		require.Equal(t, shards, 1)
	})

	t.Run("it keeps track of multiple streams", func(t *testing.T) {
		sharder := NewStreamSharder(3)
		sharderImpl, ok := sharder.(*streamSharder)
		require.True(t, ok)

		sharderImpl.storeRate(stream, 1000)   // 1 MB
		sharderImpl.storeRate(stream2, 20000) // 20 MB

		shards, err := sharder.ShardCountFor(stream)
		require.NoError(t, err)

		require.Equal(t, 1, shards)

		shards, err = sharder.ShardCountFor(stream2)
		require.NoError(t, err)

		require.Equal(t, 6, shards)
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

func (s *StreamSharderMock) ShardCountFor(stream logproto.Stream) (int, error) {
	s.increaseCallsFor("ShardCountFor")
	if s.wantShards < 0 {
		return 0, fmt.Errorf("unshardable stream")
	}
	return s.wantShards, nil
}

func (s *StreamSharderMock) increaseCallsFor(funcName string) {
	if _, ok := s.calls[funcName]; ok {
		s.calls[funcName]++
		return
	}

	s.calls[funcName] = 1
}
