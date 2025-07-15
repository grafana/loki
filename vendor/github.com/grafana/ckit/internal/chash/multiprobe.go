package chash

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"sync"

	"github.com/cespare/xxhash/v2"
)

// Multiprobe implements a multi-probe hash: https://arxiv.org/abs/1505.00062
// Multiprobe is optimized for a median peak-to-average load ratio of 1.05.
// It performs a lookup in O(K * log N) time, where K is 21.
func Multiprobe() Hash {
	return &multiprobe{}
}

type multiprobe struct {
	mut    sync.RWMutex
	tokens []ringToken
}

func (mp *multiprobe) Get(key uint64, n int) ([]string, error) {
	mp.mut.RLock()
	defer mp.mut.RUnlock()

	if n > len(mp.tokens) {
		return nil, fmt.Errorf("not enough nodes: need at least %d, have %d", n, len(mp.tokens))
	} else if n == 0 {
		return []string{}, nil
	}

	var (
		h1 = secondKey(key)
		h2 = secondKey(h1)

		K = 21
	)

	var (
		closestIdx  int    // closest node
		closestDist uint64 = math.MaxUint64
	)

	// TODO(rfratto): this is a little slow. Instead of iterating over the full
	// set of tokens, we could partition tokens by bit prefix and iterate over
	// the partition instead. This may help speed up lookups at lower numbers of
	// nodes.
	//
	// If the bit prefix was the high 4 bits, that would result in 16 buckets.
	// With 100 nodes, that would roughly be 7 nodes per bucket, moving the
	// lookup time to O(21 * log(100)) to O(21 * log(7)) (roughly 139 to 59,
	// a 57% improvement).
	for k := 0; k < K; k++ {
		h := h1 + uint64(k)*h2

		idx := findClosest(mp.tokens, h)
		tok := mp.tokens[idx]

		dist := distance(tok.token, h)
		if dist < closestDist {
			closestIdx = idx
			closestDist = dist
		}
	}

	var (
		res = make([]string, n)
	)
	for i := 0; i < n; i++ {
		// NOTE(rfratto): Only closestIdx is guaranteed to be the closest
		// token in terms of distance. We don't account for distance when
		// searching for the other nodes for replication. In practice this
		// shouldn't harm distribution much.
		res[i] = mp.tokens[wrapIndex(closestIdx+i, len(mp.tokens))].node
	}
	return res, nil
}

func secondKey(key1 uint64) uint64 {
	dig := xxhash.New()
	_, _ = dig.Write(strconv.AppendUint(nil, key1, 16))
	return dig.Sum64()
}

// findClosest returns the index of the tok whose distance to "to" is the
// smallest.
func findClosest(tok []ringToken, to uint64) int {
	found := sort.Search(len(tok), func(i int) bool {
		return tok[i].token >= to
	})

	var (
		idxA = wrapIndex(found-1, len(tok))
		idxB = wrapIndex(found+0, len(tok))

		distA = distance(tok[idxA].token, to)
		distB = distance(tok[idxB].token, to)
	)

	if distA <= distB {
		return idxA
	}
	return idxB
}

func wrapIndex(i int, len int) int {
	if i < 0 {
		return len - 1
	}
	return i % len
}

func distance(a, b uint64) uint64 {
	if a < b {
		return b - a
	}
	return a - b
}

func (mp *multiprobe) SetNodes(nodes []string) {
	newTokens := make([]ringToken, len(nodes))
	for i, n := range nodes {
		newTokens[i] = ringToken{
			node:  n,
			token: xxhash.Sum64String(n),
		}
	}
	sort.Sort(byRingToken(newTokens))

	mp.mut.Lock()
	defer mp.mut.Unlock()
	mp.tokens = newTokens
}
