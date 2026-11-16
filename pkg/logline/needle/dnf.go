package needle

import (
	"slices"
	"strings"
)

// dnf is the compiler's private disjunctive normal form. Intermediate values
// may share literal slices with source nodes. Only the final prune produces
// fully owned, substring-pruned conjunctions for callers. Its zero value is TRUE.
type dnf struct {
	conj [][]string
}

func (p dnf) isTrue() bool { return len(p.conj) == 0 }

func (p dnf) prune() dnf {
	if p.isTrue() {
		return dnf{}
	}
	out := make([][]string, 0, len(p.conj))
	for _, c := range p.conj {
		longest := 0
		for _, lit := range c {
			longest = max(longest, len(lit))
		}
		out = append(out, slices.DeleteFunc(slices.Clone(c), func(lit string) bool {
			return len(lit) < longest && containedInOther(c, lit)
		}))
	}
	// Pruning preserves each conjunction's meaning, so it cannot introduce
	// new duplicates or implications. Only the lexicographic order can change.
	slices.SortFunc(out, slices.Compare[[]string])
	return dnf{conj: out}
}

// canonical returns a sorted, deduplicated copy without empty literals.
func canonical(lits []string) []string {
	out := make([]string, 0, len(lits))
	for _, lit := range lits {
		if lit != "" {
			out = append(out, lit)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// product is called only after checking the conjunction limit. TRUE is its
// identity. The resulting literals must not be pruned until compilation ends.
func product(a, b dnf) dnf {
	if a.isTrue() {
		return b
	}
	if b.isTrue() {
		return a
	}
	out := make([][]string, 0, len(a.conj)*len(b.conj))
	for _, ca := range a.conj {
		for _, cb := range b.conj {
			c := make([]string, 0, len(ca)+len(cb))
			c = append(c, ca...)
			c = append(c, cb...)
			slices.Sort(c)
			out = append(out, slices.Compact(c))
		}
	}
	return normalize(out)
}

// normalize owns and modifies only the outer slice. Literal slices can still
// belong to the source tree or another intermediate DNF.
func normalize(conj [][]string) dnf {
	if len(conj) == 0 {
		return dnf{}
	}
	slices.SortFunc(conj, slices.Compare[[]string])
	conj = slices.CompactFunc(conj, slices.Equal[[]string])
	if len(conj) > 1 {
		conj = absorb(conj)
	}
	return dnf{conj: conj}
}

// absorb drops conjunctions implying another one. For mutual implication,
// the lexicographically first conjunction wins deterministically.
func absorb(conj [][]string) [][]string {
	drop := make([]bool, len(conj))
	for j, b := range conj {
		for i, a := range conj {
			if i == j || !implies(b, a) {
				continue
			}
			if i < j || !implies(a, b) {
				drop[j] = true
				break
			}
		}
	}
	out := conj[:0]
	for i, c := range conj {
		if !drop[i] {
			out = append(out, c)
		}
	}
	return out
}

// impliesDNF reports whether each conjunction of a implies a conjunction of b.
// Empty DNFs mean TRUE, not an empty disjunction.
func impliesDNF(a, b dnf) bool {
	if b.isTrue() {
		return true
	}
	if a.isTrue() {
		return false
	}
	for _, stronger := range a.conj {
		found := false
		for _, weaker := range b.conj {
			if implies(stronger, weaker) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// implies reports whether containing every literal of b implies every literal
// of a. Exact membership handles the common shared-mandatory-literal case.
func implies(b, a []string) bool {
	for _, x := range a {
		if _, found := slices.BinarySearch(b, x); found {
			continue
		}
		if !containedInOther(b, x) {
			return false
		}
	}
	return true
}

// c has no duplicates, so only longer literals can contain lit.
func containedInOther(c []string, lit string) bool {
	return slices.ContainsFunc(c, func(other string) bool {
		return len(other) > len(lit) && strings.Contains(other, lit)
	})
}
