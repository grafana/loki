// Package needle combines the literals a log line must contain into a
// requirement that a logline index can look up.
//
// A requirement is kept in disjunctive normal form: an OR of conjunctions,
// each an AND of literals. A literal stands for "the line contains this
// text". The package knows nothing about n-grams, case or index versions.
// Callers map literals to what they can look up before they combine them.
//
// Every operation is sound. A line that satisfies the exact boolean formula
// also satisfies the result. When a result would grow past the caller's
// maxConjunctions, it is replaced by a weaker one, never by a stronger one.
package needle

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

// orAbsorbFactor sets how far above maxConjunctions an Or may go before it
// skips absorption and collapses directly. Absorption is quadratic, and an Or
// with thousands of branches would collapse anyway.
const orAbsorbFactor = 4

// Expr is a requirement on a line in disjunctive normal form. A line can match
// only if it contains every literal of at least one conjunction. The zero
// value is True, nothing is required.
//
// An Expr is immutable. Operations return a new value and never change their
// operands.
type Expr struct {
	// conj is sorted and has no duplicates. Each conjunction is sorted,
	// deduplicated and non-empty. No conjunction implies another one. nil
	// means True.
	conj [][]string
}

// True returns the Expr that every line satisfies.
func True() Expr { return Expr{} }

// Lit returns the requirement that a line contains s. The empty string is
// in every line, so Lit("") is True.
func Lit(s string) Expr {
	if s == "" {
		return True()
	}
	return Expr{conj: [][]string{{s}}}
}

// AllOf returns the requirement that a line contains every literal. Empty
// literals are skipped. With none left, the result is True.
func AllOf(lits ...string) Expr {
	c := canonical(lits)
	if len(c) == 0 {
		return True()
	}
	return Expr{conj: [][]string{c}}
}

// And returns the requirement that a line satisfies every operand.
//
// The product of the operands has one conjunction per combination of their
// conjunctions. Operands are folded from fewest to most conjunctions. When
// the next product would have more than maxConjunctions conjunctions, that
// operand is replaced by its Collapse first. The result never has more than
// maxConjunctions conjunctions. A value below 1 is treated as 1.
func And(maxConjunctions int, xs ...Expr) Expr {
	maxConjunctions = max(maxConjunctions, 1)

	ops := make([]Expr, 0, len(xs))
	for _, x := range xs {
		if !x.IsTrue() {
			ops = append(ops, x)
		}
	}
	slices.SortStableFunc(ops, func(a, b Expr) int {
		return cmp.Compare(len(a.conj), len(b.conj))
	})

	acc := True()
	for _, x := range ops {
		if max(len(acc.conj), 1)*len(x.conj) > maxConjunctions {
			x = x.Collapse()
		}
		switch {
		case x.IsTrue():
		case acc.IsTrue():
			acc = x
		default:
			acc = product(acc, x)
		}
	}
	return acc
}

// Or returns the requirement that a line satisfies at least one operand.
//
// A True operand makes the result True. With no operands the result is True
// too, because there is no False. When the union has more than maxConjunctions
// conjunctions after absorption, it is replaced by its Collapse. The result
// never has more than maxConjunctions conjunctions. A value below 1 is treated
// as 1.
func Or(maxConjunctions int, xs ...Expr) Expr {
	maxConjunctions = max(maxConjunctions, 1)
	if len(xs) == 0 {
		return True()
	}

	total := 0
	for _, x := range xs {
		if x.IsTrue() {
			return True()
		}
		total += len(x.conj)
	}
	all := make([][]string, 0, total)
	for _, x := range xs {
		all = append(all, x.conj...)
	}

	if total/orAbsorbFactor > maxConjunctions {
		return collapse(all)
	}
	e := normalize(all)
	if len(e.conj) > maxConjunctions {
		return e.Collapse()
	}
	return e
}

// Map rewrites every literal with f. When f returns false or an empty string,
// the literal is dropped from its conjunction, which is the same as replacing
// it with True. A conjunction that ends up empty makes the result True.
//
// f must return a literal that the original one implies, for example a
// shorter or a case-folded form of it. Otherwise the result is not sound.
func (e Expr) Map(f func(string) (string, bool)) Expr {
	out := make([][]string, 0, len(e.conj))
	for _, c := range e.conj {
		mapped := make([]string, 0, len(c))
		for _, lit := range c {
			if s, ok := f(lit); ok {
				mapped = append(mapped, s)
			}
		}
		mapped = canonical(mapped)
		if len(mapped) == 0 {
			return True()
		}
		out = append(out, mapped)
	}
	return normalize(out)
}

// Collapse returns AllOf the literals present in every conjunction, or True
// when there are none. Every line that satisfies e contains them, so the
// result is sound. It has at most one conjunction.
func (e Expr) Collapse() Expr {
	return collapse(e.conj)
}

// Prune drops each literal that is contained in another literal of the same
// conjunction. A line that contains the longer one also contains the shorter,
// so the requirement does not change.
//
// Call it only on the final Expr. Collapse keeps the literals present in
// every conjunction, and a redundant literal can be the one they share.
func (e Expr) Prune() Expr {
	out := make([][]string, 0, len(e.conj))
	for _, c := range e.conj {
		out = append(out, slices.DeleteFunc(slices.Clone(c), func(lit string) bool {
			return containedInOther(c, lit)
		}))
	}
	return normalize(out)
}

// IsTrue reports whether e requires nothing.
func (e Expr) IsTrue() bool { return len(e.conj) == 0 }

// Conjunctions returns a copy of the conjunctions in normal form. The result
// is empty when e is True.
func (e Expr) Conjunctions() [][]string {
	out := make([][]string, len(e.conj))
	for i, c := range e.conj {
		out[i] = slices.Clone(c)
	}
	return out
}

// String formats e as `{"a", "b"} or {"c"}`, or as TRUE.
func (e Expr) String() string {
	if e.IsTrue() {
		return "TRUE"
	}
	var sb strings.Builder
	for i, c := range e.conj {
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

// canonical returns a sorted and deduplicated copy of lits without empty
// strings.
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

// product returns the conjunction of every pair of conjunctions of a and b.
func product(a, b Expr) Expr {
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

// collapse returns AllOf the literals present in every conjunction.
func collapse(conj [][]string) Expr {
	if len(conj) == 0 {
		return True()
	}
	common := slices.Clone(conj[0])
	for _, c := range conj[1:] {
		common = slices.DeleteFunc(common, func(lit string) bool {
			_, found := slices.BinarySearch(c, lit)
			return !found
		})
		if len(common) == 0 {
			return True()
		}
	}
	return Expr{conj: [][]string{common}}
}

// normalize sorts, deduplicates and absorbs conj in place. Each conjunction
// must already be canonical. The caller must own the outer slice.
func normalize(conj [][]string) Expr {
	if len(conj) == 0 {
		return True()
	}
	slices.SortFunc(conj, slices.Compare[[]string])
	conj = slices.CompactFunc(conj, slices.Equal[[]string])
	if len(conj) > 1 {
		conj = absorb(conj)
	}
	return Expr{conj: conj}
}

// absorb drops every conjunction that implies another one. When B implies
// A, a line that satisfies B satisfies A, so "A or B" is A.
//
// Two different conjunctions can imply each other, for example {abc, abcd}
// and {abcd}. Then the one that sorts first is kept. Implication is
// transitive, so every dropped conjunction is implied by a kept one.
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

// implies reports whether a line that contains every literal of b must
// contain every literal of a. That holds when each literal of a is a
// substring of some literal of b.
func implies(b, a []string) bool {
	for _, x := range a {
		if !slices.ContainsFunc(b, func(y string) bool { return strings.Contains(y, x) }) {
			return false
		}
	}
	return true
}

// containedInOther reports whether lit is a substring of another literal of
// the conjunction c. c has no duplicates, so any other literal that contains
// lit is longer.
func containedInOther(c []string, lit string) bool {
	return slices.ContainsFunc(c, func(other string) bool {
		return other != lit && strings.Contains(other, lit)
	})
}
