package compactionv2

import (
	"slices"
	"sort"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

// run is one non-overlapping run of sections. Runs are kept in creation order
// in the calculator's `runs` slice (no deletes, no reordering), so a run's
// index in that slice equals its creation order.
//
// topMaxKey holds the most recent appended section's MaxKey tuple (the run's
// upper bound in sort-key space). It is compared via slices.Compare against
// other tuples; see the contract note on calculateRuns regarding equal
// tuple lengths.
type run struct {
	sections  []*compactionv2pb.SectionRef
	topMaxKey []string
}

// calculateRuns sorts the provided [sections] in place and returns a set of
// runs in creation order. A "run" is a sorted sequence of sections whose
// (MinKey, MaxKey) bounds are pairwise non-overlapping in sort-key space.
//
// MinKey/MaxKey are []string tuples -- one element per sort_schema column,
// in schema order. They are compared element-wise (via [slices.Compare]),
// which gives the schema's natural tuple order without the prefix/separator
// ambiguity of a concatenated-string representation. The caller is
// responsible for constructing them from each section's stats row.
//
// Contract: all SectionRefs passed in a single call must share the same
// tenant's sort_schema, so their MinKey/MaxKey tuples have equal length.
// Mixed-schema input is caught downstream at task-execution time. If a
// length mismatch reaches this function it indicates an upstream encoding
// bug; the comparison still produces a deterministic order via
// shorter-tuple-is-less semantics but the result is not semantically
// meaningful.
//
// The input slice is sorted in place; callers that need the original order
// must copy beforehand. The contract requires non-nil SectionRef entries;
// nil entries will panic.
func calculateRuns(sections []*compactionv2pb.SectionRef) []*run {
	if len(sections) == 0 {
		return nil
	}

	// Step 1: sort sections by (MinKey ASC, MaxKey ASC, ObjectPath ASC, SectionIndex ASC).
	// MinKey/MaxKey are []string tuples; slices.Compare gives element-wise
	// lexicographic order (matches the sort_schema's natural tuple order without
	// the prefix/separator problem of a concatenated-string representation).
	sort.Slice(sections, func(i, j int) bool {
		a, b := sections[i], sections[j]
		if c := slices.Compare(a.MinKey, b.MinKey); c != 0 {
			return c < 0
		}
		if c := slices.Compare(a.MaxKey, b.MaxKey); c != 0 {
			return c < 0
		}
		if a.ObjectPath != b.ObjectPath {
			return a.ObjectPath < b.ObjectPath
		}
		return a.SectionIndex < b.SectionIndex
	})

	// runs in creation order; this is what we return.
	runs := make([]*run, 0)

	// Iterate sorted sections and append to the existing run whose top_max_key is
	// the largest value strictly less than the section's MinKey; if no such run
	// exists, start a new one. Among runs tied on top_max_key, the OLDEST run
	// (first seen) wins. Below is a demonstration of how this algorithm allows us
	// to generate runs and make compaction decisions.
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
	// Result: P=3 runs, one section each. After compaction the new L1 sections
	// ARE sorted:
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
	// data locality. Sections can be perfectly sorted by sort_schema and STILL
	// overlap if each one covers the full sort_schema_value range. The size of P
	// allows us to determine if compaction is necessary in these cases.
	for _, s := range sections {
		var best *run
		for _, r := range runs {
			if slices.Compare(r.topMaxKey, s.MinKey) < 0 {
				if best == nil || slices.Compare(r.topMaxKey, best.topMaxKey) > 0 {
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
		})
	}

	return runs
}
