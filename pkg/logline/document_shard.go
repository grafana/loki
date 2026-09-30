package logline

import "math/bits"

// VersionHasDocumentShards reports whether an index version splits each
// document interval by stream fingerprint.
func VersionHasDocumentShards(version string) bool {
	return version == "v5"
}

// DocumentShard returns the document shard of a stream fingerprint: its top
// log2(shards) bits. That is the prefix TSDB's index.ShardAnnotation.Match
// uses, so document shards line up with Loki's power-of-two query shards.
//
// fp must be the stream's ingester fingerprint (labels.StableHash of the
// stream labels). shards must be a power of two; 0 and 1 both mean one shard.
func DocumentShard(fp uint64, shards int) uint32 {
	if shards <= 1 {
		return 0
	}
	return uint32(fp >> (64 - bits.TrailingZeros(uint(shards))))
}
