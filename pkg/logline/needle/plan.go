package needle

import (
	"slices"
	"strconv"
	"strings"
)

// Plan is an immutable compiled requirement in disjunctive normal form: a
// line must contain every literal of at least one conjunction. The zero value
// is TRUE. Only Expr.Compile constructs non-TRUE plans.
type Plan struct {
	// Both levels are sorted and deduplicated. Conjunctions are non-empty,
	// contain no redundant substrings, and do not imply another conjunction.
	conj [][]string
}

// IsTrue reports whether p requires nothing.
func (p Plan) IsTrue() bool { return len(p.conj) == 0 }

// Conjunctions returns a deep copy of the normalized conjunctions, or an
// empty slice for TRUE.
func (p Plan) Conjunctions() [][]string {
	out := make([][]string, len(p.conj))
	for i, c := range p.conj {
		out[i] = slices.Clone(c)
	}
	return out
}

// String formats p as `{"a", "b"} or {"c"}`, or as TRUE.
func (p Plan) String() string {
	if p.IsTrue() {
		return "TRUE"
	}
	var sb strings.Builder
	for i, c := range p.conj {
		if i > 0 {
			sb.WriteString(" or ")
		}
		sb.WriteByte('{')
		for j, lit := range c {
			if j > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(strconv.Quote(lit))
		}
		sb.WriteByte('}')
	}
	return sb.String()
}

func (p Plan) prune() Plan {
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
	return Plan{conj: out}
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
func product(a, b Plan) Plan {
	if a.IsTrue() {
		return b
	}
	if b.IsTrue() {
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
// belong to the source tree or another intermediate plan.
func normalize(conj [][]string) Plan {
	if len(conj) == 0 {
		return Plan{}
	}
	slices.SortFunc(conj, slices.Compare[[]string])
	conj = slices.CompactFunc(conj, slices.Equal[[]string])
	if len(conj) > 1 {
		conj = absorb(conj)
	}
	return Plan{conj: conj}
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
