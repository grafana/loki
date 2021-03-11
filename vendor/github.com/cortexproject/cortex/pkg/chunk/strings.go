package chunk

import "sort"

func uniqueStrings(cs []string) []string {
	if len(cs) == 0 {
		return []string{}
	}

	result := make([]string, 1, len(cs))
	result[0] = cs[0]
	i, j := 0, 1
	for j < len(cs) {
		if result[i] == cs[j] {
			j++
			continue
		}
		result = append(result, cs[j])
		i++
		j++
	}
	return result
}

func intersectStrings(left, right []string) []string {
	var (
		i, j   = 0, 0
		result = []string{}
	)
	for i < len(left) && j < len(right) {
		if left[i] == right[j] {
			result = append(result, left[i])
		}

		if left[i] < right[j] {
			i++
		} else {
			j++
		}
	}
	return result
}

//nolint:unused //Ignoring linting as this might be useful
func nWayIntersectStrings(sets [][]string) []string {
	l := len(sets)
	switch l {
	case 0:
		return []string{}
	case 1:
		return sets[0]
	case 2:
		return intersectStrings(sets[0], sets[1])
	default:
		var (
			split = l / 2
			left  = nWayIntersectStrings(sets[:split])
			right = nWayIntersectStrings(sets[split:])
		)
		return intersectStrings(left, right)
	}
}

// UniqueStrings keeps a slice of unique strings.
type UniqueStrings struct {
	values map[string]struct{}
	result []string
}

// Add adds a new string, dropping duplicates.
func (us *UniqueStrings) Add(strings ...string) {
	for _, s := range strings {
		if _, ok := us.values[s]; ok {
			continue
		}
		if us.values == nil {
			us.values = map[string]struct{}{}
		}
		us.values[s] = struct{}{}
		us.result = append(us.result, s)
	}
}

// Strings returns the sorted sliced of unique strings.
func (us UniqueStrings) Strings() []string {
	sort.Strings(us.result)
	return us.result
}
