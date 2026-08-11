package compactionv2

import (
	"sort"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

// run is one ordered sequence built by the calculateRuns. topMax is the upper
// bound of its final section.
type run[K any] struct {
	sections []*compactionv2pb.SectionRef
	topMax   K
}

type object[K any] struct {
	path     string
	sections []*compactionv2pb.SectionRef
	min, max K
}

func (r *run[K]) Sections() []*compactionv2pb.SectionRef { return r.sections }

func (r *run[K]) Size() uint64 {
	var total uint64
	for _, section := range r.sections {
		total += uint64(section.UncompressedSize)
	}
	return total
}

func groupSectionsByObject[K any](sections []Section[K], compare CompareFunc[K]) []object[K] {
	for _, section := range sections {
		if section.Ref == nil {
			panic("nil section reference")
		}
	}

	byPath := make(map[string][]Section[K])
	for _, section := range sections {
		path := section.Ref.ObjectPath
		byPath[path] = append(byPath[path], section)
	}

	objects := make([]object[K], 0, len(byPath))
	for path, objectSections := range byPath {
		// Sort indexes
		sort.Slice(objectSections, func(i, j int) bool {
			a, b := objectSections[i], objectSections[j]
			return a.Ref.SectionIndex < b.Ref.SectionIndex
		})

		group := object[K]{
			path:     path,
			sections: make([]*compactionv2pb.SectionRef, len(objectSections)),
			min:      objectSections[0].Min,
			max:      objectSections[0].Max,
		}
		for i, section := range objectSections {
			group.sections[i] = section.Ref
			if compare(section.Min, group.min) < 0 {
				group.min = section.Min
			}
			if compare(section.Max, group.max) > 0 {
				group.max = section.Max
			}
		}
		objects = append(objects, group)
	}

	sort.Slice(objects, func(i, j int) bool {
		a, b := objects[i], objects[j]
		if n := compare(a.min, b.min); n != 0 {
			return n < 0
		}
		if n := compare(a.max, b.max); n != 0 {
			return n < 0
		}
		return a.path < b.path
	})
	return objects
}

func calculateRuns[K any](objects []object[K], compare CompareFunc[K]) []*run[K] {
	// Place each object in the run with the greatest upper bound that still
	// ends before this object starts. If no run is eligible, start a new one.
	// An object is assumed to be internally sorted and is considered a single Run to simplify planning.
	//
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
	for _, obj := range objects {
		var best *run[K]
		for _, candidate := range runs {
			canFollow := compare(candidate.topMax, obj.min) < 0
			isCloser := best == nil || compare(candidate.topMax, best.topMax) > 0
			if canFollow && isCloser {
				best = candidate
			}
		}

		if best == nil {
			runs = append(runs, &run[K]{
				sections: append([]*compactionv2pb.SectionRef(nil), obj.sections...),
				topMax:   obj.max,
			})
			continue
		}

		best.sections = append(best.sections, obj.sections...)
		best.topMax = obj.max
	}
	return runs
}
