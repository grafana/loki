package rangeset

import (
	"cmp"
	"slices"
)

// Union returns a new set, holding the union of a and b.
func Union(a, b Set) Set {
	var out Set

	// Simple cases: if either set is empty, the union is the other set.
	switch {
	case len(a.ranges) == 0:
		out.ranges = append(out.ranges, b.ranges...)
		return out
	case len(b.ranges) == 0:
		out.ranges = append(out.ranges, a.ranges...)
		return out
	}

	// Do a union by appending a and b, then fixing overlaps.
	out.ranges = slices.Grow(out.ranges, len(a.ranges)+len(b.ranges))
	out.ranges = append(out.ranges, a.ranges...)
	out.ranges = append(out.ranges, b.ranges...)

	slices.SortFunc(out.ranges, func(a, b Range) int {
		return cmp.Compare(a.Start, b.Start)
	})

	out.normalize()
	return out
}
