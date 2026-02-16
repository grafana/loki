package rangeset

// Intersect returns a new set, holding the intersection of a and b.
func Intersect(a, b Set) Set {
	var out Set

	if len(a.ranges) == 0 || len(b.ranges) == 0 {
		return out
	}

	for i, j := 0, 0; i < len(a.ranges) && j < len(b.ranges); {
		var (
			start = max(a.ranges[i].Start, b.ranges[j].Start)
			end   = min(a.ranges[i].End, b.ranges[j].End)
		)
		if start < end {
			out.ranges = append(out.ranges, Range{Start: start, End: end})
		}

		// Only move the index of the range that ends first, because:
		//
		// * If b.ranges[j] ends first, we need to see if b.ranges[j+1]
		//   overlaps.
		//
		// * If a.ranges[i] ends first, we need to see if a.ranges[i+1]
		//   overlaps.
		if a.ranges[i].End < b.ranges[j].End {
			i++
		} else {
			j++
		}
	}

	return out
}
