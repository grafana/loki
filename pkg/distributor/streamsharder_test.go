package distributor

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/logproto"
)

type StreamSharderMock struct {
	wantShards int
}

func NewStreamSharderMock(shards int) *StreamSharderMock {
	return &StreamSharderMock{
		wantShards: shards,
	}
}

func (s *StreamSharderMock) ShardCountFor(*logproto.Stream, int, RateStore) (int, error) {
	if s.wantShards < 0 {
		return 0, fmt.Errorf("unshardable stream")
	}
	return s.wantShards, nil
}
