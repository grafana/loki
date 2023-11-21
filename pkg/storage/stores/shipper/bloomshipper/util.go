package bloomshipper

import (
	"sort"

	"github.com/grafana/loki/pkg/logproto"
)

// BoundedRefs is a set of requests that are clamped to a specific range
type BoundedRefs struct {
	BlockRef BlockRef
	Refs     [][]*logproto.GroupedChunkRefs
}

// reqs models a set of requests covering many fingerprints.
// consumers models a set of blocks covering different fingerprint ranges
func PartitionFingerprintRange(reqs [][]*logproto.GroupedChunkRefs, blocks []BlockRef) (res []BoundedRefs) {
	for _, block := range blocks {
		bounded := BoundedRefs{
			BlockRef: block,
		}

		for _, req := range reqs {
			min := sort.Search(len(req), func(i int) bool {
				return block.Cmp(req[i].Fingerprint) > Before
			})

			max := sort.Search(len(req), func(i int) bool {
				return block.Cmp(req[i].Fingerprint) == After
			})

			// All fingerprints fall outside of the consumer's range
			if min == len(req) || max == 0 {
				continue
			}

			bounded.Refs = append(bounded.Refs, req[min:max])
		}

		if len(bounded.Refs) > 0 {
			res = append(res, bounded)
		}

	}
	return res
}
