package needle

import (
	"cmp"
	"slices"
)

// orAbsorbFactor bounds the union considered for quadratic OR absorption.
// Duplicate and absorbed conjunctions do not count toward this allowance.
const orAbsorbFactor = 4

// Compile lowers e to bounded disjunctive normal form. A line must contain
// every literal of at least one conjunction. A nil or empty outer slice means
// TRUE; inner slices are never empty. Both slice levels are sorted, normalized
// and caller-owned: mutating them cannot affect e or any other compilation.
//
// Values below one are treated as one. Products are checked before expansion;
// oversized operands or unions are weakened to their implied existing
// literals, including substrings. No new substrings are invented. Redundant
// literals remain available for every collapse decision and are pruned last.
func (e Expr) Compile(maxConjunctions int) Conjunctions {
	c := compiler{
		root:  e,
		limit: max(maxConjunctions, 1),
		plans: make(map[*node]dnf),
	}
	return c.lower(e).prune().conj
}

type compiler struct {
	root       Expr
	limit      int
	plans      map[*node]dnf
	collapsed  map[*node]dnf
	candidates []string
	proofs     map[implicationKey]bool
	scratch    []implicationFrame
	costs      map[*node]int
}

func (c *compiler) lower(e Expr) dnf {
	if e.root == nil {
		return dnf{}
	}
	if e.root.op == literals {
		return dnf{conj: [][]string{e.root.lits}}
	}
	if p, ok := c.plans[e.root]; ok {
		return p
	}
	ops := flatten(e)
	var p dnf
	if e.root.op == or {
		p = c.lowerOr(e, ops)
	} else {
		p = c.lowerAnd(ops)
	}
	c.plans[e.root] = p
	return p
}

// flatten walks associative chains in source order without recursion. The
// same node is visited once, even if a shared DAG has exponentially many paths.
func flatten(e Expr) []Expr {
	op := e.root.op
	stack := []Expr{e}
	seen := make(map[*node]struct{})
	var out []Expr
	for len(stack) > 0 {
		i := len(stack) - 1
		x := stack[i]
		stack = stack[:i]
		if _, ok := seen[x.root]; ok {
			continue
		}
		seen[x.root] = struct{}{}
		if x.root != nil && x.root.op == op {
			for j := len(x.root.children) - 1; j >= 0; j-- {
				stack = append(stack, x.root.children[j])
			}
		} else {
			out = append(out, x)
		}
	}
	return out
}

func (c *compiler) lowerOr(source Expr, ops []Expr) dnf {
	var all [][]string
	for _, x := range ops {
		p := c.lower(x)
		if p.isTrue() {
			// A bounded child may have lost a substring that exists only as
			// a candidate in another branch. Consult the entire source OR.
			return c.collapse(source)
		}
		for _, conjunction := range p.conj {
			all = append(all, conjunction)
			// Preserve the original total/4 > cap threshold without integer
			// overflow. Normalize each full batch before deciding to collapse;
			// a long sequence of duplicates can remain an exact small union.
			if len(all)/orAbsorbFactor > c.limit {
				all = normalize(all).conj
				if len(all)/orAbsorbFactor > c.limit {
					return c.collapse(source)
				}
			}
		}
	}
	p := normalize(all)
	if len(p.conj) > c.limit {
		return c.collapse(source)
	}
	return p
}

func (c *compiler) lowerAnd(xs []Expr) dnf {
	type operand struct {
		source Expr
		plan   dnf
	}
	var mandatory []string
	ops := make([]operand, 0, len(xs))
	for _, x := range xs {
		if x.root != nil && x.root.op == literals {
			mandatory = append(mandatory, x.root.lits...)
			continue
		}
		p := c.lower(x)
		switch len(p.conj) {
		case 0:
		case 1:
			mandatory = append(mandatory, p.conj[0]...)
		default:
			ops = append(ops, operand{x, p})
		}
	}
	slices.SortStableFunc(ops, func(a, b operand) int {
		return cmp.Compare(len(a.plan.conj), len(b.plan.conj))
	})
	acc := literalDNF(mandatory)
	for _, x := range ops {
		p := x.plan
		// Division avoids overflowing before deciding whether to expand.
		if max(len(acc.conj), 1) > c.limit/len(p.conj) {
			p = c.collapse(x.source)
		}
		// A collapsed operand can absorb alternatives in acc. Apply and
		// normalize it before deciding whether the next product fits.
		acc = product(acc, p)
	}
	return acc
}

func literalDNF(lits []string) dnf {
	c := canonical(lits)
	if len(c) == 0 {
		return dnf{}
	}
	return dnf{conj: [][]string{c}}
}

// collapse considers candidates from the entire source subtree, even branches
// removed by absorption or earlier bounded lowering. Source node identities
// remain intact so implication proofs can be reused across nested collapses.
func (c *compiler) collapse(e Expr) dnf {
	if p, ok := c.collapsed[e.root]; ok {
		return p
	}
	// Every implied candidate must be contained in this one exact source
	// conjunction. This is only a rejection filter: candidates still come
	// from every branch and must pass the memoized source proof below.
	witness := c.sourceConjunction(e)
	var common []string
	for _, lit := range c.allCandidates() {
		if implies(witness, []string{lit}) && c.prove(e, lit, false) && c.prove(e, lit, true) {
			common = append(common, lit)
		}
	}
	var p dnf
	if len(common) > 0 {
		p = dnf{conj: [][]string{common}}
	}
	if c.collapsed == nil {
		c.collapsed = make(map[*node]dnf)
	}
	c.collapsed[e.root] = p
	return p
}

// Index candidates once for the whole compilation, not once for every nested
// collapse. The occurrence proof in collapse restricts them to that subtree.
func (c *compiler) allCandidates() []string {
	if c.candidates != nil {
		return c.candidates
	}
	var lits []string
	stack := []Expr{c.root}
	seen := make(map[*node]struct{})
	for len(stack) > 0 {
		i := len(stack) - 1
		n := stack[i].root
		stack = stack[:i]
		if n == nil {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		lits = append(lits, n.lits...)
		stack = append(stack, n.children...)
	}
	c.candidates = canonical(lits)
	return c.candidates
}

// witnessCost counts literal leaves in a cheap exact source conjunction:
// minimum for OR, saturating sum for AND. Cache only a scalar per source node,
// not growing literal slices. Iterative postorder also handles long DAG chains.
func (c *compiler) witnessCost(e Expr) int {
	if c.costs == nil {
		c.costs = map[*node]int{nil: 0}
	}
	type frame struct {
		node *node
		next int
	}
	const maxCost = int(^uint(0) >> 1)
	stack := []frame{{node: e.root}}
	for len(stack) > 0 {
		i := len(stack) - 1
		fr := &stack[i]
		n := fr.node
		if _, ok := c.costs[n]; ok {
			stack = stack[:i]
			continue
		}
		if fr.next < len(n.children) {
			child := n.children[fr.next].root
			fr.next++
			stack = append(stack, frame{node: child})
			continue
		}
		cost := len(n.lits)
		switch n.op {
		case or:
			cost = maxCost
			for _, child := range n.children {
				cost = min(cost, c.costs[child.root])
			}
		case and:
			for _, child := range n.children {
				next := c.costs[child.root]
				if next > maxCost-cost {
					cost = maxCost
					break
				}
				cost += next
			}
		}
		c.costs[n] = cost
		stack = stack[:i]
	}
	return c.costs[e.root]
}

// cheapestChild requires witness costs for n's subtree to have been computed.
func (c *compiler) cheapestChild(n *node) int {
	best := 0
	for i := 1; i < len(n.children); i++ {
		if c.costs[n.children[i].root] < c.costs[n.children[best].root] {
			best = i
		}
	}
	return best
}

// sourceConjunction picks one exact DNF conjunction without distributing:
// choose a cheapest branch of each OR and every operand of each AND. It is
// only a rejection filter, never a weakened source plan.
func (c *compiler) sourceConjunction(e Expr) []string {
	c.witnessCost(e)
	var lits []string
	stack := []Expr{e}
	seen := make(map[*node]struct{})
	for len(stack) > 0 {
		i := len(stack) - 1
		n := stack[i].root
		stack = stack[:i]
		if n == nil {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		switch n.op {
		case literals:
			lits = append(lits, n.lits...)
		case or:
			stack = append(stack, n.children[c.cheapestChild(n)])
		case and:
			stack = append(stack, n.children...)
		}
	}
	return canonical(lits)
}

type implicationKey struct {
	node       *node
	literal    string
	occurrence bool
}

type implicationFrame struct {
	expr  Expr
	next  int
	first int
}

// prove answers either implication or literal occurrence. An AND implies a
// literal if any operand does; an OR only if every operand does. Occurrence
// needs any operand for either operator. Both queries are memoized per compile.
// Check a cheapest operand first, then the remaining operands in reverse
// order, so rejecting a candidate does not depend on the source branch order.
func (c *compiler) prove(e Expr, lit string, occurrence bool) bool {
	if c.proofs == nil {
		c.proofs = make(map[implicationKey]bool)
	}
	stack := append(c.scratch[:0], implicationFrame{expr: e})
	var result bool
	for len(stack) > 0 {
		i := len(stack) - 1
		fr := &stack[i]
		n := fr.expr.root
		cached, ok := c.proofs[implicationKey{n, lit, occurrence}]
		switch {
		case ok:
			result = cached
		case n == nil:
			result = false
		case n.op == literals:
			if occurrence {
				_, result = slices.BinarySearch(n.lits, lit)
			} else {
				result = implies(n.lits, []string{lit})
			}
		case fr.next < len(n.children):
			if fr.next == 0 {
				fr.first = c.cheapestChild(n)
			}
			j := fr.first
			if fr.next > 0 {
				j = len(n.children) - fr.next
				if j <= fr.first {
					j--
				}
			}
			child := n.children[j]
			fr.next++
			stack = append(stack, implicationFrame{expr: child})
			continue
		default:
			result = n.op == or && !occurrence
		}
		c.proofs[implicationKey{n, lit, occurrence}] = result
		stack = stack[:i]
		// Finish and memoize parents while this result short-circuits them.
		for len(stack) > 0 {
			parent := &stack[len(stack)-1]
			if (parent.expr.root.op == and || occurrence) != result {
				break
			}
			c.proofs[implicationKey{parent.expr.root, lit, occurrence}] = result
			stack = stack[:len(stack)-1]
		}
	}
	c.scratch = stack
	return result
}
