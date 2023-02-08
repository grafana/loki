package wal

const (
	witness = byte('a')
)

// diffset is a simple implementation of a set that allows difference operations
type diffset map[int]byte

func newDiffset(elements []int) diffset {
	d := diffset{}
	for _, elem := range elements {
		d[elem] = witness
	}
	return d
}

// Difference calculates the set difference between a and b
func (a diffset) Difference(b diffset) diffset {
	diff := diffset{}
	// e \in diff iif e \in a && e \not \in b
	for keyInA := range a {
		if _, ok := b[keyInA]; !ok {
			diff[keyInA] = witness
		}
	}
	return diff
}
