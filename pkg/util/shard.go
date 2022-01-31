package util

import (
	"crypto/md5"
	"encoding/binary"
	"math"
)

const (
	// Sharding strategies.
	ShardingStrategyDefault = "default"
	ShardingStrategyShuffle = "shuffle-sharding"
)

var (
	seedSeparator = []byte{0}
)

// ShuffleShardSeed returns seed for random number generator, computed from provided identifier.
func ShuffleShardSeed(identifier, zone string) int64 {
	// Use the identifier to compute an hash we'll use to seed the random.
	hasher := md5.New()
	hasher.Write(YoloBuf(identifier)) // nolint:errcheck
	if zone != "" {
		hasher.Write(seedSeparator) // nolint:errcheck
		hasher.Write(YoloBuf(zone)) // nolint:errcheck
	}
	checksum := hasher.Sum(nil)

	// Generate the seed based on the first 64 bits of the checksum.
	return int64(binary.BigEndian.Uint64(checksum))
}

// ShuffleShardExpectedInstancesPerZone returns the number of instances that should be selected for each
// zone when zone-aware replication is enabled. The algorithm expects the shard size to be divisible
// by the number of zones, in order to have nodes balanced across zones. If it's not, we do round up.
func ShuffleShardExpectedInstancesPerZone(shardSize, numZones int) int {
	return int(math.Ceil(float64(shardSize) / float64(numZones)))
}

// ShuffleShardExpectedInstances returns the total number of instances that should be selected for a given
// tenant. If zone-aware replication is disabled, the input numZones should be 1.
func ShuffleShardExpectedInstances(shardSize, numZones int) int {
	return ShuffleShardExpectedInstancesPerZone(shardSize, numZones) * numZones
}
