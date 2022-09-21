package distributor

import (
	"fmt"
	"sync"

	"github.com/grafana/loki/pkg/logproto"
)

type shardStore struct {
	mu     sync.RWMutex
	shards map[string]int
}

func NewShardStore() ShardStore {
	return &shardStore{
		shards: make(map[string]int),
	}
}

func (s *shardStore) ShardsFor(stream logproto.Stream) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if rate, ok := s.shards[stream.Labels]; ok {
		return rate, nil
	}
	return 0, fmt.Errorf("no shards for stream %s", stream.Labels)
}

func (s *shardStore) StoreShards(stream logproto.Stream, shardCnt int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.shards[stream.Labels] = shardCnt
	return nil
}
