package distributor

import (
	"math"
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

func (s *streamSharder) ShardsFor(stream logproto.Stream) (ShardStats, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	shards := s.streams[stream.Labels]
	if shards > 0 {
		return NewShardStats(stream, shards), true
	}

	return nil, false
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

type shardIter struct {
	numShards    int
	numEntries   int
	batchSize    int
	currentIndex int
}

func NewShardStats(stream logproto.Stream, numShards int) ShardStats {
	numEntries := len(stream.Entries)
	batchSize := float64(0)
	if numShards > 0 {
		batchSize = math.Ceil(float64(numEntries) / float64(numShards))
	}

	return &shardIter{
		numShards:  numShards,
		numEntries: numEntries,
		batchSize:  int(batchSize),
	}
}

func (s *shardIter) NumShards() int {
	return s.numShards
}

func (s *shardIter) NextShardID() (int, bool) {
	if s.currentIndex < s.numEntries {
		val := 0
		if s.batchSize > 0 {
			val = s.currentIndex / s.batchSize
		}
		s.currentIndex++
		return val, true
	}
	return 0, false
}
