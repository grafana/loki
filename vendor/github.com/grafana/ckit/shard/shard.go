// Package shard implements a set of consistent hashing algorithms to determine
// ownership of a key within a cluster.
package shard

import (
	"fmt"
	"sort"
	"sync"

	"github.com/grafana/ckit/internal/chash"
	"github.com/grafana/ckit/peer"
)

// Op is used to signify how a hash is intended to be used.
type Op uint8

const (
	// OpRead is used for read-only lookups. Only nodes in the Participant
	// or Terminating state are considered.
	OpRead Op = iota
	// OpReadWrite is used for read or write lookups. Only nodes in the
	// Participant state are considered.
	OpReadWrite
)

// String returns a string representation of the Op.
func (ht Op) String() string {
	switch ht {
	case OpRead:
		return "Read"
	case OpReadWrite:
		return "ReadWrite"
	default:
		return fmt.Sprintf("Op(%d)", ht)
	}
}

// A Sharder can lookup the owner for a specific key.
type Sharder interface {
	// Lookup returns numOwners Peers for the provided key. The provided op
	// is used to determine which peers may be considered potential owners.
	//
	// An error will be returned if the type of eligible peers for the provided
	// op is less than numOwners.
	Lookup(key Key, numOwners int, op Op) ([]peer.Peer, error)

	// Peers gets the current set of non-viewer peers used for sharding.
	Peers() []peer.Peer

	// SetPeers updates the set of peers used for sharding. Peers will be ignored
	// if they are a viewer.
	SetPeers(ps []peer.Peer)
}

// chasher wraps around two chash.Hash and adds logic for Op.
type chasher struct {
	peersMut sync.RWMutex
	peers    map[string]peer.Peer // Set of all peers shared across both hashes

	read, readWrite chash.Hash
}

func (ch *chasher) Peers() []peer.Peer {
	ch.peersMut.RLock()
	defer ch.peersMut.RUnlock()

	ps := make([]peer.Peer, 0, len(ch.peers))
	for _, p := range ch.peers {
		ps = append(ps, p)
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].Name < ps[j].Name })

	return ps
}

func (ch *chasher) SetPeers(ps []peer.Peer) {
	sort.Slice(ps, func(i, j int) bool { return ps[i].Name < ps[j].Name })

	var (
		newPeers     = make(map[string]peer.Peer, len(ps))
		newRead      = make([]string, 0, len(ps))
		newReadWrite = make([]string, 0, len(ps))
	)

	for _, p := range ps {
		// NOTE(rfratto): newRead and newReadWrite remain in sorted order since we
		// append to them from the already-sorted ps slice.
		switch p.State {
		case peer.StateParticipant:
			newRead = append(newRead, p.Name)
			newReadWrite = append(newReadWrite, p.Name)
			newPeers[p.Name] = p
		case peer.StateTerminating:
			newRead = append(newRead, p.Name)
			newPeers[p.Name] = p
		}
	}

	ch.peersMut.Lock()
	defer ch.peersMut.Unlock()

	ch.peers = newPeers
	ch.read.SetNodes(newRead)
	ch.readWrite.SetNodes(newReadWrite)
}

func (ch *chasher) Lookup(key Key, numOwners int, op Op) ([]peer.Peer, error) {
	ch.peersMut.RLock()
	defer ch.peersMut.RUnlock()

	var (
		names []string
		err   error
	)

	switch op {
	case OpRead:
		names, err = ch.read.Get(uint64(key), numOwners)
	case OpReadWrite:
		names, err = ch.readWrite.Get(uint64(key), numOwners)
	default:
		return nil, fmt.Errorf("unknown op %s", op)
	}
	if err != nil {
		return nil, err
	}

	res := make([]peer.Peer, len(names))
	for i, name := range names {
		p, ok := ch.peers[name]
		if !ok {
			panic("Unexpected peer " + name)
		}
		res[i] = p
	}
	return res, nil
}

// Multiprobe implements a multi-probe sharder: https://arxiv.org/abs/1505.00062
//
// Multiprobe is optimized for a median peak-to-average load ratio of 1.05. It
// performs a lookup in O(K * log N) time, where K is 21.
func Multiprobe() Sharder {
	return &chasher{
		read:      chash.Multiprobe(),
		readWrite: chash.Multiprobe(),
	}
}

// Rendezvous returns a rendezvous sharder (HRW, Highest Random Weight).
//
// Rendezvous is optimized for excellent load distribution, but has a runtime
// complexity of O(N).
func Rendezvous() Sharder {
	return &chasher{
		read:      chash.Rendezvous(),
		readWrite: chash.Rendezvous(),
	}
}

// Ring implements a ring sharder. numTokens determines how many tokens each
// node should have. Tokens are mapped to the unit circle, and then ownership
// of a key is determined by finding the next token on the unit circle. If two
// nodes have the same token, the node that lexicographically comes first will
// be used as the first owner.
//
// Ring is extremely fast, running in O(log N) time, but increases in memory
// usage as numTokens increases. Low values of numTokens will cause poor
// distribution; 256 or 512 is a good starting point.
func Ring(numTokens int) Sharder {
	return &chasher{
		read:      chash.Ring(numTokens),
		readWrite: chash.Ring(numTokens),
	}
}
