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

// objectChain is the ordered sequence of sections belonging to one physical
// object, together with a conservative envelope covering all section bounds.
// Sections inside an object are already ordered by construction and must not be
// split into separate runs merely because their projected bounds look
// non-monotonic.
type objectChain[K any] struct {
	path     string
	sections []Section[K]
	min      K
	max      K
}

func (r *run[K]) Sections() []*compactionv2pb.SectionRef { return r.sections }

func (r *run[K]) Size() uint64 {
	var total uint64
	for _, section := range r.sections {
		total += uint64(section.UncompressedSize)
	}
	return total
}

func buildObjectChains[K any](sections []Section[K], compare CompareFunc[K]) []objectChain[K] {
	for _, section := range sections {
		if section.Ref == nil {
			panic("nil section reference")
		}
	}

	byPath := make(map[string][]Section[K])
	for _, section := range sections {
		byPath[section.Ref.ObjectPath] = append(byPath[section.Ref.ObjectPath], section)
	}

	chains := make([]objectChain[K], 0, len(byPath))
	for path, objectSections := range byPath {
		sort.Slice(objectSections, func(i, j int) bool {
			if objectSections[i].Ref.SectionIndex != objectSections[j].Ref.SectionIndex {
				return objectSections[i].Ref.SectionIndex < objectSections[j].Ref.SectionIndex
			}
			if n := compare(objectSections[i].Min, objectSections[j].Min); n != 0 {
				return n < 0
			}
			return compare(objectSections[i].Max, objectSections[j].Max) < 0
		})

		chain := objectChain[K]{
			path:     path,
			sections: objectSections,
			min:      objectSections[0].Min,
			max:      objectSections[0].Max,
		}
		for _, section := range objectSections {
			// Include both endpoints so the envelope remains valid even when an
			// object's projected section bounds appear backwards.
			if compare(section.Min, chain.min) < 0 {
				chain.min = section.Min
			}
			if compare(section.Max, chain.min) < 0 {
				chain.min = section.Max
			}
			if compare(section.Min, chain.max) > 0 {
				chain.max = section.Min
			}
			if compare(section.Max, chain.max) > 0 {
				chain.max = section.Max
			}
		}
		chains = append(chains, chain)
	}

	sort.Slice(chains, func(i, j int) bool {
		if n := compare(chains[i].min, chains[j].min); n != 0 {
			return n < 0
		}
		if n := compare(chains[i].max, chains[j].max); n != 0 {
			return n < 0
		}
		return chains[i].path < chains[j].path
	})

	// Preserve CalculateRuns' documented in-place sorting behavior. Callers see
	// objects in envelope order and sections in physical section order.
	var offset int
	for _, chain := range chains {
		offset += copy(sections[offset:], chain.sections)
	}
	return chains
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

	var runs []*run[K]
	for _, section := range sections {
		var best *run[K]
		for _, candidate := range runs {
			canFollow := compare(candidate.topMax, section.Min) <= 0
			isCloser := best == nil || compare(candidate.topMax, best.topMax) >= 0
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

func calculateObjectRuns[K any](sections []Section[K], compare CompareFunc[K]) []*run[K] {
	if len(sections) == 0 {
		return nil
	}
	chains := buildObjectChains(sections, compare)

	// Place each object chain in the run with the greatest upper bound that
	// still ends before this object starts. If no run is eligible, start a new one.
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
	for _, chain := range chains {
		var best *run[K]
		for _, candidate := range runs {
			canFollow := compare(candidate.topMax, chain.min) <= 0
			isCloser := best == nil || compare(candidate.topMax, best.topMax) >= 0
			if canFollow && isCloser {
				best = candidate
			}
		}

		refs := make([]*compactionv2pb.SectionRef, len(chain.sections))
		for i, section := range chain.sections {
			refs[i] = section.Ref
		}
		if best == nil {
			runs = append(runs, &run[K]{
				sections: refs,
				topMax:   chain.max,
			})
			continue
		}

		best.sections = append(best.sections, refs...)
		best.topMax = chain.max
	}
	return runs
}
