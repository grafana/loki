/*
Copyright 2013 Google Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package consistenthash provides an implementation of a ring hash.
package consistenthash // import "github.com/vimeo/galaxycache/consistenthash"

import (
	"encoding/binary"
	"hash/crc32"
	"sort"
	"strconv"
)

// Hash maps the data to a uint32 hash-ring
type Hash func(data []byte) uint32

// Map tracks segments in a hash-ring, mapped to specific keys.
type Map struct {
	hash       Hash
	segsPerKey int
	keyHashes  []uint32 // Sorted
	hashMap    map[uint32]string
	keys       map[string]struct{}
}

// New constructs a new consistenthash hashring, with segsPerKey segments per added key.
func New(segsPerKey int, fn Hash) *Map {
	m := &Map{
		segsPerKey: segsPerKey,
		hash:       fn,
		hashMap:    make(map[uint32]string),
		keys:       make(map[string]struct{}),
	}
	if m.hash == nil {
		m.hash = crc32.ChecksumIEEE
	}
	return m
}

// IsEmpty returns true if there are no items available.
func (m *Map) IsEmpty() bool {
	return len(m.keyHashes) == 0
}

// Add adds some keys to the hashring, establishing ownership of segsPerKey
// segments.
func (m *Map) Add(keys ...string) {
	for _, key := range keys {
		m.keys[key] = struct{}{}
		for i := 0; i < m.segsPerKey; i++ {
			hash := m.hash([]byte(strconv.Itoa(i) + key))
			m.keyHashes = append(m.keyHashes, hash)
			m.hashMap[hash] = key
		}
	}
	sort.Slice(m.keyHashes, func(i, j int) bool { return m.keyHashes[i] < m.keyHashes[j] })
}

// Get gets the closest item in the hash to the provided key.
func (m *Map) Get(key string) string {
	if m.IsEmpty() {
		return ""
	}

	hash := m.hash([]byte(key))

	_, _, owner := m.findSegmentOwner(hash)
	return owner
}

func (m *Map) findSegmentOwner(hash uint32) (int, uint32, string) {
	// Binary search for appropriate replica.
	idx := sort.Search(len(m.keyHashes), func(i int) bool { return m.keyHashes[i] >= hash })

	// Means we have cycled back to the first replica.
	if idx == len(m.keyHashes) {
		idx = 0
	}

	return idx, m.keyHashes[idx], m.hashMap[m.keyHashes[idx]]
}

func (m *Map) prevSegmentOwner(idx int, lastSegHash, hash uint32) (int, uint32, string) {
	if len(m.keys) == 1 {
		panic("attempt to find alternate owner for single-key map")
	}
	if idx == 0 {
		// if idx is 0, then wrap around
		return m.prevSegmentOwner(len(m.keyHashes)-1, lastSegHash, hash)
	}

	// we're moving backwards within a ring; decrement the index
	idx--

	return idx, m.keyHashes[idx], m.hashMap[m.keyHashes[idx]]
}

func (m *Map) idxedKeyReplica(key string, replica int) uint32 {
	// For replica zero, do not append a suffix so Get() and GetReplicated are compatible
	if replica == 0 {
		return m.hash([]byte(key))
	}
	// Allocate an extra 2 bytes so we have 2 bytes of padding to function
	// as a separator between the main key and the suffix
	idxSuffixBuf := [binary.MaxVarintLen64 + 2]byte{}
	// Set those 2 bytes of padding to a nice non-zero value with
	// alternating zeros and ones.
	idxSuffixBuf[0] = 0xaa
	idxSuffixBuf[1] = 0xaa

	// Encode the replica using unsigned varints which are more compact and cheaper to encode.
	// definition: https://developers.google.com/protocol-buffers/docs/encoding#varints
	vIntLen := binary.PutUvarint(idxSuffixBuf[2:], uint64(replica))

	idxHashKey := append([]byte(key), idxSuffixBuf[:vIntLen+2]...)
	return m.hash(idxHashKey)
}

// GetReplicated gets the closest item in the hash to a deterministic set of
// keyReplicas variations of the provided key.
// The returned set of segment-owning keys is dedup'd, and collisions are
// resolved by traversing backwards in the hash-ring to find an unused
// owning-key.
func (m *Map) GetReplicated(key string, keyReplicas int) []string {
	if m.IsEmpty() {
		return []string{}
	}
	out := make([]string, 0, keyReplicas)
	segOwners := make(map[string]struct{}, keyReplicas)

	for i := 0; i < keyReplicas && len(out) < len(m.keys); i++ {
		h := m.idxedKeyReplica(key, i)
		segIdx, segBound, owner := m.findSegmentOwner(h)
		for _, present := segOwners[owner]; present; _, present = segOwners[owner] {
			// this may overflow, which is fine.
			segIdx, segBound, owner = m.prevSegmentOwner(segIdx, segBound, h)
		}
		segOwners[owner] = struct{}{}
		out = append(out, owner)
	}

	return out
}
