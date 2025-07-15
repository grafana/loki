package chash

import (
	"fmt"
	"sort"
	"sync"

	"github.com/cespare/xxhash/v2"
)

// Rendezvous returns a rendezvous hashing algorithm (HRW, Highest Random
// Weight). Rendezvous is optimized for excellent load distribution, but
// has a runtime complexity of O(N).
func Rendezvous() Hash {
	return &rendezvous{}
}

type rendezvous struct {
	mut    sync.RWMutex
	hashes map[string]uint64
	nodes  []string
}

func (r *rendezvous) Get(key uint64, n int) ([]string, error) {
	r.mut.RLock()
	defer r.mut.RUnlock()

	if n > len(r.nodes) {
		return nil, fmt.Errorf("not enough nodes: need at least %d, have %d", n, len(r.nodes))
	} else if n == 0 {
		return []string{}, nil
	}

	var (
		res    = make([]string, n)
		hashes = make([]ringToken, len(r.nodes))
	)

	// Get the hash for all nodes, sort them, and then got the lowest three. This
	// uses more memory but runs in O(N) time instead of O(N * log n).
	for i, node := range r.nodes {
		hashes[i] = ringToken{
			node:  node,
			token: xorshiftMult64(key ^ r.hashes[node]),
		}
	}
	sort.Sort(byRingToken(hashes))

	for i := 0; i < n; i++ {
		res[i] = hashes[i].node
	}
	return res, nil
}

func (r *rendezvous) SetNodes(nodes []string) {
	var (
		newHashes = make(map[string]uint64, len(nodes))
		newNodes  = make([]string, len(nodes))
	)
	for i, n := range nodes {
		newHashes[n] = xxhash.Sum64String(n)
		newNodes[i] = n
	}
	sort.Strings(newNodes)

	r.mut.Lock()
	defer r.mut.Unlock()
	r.hashes = newHashes
	r.nodes = newNodes
}

// https://vigna.di.unimi.it/ftp/papers/xorshift.pdf
func xorshiftMult64(x uint64) uint64 {
	x ^= x >> 12 // a
	x ^= x << 25 // b
	x ^= x >> 27 // c
	return x * 2685821657736338717
}
