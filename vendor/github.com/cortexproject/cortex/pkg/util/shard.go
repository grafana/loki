package util

import (
	"crypto/md5"
	"encoding/binary"
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
