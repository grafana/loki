package distributor

import (
	"sync"

	"github.com/grafana/loki/pkg/logproto"
)

type streamSharder struct {
	mu      sync.Mutex
	streams map[string]int
}

func NewStreamSharder() StreamSharder {
	return &streamSharder{
		streams: make(map[string]int),
	}
}

func (s *streamSharder) ShardsFor(stream logproto.Stream) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	shards := s.streams[stream.Labels]
	if shards > 0 {
		return shards, true
	}

	return 0, false
}

func (s *streamSharder) IncreaseShardsFor(stream logproto.Stream) {
	s.mu.Lock()
	defer s.mu.Unlock()

	shards := s.streams[stream.Labels]
	s.streams[stream.Labels] = max(shards*2, 2)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
