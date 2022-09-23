package distributor

import (
	"fmt"
	"sync"

	"github.com/grafana/loki/pkg/logproto"
)

type rateStore struct {
	mu    sync.RWMutex
	rates map[string]int // TODO: populate this with ingested rates (in bytes) fetched from ingesters API.
}

type unshardableStreamErr struct {
	labels     string
	entriesNum int
	shardNum   int
}

func (u *unshardableStreamErr) Error() string {
	return fmt.Sprintf("couldn't shard stream %s. number of shards (%d) is higher than number of entries (%d)", u.labels, u.shardNum, u.entriesNum)
}

func NewRateStore() RateStore {
	return &rateStore{
		rates: make(map[string]int),
	}
}

func (r *rateStore) RateFor(stream logproto.Stream) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if rate, ok := r.rates[stream.Labels]; ok {
		return rate, nil
	}
	return 0, fmt.Errorf("no rate for stream %s", stream.Labels)
}

func (r *rateStore) storeRate(stream logproto.Stream, rate int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.rates[stream.Labels] = rate
	return nil
}
