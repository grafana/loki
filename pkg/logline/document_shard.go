package logline

import (
	"math/bits"

	"github.com/prometheus/prometheus/model/labels"
)

// VersionHasDocumentShards reports whether an index version splits each
// document interval by stream fingerprint.
func VersionHasDocumentShards(version string) bool {
	return version == "v5"
}

// StreamFingerprint returns a stream's fingerprint as the ingester computes it
// (instance.getHashForLabels, before collision mapping). TSDB stores that
// fingerprint for the stream's series and shards queries on it, so document
// shards must be computed from this function and nothing else.
//
// buf is scratch space for the hash input; the grown buffer is returned for
// reuse.
func StreamFingerprint(ls labels.Labels, buf []byte) (uint64, []byte) {
	return ls.HashWithoutLabels(buf)
}

// DocumentShard returns the document shard of a stream fingerprint: its top
// log2(shards) bits. That is the prefix TSDB's index.ShardAnnotation.Match
// uses, so document shards line up with Loki's power-of-two query shards.
//
// fp must come from StreamFingerprint. shards must be a power of two; 0 and 1
// both mean one shard.
func DocumentShard(fp uint64, shards int) uint32 {
	if shards <= 1 {
		return 0
	}
	return uint32(fp >> (64 - bits.TrailingZeros(uint(shards))))
}
