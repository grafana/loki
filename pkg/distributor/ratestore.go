package distributor

import (
	"fmt"

	"github.com/grafana/loki/pkg/logproto"
)

type unshardableStreamErr struct {
	labels     string
	entriesNum int
	shardNum   int
}

func (u *unshardableStreamErr) Error() string {
	return fmt.Sprintf("couldn't shard stream %s. number of shards (%d) is higher than number of entries (%d)", u.labels, u.shardNum, u.entriesNum)
}

type noopRateStore struct {
	rate int
}

func (n *noopRateStore) RateFor(stream *logproto.Stream) (int, error) {
	return n.rate, nil
}
