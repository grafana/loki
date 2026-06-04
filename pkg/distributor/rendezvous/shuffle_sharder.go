package rendezvous

import (
	"errors"
	"hash/crc64"
	"sort"
	"strconv"

	"github.com/cespare/xxhash/v2"
)

var table = crc64.MakeTable(crc64.ECMA)

// ShuffleSharder implements rendezvous hashing - https://en.wikipedia.org/wiki/Rendezvous_hashing.
// Creates a score of each partition on creation and uses xorshiftMult64
// (https://vigna.di.unimi.it/ftp/papers/xorshift.pdf) to generate scores thereafter to avoid running expensive score
// functions for every request.
type ShuffleSharder struct {
	partitions []int32
	hashes     []uint64
}

func NewShuffleSharder(partitions []int32) ShuffleSharder {
	partitionsCopy := make([]int32, len(partitions))
	hashes := make([]uint64, len(partitions))
	for i, partition := range partitions {
		partitionsCopy[i] = partition
		hashes[i] = xxhash.Sum64String("loki-distributor-" + strconv.Itoa(int(partition)))
	}
	return ShuffleSharder{partitionsCopy, hashes}
}

func (r *ShuffleSharder) Shard(key uint32) (int32, error) {
	if len(r.partitions) == 0 {
		return 0, errors.New("no active partitions")
	}
	var maxPartition int32
	var maxScore uint64
	for i, partition := range r.partitions {
		score := xorshiftMult64(uint64(key) ^ r.hashes[i])
		if maxScore == 0 || score > maxScore {
			maxPartition = partition
			maxScore = score
		}
	}
	return maxPartition, nil
}

func (r *ShuffleSharder) ShuffleShard(shuffleShardKey string, numShards int) *ShuffleSharder {
	if numShards == 0 || numShards >= len(r.partitions) {
		return r
	}

	key := crc64.Checksum([]byte(shuffleShardKey), table)
	scores := make([]partitionAndScore, len(r.partitions))
	for i, partition := range r.partitions {
		originalHash := r.hashes[i]
		scores[i] = partitionAndScore{
			partition,
			xorshiftMult64(key ^ originalHash),
			originalHash,
		}
	}

	// This sort dominates the cost of shuffle sharding.
	// Possible future optimization - using a size-limited max heap to keep track of the k largest scores.
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	subpartitions := make([]int32, numShards)
	subHashesSet := make([]uint64, numShards)
	for i := 0; i < numShards; i++ {
		subpartitions[i] = scores[i].partition
		subHashesSet[i] = scores[i].hash // Avoid recalculating these hashes
	}
	return &ShuffleSharder{subpartitions, subHashesSet}
}

func (r *ShuffleSharder) Size() int {
	return len(r.partitions)
}

type partitionAndScore struct {
	partition int32
	score     uint64
	hash      uint64
}

// https://vigna.di.unimi.it/ftp/papers/xorshift.pdf
func xorshiftMult64(x uint64) uint64 {
	x ^= x >> 12 // a
	x ^= x << 25 // b
	x ^= x >> 27 // c
	return x * 2685821657736338717
}
