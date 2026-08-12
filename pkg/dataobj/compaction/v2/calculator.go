package compactionv2

import (
	"sort"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

// run is one ordered sequence built by calculateRuns. topMax is the upper
// bound of its final section.
type run[K any] struct {
	sections []*compactionv2pb.SectionRef
	topMax   K
}

func (r *run[K]) Sections() []*compactionv2pb.SectionRef { return r.sections }

func (r *run[K]) Size() uint64 {
	var total uint64
	for _, section := range r.sections {
		total += uint64(section.UncompressedSize)
	}
	return total
}

func sortSections[K any](sections []Section[K], compare CompareFunc[K]) {
	for _, section := range sections {
		if section.Ref == nil {
			panic("nil section reference")
		}
	}

	sort.Slice(sections, func(i, j int) bool {
		a, b := sections[i], sections[j]
		if n := compare(a.Min, b.Min); n != 0 {
			return n < 0
		}
		if n := compare(a.Max, b.Max); n != 0 {
			return n < 0
		}
		if a.Ref.ObjectPath != b.Ref.ObjectPath {
			return a.Ref.ObjectPath < b.Ref.ObjectPath
		}
		return a.Ref.SectionIndex < b.Ref.SectionIndex
	})
}

func calculateRuns[K any](sections []Section[K], compare CompareFunc[K]) []*run[K] {
	if len(sections) == 0 {
		return nil
	}
	sortSections(sections, compare)

	// Place each section in the run with the greatest upper bound that still
	// ends before this section starts. If no run is eligible, start a new one.
	// When two runs have the same upper bound, the oldest run wins.
	//
	// Consider three L0 sections sorted by timestamp rather than service_name.
	// Services interleave inside each section, so every section spans much of the
	// service_name keyspace:
	//
	//	Section A0                         Section B0                         Section C0
	//	----------                         ----------                         ----------
	//	auth    | T1 | "login"            billing | T4 | "pay"              auth    | T7 | "refresh"
	//	billing | T2 | "invoice"          auth    | T5 | "logout"           auth    | T8 | "login"
	//	cart    | T3 | "add"              cart    | T6 | "checkout"         billing | T9 | "renew"
	//
	//	Min = ["auth", T1]               Min = ["auth", T5]               Min = ["auth", T7]
	//	Max = ["cart", T3]               Max = ["cart", T6]               Max = ["billing", T9]
	//
	// Patience sorting considers them in lower-bound order:
	//   - A0 has no predecessor, so it starts run 0 with top ["cart", T3].
	//   - B0 starts run 1 because run 0's "cart" top cannot precede B0's
	//     "auth" lower bound.
	//   - C0 starts run 2 for the same reason.
	//
	// The result is three overlapping runs. A K-way merge can rewrite them into
	// sections that are ordered by service_name:
	//
	//	Section X1                         Section Y1                         Section Z1
	//	----------                         ----------                         ----------
	//	auth | T1 | "login"               auth    | T8 | "login"            billing | T9 | "renew"
	//	auth | T5 | "logout"              billing | T2 | "invoice"          cart    | T3 | "add"
	//	auth | T7 | "refresh"             billing | T4 | "pay"              cart    | T6 | "checkout"
	//
	//	Min = ["auth", T1]               Min = ["auth", T8]               Min = ["billing", T9]
	//	Max = ["auth", T7]               Max = ["billing", T4]            Max = ["cart", T6]
	//
	// A later calculation places X1, Y1, and Z1 in one run. This is why run count
	// measures locality even when the number of physical sections does not
	// change.
	var runs []*run[K]
	for _, section := range sections {
		var best *run[K]
		for _, candidate := range runs {
			canFollow := compare(candidate.topMax, section.Min) < 0
			isCloser := best == nil || compare(candidate.topMax, best.topMax) > 0
			if canFollow && isCloser {
				best = candidate
			}
		}

		if best == nil {
			runs = append(runs, &run[K]{
				sections: []*compactionv2pb.SectionRef{section.Ref},
				topMax:   section.Max,
			})
			continue
		}

		best.sections = append(best.sections, section.Ref)
		best.topMax = section.Max
	}
	return runs
}
