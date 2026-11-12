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

// unit is one planning unit: the objects of one [Section.PlanningUnit], with the
// union of their bounds.
type unit[K any] struct {
	name     string
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

// groupSectionsByUnit groups sections into their units, and returns the units
// sorted by bounds, then by name. The sections of a unit are in object order,
// by the lowest key of each object, and in section index order within an
// object.
func groupSectionsByUnit[K any](sections []Section[K], compare CompareFunc[K]) []unit[K] {
	for _, section := range sections {
		if section.Ref == nil {
			panic("nil section reference")
		}
		if section.PlanningUnit == "" {
			panic("section has no unit")
		}
	}

	byUnit := make(map[string]map[string][]Section[K])
	for _, section := range sections {
		if byUnit[section.PlanningUnit] == nil {
			byUnit[section.PlanningUnit] = make(map[string][]Section[K])
		}
		byUnit[section.PlanningUnit][section.Ref.ObjectPath] = append(byUnit[section.PlanningUnit][section.Ref.ObjectPath], section)
	}

	units := make([]unit[K], 0, len(byUnit))
	for name, byPath := range byUnit {
		// Order the objects of the unit by their own bounds, then join them.
		objects := make([]unit[K], 0, len(byPath))
		for path, objectSections := range byPath {
			objects = append(objects, newUnit(path, objectSections, compare))
		}
		sortUnits(objects, compare)

		joined := objects[0]
		joined.name = name
		for _, obj := range objects[1:] {
			joined.sections = append(joined.sections, obj.sections...)
			if compare(obj.min, joined.min) < 0 {
				joined.min = obj.min
			}
			if compare(obj.max, joined.max) > 0 {
				joined.max = obj.max
			}
		}
		units = append(units, joined)
	}

	sortUnits(units, compare)
	return units
}

// newUnit returns the unit named name that holds sections, with its sections
// in section index order.
func newUnit[K any](name string, sections []Section[K], compare CompareFunc[K]) unit[K] {
	sort.Slice(sections, func(i, j int) bool {
		return sections[i].Ref.SectionIndex < sections[j].Ref.SectionIndex
	})

	u := unit[K]{
		name:     name,
		sections: make([]*compactionv2pb.SectionRef, len(sections)),
		min:      sections[0].Min,
		max:      sections[0].Max,
	}
	for i, section := range sections {
		u.sections[i] = section.Ref
		if compare(section.Min, u.min) < 0 {
			u.min = section.Min
		}
		if compare(section.Max, u.max) > 0 {
			u.max = section.Max
		}
	}
	return u
}

// sortUnits sorts units by their bounds, then by name.
func sortUnits[K any](units []unit[K], compare CompareFunc[K]) {
	sort.Slice(units, func(i, j int) bool {
		a, b := units[i], units[j]
		if n := compare(a.min, b.min); n != 0 {
			return n < 0
		}
		if n := compare(a.max, b.max); n != 0 {
			return n < 0
		}
		return a.name < b.name
	})
}

func calculateRuns[K any](units []unit[K], compare CompareFunc[K]) []*run[K] {
	// Place each unit in the run with the greatest upper bound that still
	// ends before the unit starts. If no run is eligible, start a new one.
	// Planning assumes each unit is internally sorted, so it treats a unit as one block.
	//
	// When two runs have the same upper bound, the oldest run wins.
	//
	// Consider three L0 objects sorted by timestamp rather than service_name. The objects may have multiple sections.
	// Services interleave inside each object, so every object spans much of the
	// service_name keyspace:
	//
	//	Object A0                          Object B0                          Object C0
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
	// objects that are ordered by service_name:
	//
	//	Object X1                          Object Y1                          Object Z1
	//	----------                         ----------                         ----------
	//	auth | T1 | "login"               auth    | T8 | "login"            billing | T9 | "renew"
	//	auth | T5 | "logout"              billing | T2 | "invoice"          cart    | T3 | "add"
	//	auth | T7 | "refresh"             billing | T4 | "pay"              cart    | T6 | "checkout"
	//
	//	Min = ["auth", T1]               Min = ["auth", T8]               Min = ["billing", T9]
	//	Max = ["auth", T7]               Max = ["billing", T4]            Max = ["cart", T6]
	//
	// A later calculation places X1, Y1, and Z1 in one run. This is why run count
	// measures locality even when the number of physical objects does not
	// change.
	var runs []*run[K]
	for _, u := range units {
		var best *run[K]
		for _, candidate := range runs {
			canFollow := compare(candidate.topMax, u.min) < 0
			isCloser := best == nil || compare(candidate.topMax, best.topMax) > 0
			if canFollow && isCloser {
				best = candidate
			}
		}

		if best == nil {
			runs = append(runs, &run[K]{
				sections: append([]*compactionv2pb.SectionRef(nil), u.sections...),
				topMax:   u.max,
			})
			continue
		}

		best.sections = append(best.sections, u.sections...)
		best.topMax = u.max
	}
	return runs
}
