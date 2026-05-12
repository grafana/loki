package compactionv2

import (
	"sort"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

// pile is one patience-sort pile under construction.
type pile struct {
	sections  []*compactionv2pb.SectionRef
	topMaxKey string
	createdAt int
}

// patienceSort runs the patience-sort run-based algorithm. Returns piles in
// creation order. The caller's slice is not mutated. See the spec § Compaction
// unit / Phase 1 for the algorithm.
func patienceSort(sections []*compactionv2pb.SectionRef) []*pile {
	if len(sections) == 0 {
		return nil
	}

	// Make a local copy so we don't mutate the caller's slice. Skip nils
	// defensively (not expected from real callers).
	sorted := make([]*compactionv2pb.SectionRef, 0, len(sections))
	for _, s := range sections {
		if s == nil {
			continue
		}
		sorted = append(sorted, s)
	}

	// Step 1: sort sections by (MinKey ASC, MaxKey ASC, ObjectPath ASC, SectionIndex ASC).
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.MinKey != b.MinKey {
			return a.MinKey < b.MinKey
		}
		if a.MaxKey != b.MaxKey {
			return a.MaxKey < b.MaxKey
		}
		if a.ObjectPath != b.ObjectPath {
			return a.ObjectPath < b.ObjectPath
		}
		return a.SectionIndex < b.SectionIndex
	})

	// piles in creation order; this is what we return.
	piles := make([]*pile, 0)

	for _, s := range sorted {
		// Step 3: best-fit = pile with the largest topMaxKey strictly less
		// than s.MinKey. Linear scan over piles in creation order.
		//
		// D2 tiebreaker (oldest wins): we update `best` only on STRICT
		// improvement of topMaxKey, so among piles tied on topMaxKey the
		// first one seen — which by creation-order iteration is the OLDEST —
		// is kept.
		var best *pile
		for _, p := range piles {
			if p.topMaxKey < s.MinKey {
				if best == nil || p.topMaxKey > best.topMaxKey {
					best = p
				}
			}
		}

		if best != nil {
			best.sections = append(best.sections, s)
			best.topMaxKey = s.MaxKey
			continue
		}

		// No eligible pile: start a new one.
		piles = append(piles, &pile{
			sections:  []*compactionv2pb.SectionRef{s},
			topMaxKey: s.MaxKey,
			createdAt: len(piles),
		})
	}

	return piles
}
