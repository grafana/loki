package distributor

import "github.com/grafana/loki/pkg/logproto"

type NoopStreamSharder struct{}

func (s *NoopStreamSharder) ShardsFor(_ logproto.Stream) ShardIter {
	return &NoopShardIter{}
}
func (s *NoopStreamSharder) IncreaseShardsFor(_ string) {}

type NoopShardIter struct{}

func (i *NoopShardIter) NumShards() int {
	return 0
}

func (i *NoopShardIter) NextShardID() int {
	return 0
}
