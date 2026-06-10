package compactionv2

import (
	"cmp"
	"slices"
	"sort"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

// run is one non-overlapping run of sections.
//
// topMaxKey + topMaxTimestamp hold the most recent appended section's upper
// bound -- the composite (labels, timestamp) sort key.
type run struct {
	sections        []compactionv2pb.SectionRef
	topMaxKey       []string
	topMaxTimestamp int64
}

// cmpSortKey compares two composite (labels, timestamp) sort keys
// lexicographically -- labels first, then timestamp.
// Returns -1 if a < b, 0 if equal, +1 if a > b.
func cmpSortKey(aLabels []string, aTs int64, bLabels []string, bTs int64) int {
	if c := slices.Compare(aLabels, bLabels); c != 0 {
		return c
	}
	return cmp.Compare(aTs, bTs)
}

// calculateRuns sorts the provided [sections] in place and returns a set of
// runs in creation order. A "run" is a sorted sequence of sections whose
// ([MinKey, MinTimestamp], [MaxKey, MaxTimestamp]) bounds are pairwise
// non-overlapping in sort-key space.
//
// The input slice is sorted in place; callers that need the original order must
// copy beforehand. The contract requires non-nil SectionRef entries; nil
// entries will panic.
func calculateRuns(sections []compactionv2pb.SectionRef) []*run {
	if len(sections) == 0 {
		return nil
	}

	// Step 1: sort sections by composite (MinKey, MinTimestamp) ASC, then
	// composite (MaxKey, MaxTimestamp) ASC, then (ObjectPath, SectionIndex) as
	// a stable tiebreaker.
	sort.Slice(sections, func(i, j int) bool {
		a, b := &sections[i], &sections[j]
		if c := cmpSortKey(a.MinKey, a.MinTimestamp, b.MinKey, b.MinTimestamp); c != 0 {
			return c < 0
		}
		if c := cmpSortKey(a.MaxKey, a.MaxTimestamp, b.MaxKey, b.MaxTimestamp); c != 0 {
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
	//	 MinKey = ["auth", T1]              MinKey = ["auth", T5]              MinKey = ["auth", T7]
	//	 MaxKey = ["cart", T3]              MaxKey = ["cart", T6]              MaxKey = ["billing", T9]
	//
	// calculateRuns walks them in sorted order, looking for the run with the
	// largest top_max_key strictly less than the section's MinKey (best-fit).
	// Tuple compare proceeds element-wise; column 0 (service_name) dominates
	// for every comparison below.
	//   - A0 -> no runs exist -> new run #0 (top_max_key = ["cart", T3])
	//   - B0 -> need top_max_key < ["auth", T5]. Run 0's top is ["cart", T3];
	//     compare column 0: "cart" > "auth". Not eligible -> new run #1
	//     (top_max_key = ["cart", T6])
	//   - C0 -> need top_max_key < ["auth", T7]. Both existing runs have
	//     ["cart", ...] tops, all > ["auth", ...] at column 0. Not eligible ->
	//     new run #2 (top_max_key = ["billing", T9])
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
	//	 MinKey = ["auth", T1]                MinKey = ["auth", T8]                MinKey = ["billing", T9]
	//	 MaxKey = ["auth", T7]                MaxKey = ["billing", T4]             MaxKey = ["cart", T6]
	//
	// The MinKey/MaxKey ranges are now non-overlapping in sort-key space:
	//
	//	(["auth",T1]..["auth",T7])..(["auth",T8]..["billing",T4])..(["billing",T9]..["cart",T6])
	//	           X1                            Y1                              Z1
	//
	// A subsequent L1 -> L2 run on {X1, Y1, Z1} would yield P=1 indicating strong
	// data locality. Sections can be perfectly sorted by sort_schema and STILL
	// overlap if each one covers the full sort_schema_value range. The size of P
	// allows us to determine if compaction is necessary in these cases.
	for _, s := range sections {
		var best *run
		for _, r := range runs {
			if cmpSortKey(r.topMaxKey, r.topMaxTimestamp, s.MinKey, s.MinTimestamp) < 0 {
				if best == nil || cmpSortKey(r.topMaxKey, r.topMaxTimestamp, best.topMaxKey, best.topMaxTimestamp) > 0 {
					best = r
				}
			}
		}

		if best != nil {
			best.sections = append(best.sections, s)
			best.topMaxKey = s.MaxKey
			best.topMaxTimestamp = s.MaxTimestamp
			continue
		}

		// No eligible pile: start a new one.
		runs = append(runs, &run{
			sections:        []compactionv2pb.SectionRef{s},
			topMaxKey:       s.MaxKey,
			topMaxTimestamp: s.MaxTimestamp,
		})
	}

	return runs
}
