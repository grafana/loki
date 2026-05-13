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

// calculateRuns sorts the provided [sections] in place and returns a set of
// runs in creation order. A "run" is a sorted sequence of sections whose
// (MinKey, MaxKey) bounds are pairwise non-overlapping in sort-key space.
//
// The input slice is sorted in place; callers that need the original order
// must copy beforehand. The contract requires non-nil SectionRef entries;
// nil entries will panic.
//
// The algorithm is a patience-sort-style best-fit: for each section in sorted
// order, append it to the existing run whose top_max_key is the largest value
// strictly less than the section's MinKey; if no such run exists, start a new
// one. Among runs tied on top_max_key, the OLDEST run wins (deterministic via
// creation-order iteration and a strict-`>` comparison).
//
// MinKey/MaxKey are treated as opaque byte sequences and compared
// lexicographically. The planner is responsible for constructing
// them as sort_schema-prefixed sort keys derived from each section's stats.
//
// L0 sections come from the consumer/ingester. They are sorted by
// __timestamp__ (ingestion order), NOT by service_name, so services
// interleave inside each section. Each section's MinKey/MaxKey therefore
// spans the full service_name range:
//
//	Section A0 (Schema=[])             Section B0 (Schema=[])             Section C0 (Schema=[])
//	-----------------------            -----------------------            -----------------------
//	 auth     | T1 | "login"             billing  | T4 | "pay"              auth     | T7 | "refresh"
//	 billing  | T2 | "invoice"           auth     | T5 | "logout"           auth     | T8 | "login"
//	 cart     | T3 | "add"               cart     | T6 | "checkout"         billing  | T9 | "renew"
//
//	 MinKey = auth|T1                   MinKey = auth|T5                   MinKey = auth|T7
//	 MaxKey = cart|T3                   MaxKey = cart|T6                   MaxKey = billing|T9
//
// calculateRuns walks them in sorted order, looking for the run
// with the largest top_max_key strictly less than the section's MinKey
// (best-fit):
//
//   - A0 -> no runs exist -> new run #0 (top_max_key = cart|T3)
//   - B0 -> need a run with top_max_key < auth|T5. Run 0's top is cart|T3,
//     and cart|T3 > auth|T5 (cart > auth). No eligible run -> new run #1
//     (top_max_key = cart|T6)
//   - C0 -> need top_max_key < auth|T7. Both existing runs have cart|...
//     tops, all > auth|... -> new run #2 (top_max_key = billing|T9)
//
// Result: P=3 runs, one section each. With K=3, one task batch containing
// all three runs. The executor's K-way merge sorts by (service_name,
// __timestamp__) and emits new L1 sections that ARE sorted:
//
//	Section X1 (Schema=[service_name])   Section Y1 (Schema=[service_name])   Section Z1 (Schema=[service_name])
//	-----------------------------------  -----------------------------------  -----------------------------------
//	 auth     | T1 | "login"               auth     | T8 | "login"              billing  | T9 | "renew"
//	 auth     | T5 | "logout"              billing  | T2 | "invoice"            cart     | T3 | "add"
//	 auth     | T7 | "refresh"             billing  | T4 | "pay"                cart     | T6 | "checkout"
//
//	 MinKey = auth|T1                     MinKey = auth|T8                     MinKey = billing|T9
//	 MaxKey = auth|T7                     MaxKey = billing|T4                  MaxKey = cart|T6
//
// The MinKey/MaxKey ranges are now non-overlapping in sort-key space:
//
//	auth|T1..auth|T7  |  auth|T8..billing|T4  |  billing|T9..cart|T6
//	      X1                    Y1                       Z1
//
// A subsequent L1 -> L2 run on {X1, Y1, Z1} would yield P=1 indicating strong
// data locality and no need for compaction. Sections can be perfectly sorted by
// sort_schema and STILL overlap if each one covers the full sort_schema_value
// range. The size of P allows us to determine if compaction is necessary in
// these cases.
func calculateRuns(sections []*compactionv2pb.SectionRef) []*run {
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
