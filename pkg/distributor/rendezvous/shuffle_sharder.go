package rendezvous

import (
	"hash/crc64"
	"sort"
	"strconv"

	"github.com/cespare/xxhash/v2"
)

var table = crc64.MakeTable(crc64.ECMA)

type ShuffleSharder struct {
	partitions []int32
	hashes     []uint64
}

func NewShuffleSharder(partitions []int32) ShuffleSharder {
	hashes := make([]uint64, len(partitions))
	for i, partition := range partitions {
		hashes[i] = xxhash.Sum64String("loki-distributor-" + strconv.Itoa(int(partition)))
	}
	return ShuffleSharder{partitions, hashes}
}

func (r ShuffleSharder) Shard(key uint32) int32 {
	var maxPartition int32
	var maxHash uint64
	for i, partition := range r.partitions {
		hash := xorshiftMult64(uint64(key) ^ r.hashes[i])
		if maxHash == 0 || hash > maxHash {
			maxPartition = partition
			maxHash = hash
		}
	}
	return maxPartition
}

func (r ShuffleSharder) ShuffleShard(shuffleShardKey string, numShards int) ShuffleSharder {
	if numShards == 0 || numShards > len(r.partitions) {
		numShards = len(r.partitions)
	}

	key := crc64.Checksum([]byte(shuffleShardKey), table)
	hashes := make([]partitionAndHash, len(r.partitions))
	for i, partition := range r.partitions {
		originalHash := r.hashes[i]
		hashes[i] = partitionAndHash{
			partition,
			xorshiftMult64(key ^ originalHash),
			originalHash,
		}
	}

	sort.Slice(hashes, func(i, j int) bool {
		return hashes[i].hash < hashes[j].hash
	})

	subpartitions := make([]int32, numShards)
	subHashesSet := make([]uint64, numShards)
	for i := 0; i < numShards; i++ {
		subpartitions[i] = hashes[i].partition
		subHashesSet[i] = hashes[i].originalHash
	}
	return ShuffleSharder{subpartitions, subHashesSet}
}

type partitionAndHash struct {
	partition    int32
	hash         uint64
	originalHash uint64
}

// https://vigna.di.unimi.it/ftp/papers/xorshift.pdf
func xorshiftMult64(x uint64) uint64 {
	x ^= x >> 12 // a
	x ^= x << 25 // b
	x ^= x >> 27 // c
	return x * 2685821657736338717
}
