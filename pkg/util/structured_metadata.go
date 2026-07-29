package util //nolint:revive

import (
	"github.com/cespare/xxhash/v2"

	"github.com/grafana/loki/pkg/push"
)

// StructuredMetadataHash hashes a structured metadata list, be it the metadata shared by every
// entry of a stream or the metadata of a single entry.
//
// The hash is order-dependent: two lists holding the same pairs in a different order hash to
// different values. That is intentional and cheap, and it is safe for the ways the hash is
// used, all of which only need "same bytes in the same order" to mean "same list":
//
//   - the distributor groups OTLP entries into wire streams by it, where the attribute order of
//     a given resource or scope is stable within a request;
//   - the ingester identifies the structured metadata an entry was stored with by it when
//     detecting duplicate pushes, where the lists compared are built in the same order from the
//     same parts, and a false "different" only means an entry that would have been dropped as a
//     duplicate is stored instead.
//
// The empty list hashes to 0, so callers can use 0 as "no structured metadata".
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
