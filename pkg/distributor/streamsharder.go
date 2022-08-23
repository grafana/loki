package distributor

import (
	"sync"

	"github.com/grafana/loki/pkg/logproto"
)

type streamSharder struct {
	mu      sync.RWMutex
	streams map[string]int
}

func NewStreamSharder() StreamSharder {
	return &streamSharder{
		streams: make(map[string]int),
	}
}

func (s *streamSharder) ShardsFor(stream logproto.Stream) (int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	shards := s.streams[stream.Labels]
	if shards > 0 {
		return shards, true
	}

	return 0, false
}

// IncreaseShardsFor shards the given stream by doubling its number of shards.
func (s *streamSharder) IncreaseShardsFor(stream logproto.Stream) {
	s.mu.Lock()
	defer s.mu.Unlock()

	shards := s.streams[stream.Labels]

	// Ensure the number of shards is at least 2
	s.streams[stream.Labels] = max(shards*2, 2)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
