package rendezvous

import (
	"errors"
	"hash/crc64"
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

	// Use a min-heap of size numShards to select the top-numShards elements in
	// O(n log k) instead of sorting all n elements in O(n log n). This also
	// allocates only k elements instead of n.
	top := make([]partitionAndScore, numShards)
	for i := range numShards {
		top[i] = partitionAndScore{r.partitions[i], xorshiftMult64(key ^ r.hashes[i]), r.hashes[i]}
	}
	buildMinHeap(top)
	for i := numShards; i < len(r.partitions); i++ {
		if score := xorshiftMult64(key ^ r.hashes[i]); score > top[0].score {
			top[0] = partitionAndScore{r.partitions[i], score, r.hashes[i]}
			siftDown(top, 0)
		}
	}

	subpartitions := make([]int32, numShards)
	subHashes := make([]uint64, numShards)
	for i, s := range top {
		subpartitions[i] = s.partition
		subHashes[i] = s.hash
	}
	return &ShuffleSharder{subpartitions, subHashes}
}

func (r *ShuffleSharder) Size() int {
	return len(r.partitions)
}

type partitionAndScore struct {
	partition int32
	score     uint64
	hash      uint64
}

func buildMinHeap(h []partitionAndScore) {
	for i := len(h)/2 - 1; i >= 0; i-- {
		siftDown(h, i)
	}
}

func siftDown(h []partitionAndScore, i int) {
	n := len(h)
	for {
		smallest := i
		if l := 2*i + 1; l < n && h[l].score < h[smallest].score {
			smallest = l
		}
		if r := 2*i + 2; r < n && h[r].score < h[smallest].score {
			smallest = r
		}
		if smallest == i {
			break
		}
		h[i], h[smallest] = h[smallest], h[i]
		i = smallest
	}
}

// https://vigna.di.unimi.it/ftp/papers/xorshift.pdf
func xorshiftMult64(x uint64) uint64 {
	x ^= x >> 12 // a
	x ^= x << 25 // b
	x ^= x >> 27 // c
	return x * 2685821657736338717
}
