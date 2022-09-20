package distributor

import (
	"fmt"
	"sync"

	"github.com/grafana/loki/pkg/logproto"
)

type streamSharder struct {
	mu          sync.RWMutex
	desiredRate int

	ingestedStreamRates map[string]int // TODO: populate this with ingested rates (in bytes) fetched from ingesters API.
}

type unshardableStreamErr struct {
	labels     string
	entriesNum int
	shardNum   int
}

func (u *unshardableStreamErr) Error() string {
	return fmt.Sprintf("couldn't shard stream %s. number of shards (%d) is higher than number of entries (%d)", u.labels, u.shardNum, u.entriesNum)
}

func NewStreamSharder(desiredRateMB int) StreamSharder {
	return &streamSharder{
		ingestedStreamRates: make(map[string]int),
		desiredRate:         desiredRateMB,
	}
}

func (s *streamSharder) ShardCountFor(stream logproto.Stream) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if ingestedRate, ok := s.ingestedStreamRates[stream.Labels]; ok {
		shards := (ingestedRate + stream.Size()) / (s.desiredRate * 1000)
		if shards > len(stream.Entries) {
			return 1, &unshardableStreamErr{labels: stream.Labels, entriesNum: len(stream.Entries), shardNum: shards}
		} else if shards == 0 {
			// 1 shard is enough for the given stream
			return 1, nil
		}
		return shards, nil
	}

	return 1, nil
}

func (s *streamSharder) storeRate(stream logproto.Stream, rate int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ingestedStreamRates[stream.Labels] = rate
}
