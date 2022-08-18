package distributor

type NoopStreamSharder struct{}

func (s *NoopStreamSharder) ShardsFor(stream string) ShardIter {
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
