package util //nolint:revive

import (
	"encoding/binary"

	"github.com/cespare/xxhash/v2"

	"github.com/grafana/loki/pkg/push"
)

// StructuredMetadataHash hashes a structured metadata list, typically the structured metadata
// shared by every entry of a stream.
//
// The hash is order-dependent: two lists holding the same pairs in a different order hash to
// different values. That is intentional and cheap, and it is safe for the ways the hash is
// used, all of which only need "same bytes in the same order" to mean "same set":
//
//   - the distributor groups OTLP entries into wire streams by it, where the attribute order of
//     a given resource or scope is stable within a request;
//   - the ingester keys its per chunk interning cache by it, and reorderings only cost a second
//     cache entry holding the same symbols;
//   - the ingester compares it to decide whether two entries came from the same shared list
//     when detecting duplicate pushes, where a false "different" only means an entry that would
//     have been dropped as a duplicate is stored instead.
//
// The empty list hashes to 0, so callers can use 0 as "no shared structured metadata".
func StructuredMetadataHash(metas push.LabelsAdapter) uint64 {
	if len(metas) == 0 {
		return 0
	}

	h := xxhash.New()
	for _, meta := range metas {
		_, _ = h.WriteString(meta.Name)
		_, _ = h.Write([]byte{0xff})
		_, _ = h.WriteString(meta.Value)
		_, _ = h.Write([]byte{0xff})
	}

	return h.Sum64()
}

// SharedPairHash combines the hashes of the resource set and of the scope set an entry
// references into the single identity of "the shared structured metadata of this entry".
//
// Entries reference two sets independently, so neither hash alone identifies what an entry
// shares; the consumers that need that identity need it as one value:
//
//   - the ingester keys its per chunk symbol interning cache by it, since what gets interned
//     is the resource and scope attributes together;
//   - the duplicate push detection compares it to decide whether two entries carry the same
//     shared metadata.
//
// The combination is order dependent, so the pair (resource, scope) and the pair
// (scope, resource) hash differently: the two sets play different roles and swapping them
// changes which one wins a name collision.
//
// Two zero hashes, meaning the entry references no set at all, combine to 0, so callers can
// keep using 0 as "nothing shared". A real pair never combines to 0.
func SharedPairHash(resourceHash, scopeHash uint64) uint64 {
	if resourceHash == 0 && scopeHash == 0 {
		return 0
	}

	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[0:8], resourceHash)
	binary.LittleEndian.PutUint64(buf[8:16], scopeHash)

	h := xxhash.Sum64(buf[:])
	if h == 0 {
		// Keep 0 reserved for "nothing shared" so that a freak collision with the sentinel
		// cannot make an entry that does share metadata look like one that does not.
		h = 1
	}
	return h
}
