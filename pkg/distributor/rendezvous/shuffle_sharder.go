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
	n := len(r.partitions)
	if numShards == 0 || numShards >= n {
		return r
	}

	key := crc64.Checksum([]byte(shuffleShardKey), table)

	if numShards <= n-numShards {
		// numShards <= n/2: select the top-numShards elements. O(n log numShards).
		top := make([]indexAndScore, numShards)
		selectTopKIndices(top, r.hashes, key, false)
		partitions := make([]int32, numShards)
		hashes := make([]uint64, numShards)
		for i, s := range top {
			partitions[i] = r.partitions[s.index]
			hashes[i] = r.hashes[s.index]
		}
		return &ShuffleSharder{partitions, hashes}
	}

	// numShards > n/2: cheaper to find the bottom-(n-numShards) elements to exclude, then
	// return everything else. O(n log (n-numShards)).
	numExclude := n - numShards
	bottom := make([]indexAndScore, numExclude)
	selectTopKIndices(bottom, r.hashes, key, true)
	excluded := make([]bool, n)
	for _, b := range bottom {
		excluded[b.index] = true
	}
	partitions := make([]int32, 0, numShards)
	hashes := make([]uint64, 0, numShards)
	for i := range n {
		if !excluded[i] {
			partitions = append(partitions, r.partitions[i])
			hashes = append(hashes, r.hashes[i])
		}
	}
	return &ShuffleSharder{partitions, hashes}
}

func (r *ShuffleSharder) Size() int {
	return len(r.partitions)
}

type indexAndScore struct {
	index int
	score uint64
}

// selectTopKIndices fills h (pre-allocated, len defines k) with the k items with the highest
// scores from hashes.
// Pass invertOrdering=true to instead select the k lowest-scoring items.
// Caller pre-allocates heap so its backing array doesn't escape to the heap.
// Doesn't use go's built-in heap support to avoid the backing array escaping to the heap. This is a significant
// performance optimization - benchmarks are ~45% slower using built-in heap.
func selectTopKIndices(heap []indexAndScore, hashes []uint64, key uint64, invertOrdering bool) {
	k := len(heap)
	// Put the first k elements into the heap
	for i := range k {
		heap[i] = indexAndScore{i, calculateScore(key, hashes[i], invertOrdering)}
	}
	// Make it into a valid heap
	for i := k/2 - 1; i >= 0; i-- {
		siftDown(heap, i)
	}
	// Push the rest of the elements into the heap
	for i := k; i < len(hashes); i++ {
		if score := calculateScore(key, hashes[i], invertOrdering); score > heap[0].score {
			heap[0] = indexAndScore{i, score}
			siftDown(heap, 0)
		}
	}
}

func calculateScore(key uint64, hash uint64, invertOrdering bool) uint64 {
	score := xorshiftMult64(key ^ hash)
	if invertOrdering {
		score = score ^ (^uint64(0))
	}
	return score
}

func siftDown(h []indexAndScore, i int) {
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
