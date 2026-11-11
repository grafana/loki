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

// object is one planning unit: one object, or the objects of one
// [Section.Unit].
type object[K any] struct {
	// For a unit of one object without a Section.Unit, unit is the path of
	// the object.
	unit     string
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
// sorted by bounds, then by unit name. The sections of a unit are in object
// order, by the lowest key of each object, and in section index order within
// an object.
func groupSectionsByUnit[K any](sections []Section[K], compare CompareFunc[K]) []object[K] {
	for _, section := range sections {
		if section.Ref == nil {
			panic("nil section reference")
		}
	}

	unitOf := func(section Section[K]) string {
		if section.Unit != "" {
			return section.Unit
		}
		return section.Ref.ObjectPath
	}
	byUnit := make(map[string]map[string][]Section[K])
	for _, section := range sections {
		unit := unitOf(section)
		if byUnit[unit] == nil {
			byUnit[unit] = make(map[string][]Section[K])
		}
		byUnit[unit][section.Ref.ObjectPath] = append(byUnit[unit][section.Ref.ObjectPath], section)
	}

	objects := make([]object[K], 0, len(byUnit))
	for unit, byPath := range byUnit {
		members := make([]object[K], 0, len(byPath))
		for path, objectSections := range byPath {
			members = append(members, newObject(path, objectSections, compare))
		}
		sortObjects(members, compare)

		group := members[0]
		group.unit = unit
		for _, member := range members[1:] {
			group.sections = append(group.sections, member.sections...)
			if compare(member.min, group.min) < 0 {
				group.min = member.min
			}
			if compare(member.max, group.max) > 0 {
				group.max = member.max
			}
		}
		objects = append(objects, group)
	}

	sortObjects(objects, compare)
	return objects
}

// newObject returns the object at path that holds sections, with its sections
// in section index order.
func newObject[K any](path string, sections []Section[K], compare CompareFunc[K]) object[K] {
	sort.Slice(sections, func(i, j int) bool {
		return sections[i].Ref.SectionIndex < sections[j].Ref.SectionIndex
	})

	obj := object[K]{
		unit:     path,
		sections: make([]*compactionv2pb.SectionRef, len(sections)),
		min:      sections[0].Min,
		max:      sections[0].Max,
	}
	for i, section := range sections {
		obj.sections[i] = section.Ref
		if compare(section.Min, obj.min) < 0 {
			obj.min = section.Min
		}
		if compare(section.Max, obj.max) > 0 {
			obj.max = section.Max
		}
	}
	return obj
}

// sortObjects sorts objects by their bounds, then by unit name.
func sortObjects[K any](objects []object[K], compare CompareFunc[K]) {
	sort.Slice(objects, func(i, j int) bool {
		a, b := objects[i], objects[j]
		if n := compare(a.min, b.min); n != 0 {
			return n < 0
		}
		if n := compare(a.max, b.max); n != 0 {
			return n < 0
		}
		return a.unit < b.unit
	})
}

func calculateRuns[K any](objects []object[K], compare CompareFunc[K]) []*run[K] {
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
