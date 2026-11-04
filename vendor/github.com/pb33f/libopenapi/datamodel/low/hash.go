// Copyright 2022-2025 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package low

import (
	"encoding/binary"
	"hash/maphash"
	"sort"
	"sync"

	"github.com/pb33f/go-yaml"
	"github.com/pb33f/libopenapi/orderedmap"
)

// globalHashSeed ensures consistent hashes across all pooled instances.
// Set once at init, deterministic within a process run.
var globalHashSeed maphash.Seed

func init() {
	globalHashSeed = maphash.MakeSeed()
}

// hasherPool pools maphash.Hash instances for reuse
var hasherPool = sync.Pool{
	New: func() any {
		h := &maphash.Hash{}
		h.SetSeed(globalHashSeed)
		return h
	},
}

// visitedPool pools visited maps for hashNodeTree to reduce allocations.
var visitedPool = sync.Pool{
	New: func() any { return make(map[*yaml.Node]bool, 32) },
}

// getVisitedMap returns a cleared map from the pool.
func getVisitedMap() map[*yaml.Node]bool {
	return visitedPool.Get().(map[*yaml.Node]bool)
}

// putVisitedMap returns a map to the pool, discarding maps that grew too large.
func putVisitedMap(m map[*yaml.Node]bool) {
	if len(m) > 1024 {
		return // let GC collect oversized maps
	}
	clear(m)
	visitedPool.Put(m)
}

// ClearNodePools does nothing. Pooled visited maps are cleared before they are returned to the pool, so they
// never hold *yaml.Node pointers between uses and there is nothing to release.
//
// Deprecated: there is no longer anything to clear. Replacing the pool while other goroutines used it was also
// a data race.
func ClearNodePools() {}

// WithHasher provides a pooled hasher for the duration of fn.
// The hasher is automatically returned to the pool after fn completes.
// This pattern eliminates forgotten PutHasher() bugs.
func WithHasher(fn func(h *maphash.Hash) uint64) uint64 {
	hasher := hasherPool.Get().(*maphash.Hash)
	hasher.Reset()
	result := fn(hasher)
	hasherPool.Put(hasher)
	return result
}

// HashBool writes a boolean as a single byte.
func HashBool(h *maphash.Hash, b bool) {
	if b {
		h.WriteByte(1)
	} else {
		h.WriteByte(0)
	}
}

// HashInt64 writes an int64 without allocation using binary encoding.
func HashInt64(h *maphash.Hash, n int64) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(n))
	h.Write(buf[:])
}

// HashUint64 writes another hash value (for composition of nested Hashable objects).
func HashUint64(h *maphash.Hash, v uint64) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], v)
	h.Write(buf[:])
}

// HASH_PIPE is the separator byte used between hash fields. :)
const HASH_PIPE = '|'

// HashLabel writes the name of a field ahead of its value. Labelling every field that is only written when it is
// set means two fields holding the same value, like minimum and maximum, can never produce the same hash.
func HashLabel(h *maphash.Hash, label string) {
	h.WriteString(label)
	h.WriteByte(':')
}

// HashString writes a labelled string. The length goes ahead of the string, so a value that contains the separator
// cannot run on into the field after it.
func HashString(h *maphash.Hash, label, value string) {
	HashLabel(h, label)
	HashInt64(h, int64(len(value)))
	h.WriteString(value)
	h.WriteByte(HASH_PIPE)
}

// HashMap writes a labelled map as the key and value hash of every entry, in key order. Sorting keeps the order the
// entries were declared in out of the hash, while writing the keys means renaming an entry changes it.
func HashMap[V any](h *maphash.Hash, label string, m *orderedmap.Map[KeyReference[string], ValueReference[V]]) {
	if m == nil || m.Len() == 0 {
		return
	}

	type entry struct {
		key   string
		value V
	}
	entries := make([]entry, 0, m.Len())
	for k, v := range m.FromOldest() {
		entries = append(entries, entry{key: k.Value, value: v.Value})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].key < entries[j].key
	})

	HashLabel(h, label)
	HashInt64(h, int64(len(entries)))
	for _, e := range entries {
		HashInt64(h, int64(len(e.key)))
		h.WriteString(e.key)
		h.WriteString(GenerateHashString(e.value))
		h.WriteByte(HASH_PIPE)
	}
}
