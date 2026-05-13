package compactionv2

import (
	"sort"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

// run is one non-overlapping run of sections
type run struct {
	sections  []*compactionv2pb.SectionRef
	topMaxKey string
	createdAt int
}

// patienceSort runs the patience-sort run-based algorithm. Returns runs in
// creation order.
//
// The input slice is sorted in place; callers that need the original order must
// copy beforehand. The contract requires non-nil SectionRef entries; nil entries
// will panic.
func patienceSort(sections []*compactionv2pb.SectionRef) []*run {
	if len(sections) == 0 {
		return nil
	}

	// Step 1: sort sections by (MinKey ASC, MaxKey ASC, ObjectPath ASC, SectionIndex ASC).
	sort.Slice(sections, func(i, j int) bool {
		a, b := sections[i], sections[j]
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

	// runs in creation order; this is what we return.
	runs := make([]*run, 0)

	for _, s := range sections {
		// Step 3: best-fit = pile with the largest topMaxKey strictly less
		// than s.MinKey. Linear scan over piles in creation order.
		//
		// tiebreaker (oldest wins): we update `best` only on STRICT
		// improvement of topMaxKey, so among piles tied on topMaxKey the
		// first one seen — which by creation-order iteration is the OLDEST —
		// is kept.
		var best *run
		for _, r := range runs {
			if r.topMaxKey < s.MinKey {
				if best == nil || r.topMaxKey > best.topMaxKey {
					best = r
				}
			}
		}

		if best != nil {
			best.sections = append(best.sections, s)
			best.topMaxKey = s.MaxKey
			continue
		}

		// No eligible pile: start a new one.
		runs = append(runs, &run{
			sections:  []*compactionv2pb.SectionRef{s},
			topMaxKey: s.MaxKey,
			createdAt: len(runs),
		})
	}

	return runs
}
