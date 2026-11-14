// Package needle combines the literals a log line must contain into a
// requirement that a logline index can look up. It knows nothing about
// n-grams, case or index versions: callers map literals before compilation.
//
// Expressions are immutable factored trees. Compile lowers them to bounded
// disjunctive normal form, weakening requirements when necessary, never
// excluding a line that satisfies the original formula.
package needle

// Expr is an immutable AND/OR tree over literals. Its zero value is TRUE.
// Constructing or mapping an expression does not expand it into conjunctions
// or apply a conjunction limit; Compile does that after all leaves are mapped.
type Expr struct {
	root *node
}

type operator uint8

const (
	literals operator = iota
	and
	or
)

type node struct {
	op       operator
	lits     []string // A compact AND of literal leaves, as constructed by AllOf.
	children []Expr
}

// True returns the expression that every line satisfies.
func True() Expr { return Expr{} }

// Lit requires a line to contain s. The empty string is in every line, so
// Lit("") is TRUE.
func Lit(s string) Expr {
	if s == "" {
		return True()
	}
	return Expr{root: &node{lits: []string{s}}}
}

// AllOf requires every non-empty literal. It copies lits and retains contained
// literals, which may be useful as substring candidates during compilation.
func AllOf(lits ...string) Expr {
	c := canonical(lits)
	if len(c) == 0 {
		return True()
	}
	return Expr{root: &node{lits: c}}
}

// And requires every operand. TRUE operands are identities. It copies xs
// without distributing ORs or copying the operands' trees.
func And(xs ...Expr) Expr { return combine(and, xs) }

// Or requires at least one operand. A TRUE operand makes the result TRUE.
// With no operands it is also TRUE: this package has no FALSE expression.
// It copies xs without expanding or limiting the expression.
func Or(xs ...Expr) Expr { return combine(or, xs) }

func combine(op operator, xs []Expr) Expr {
	if len(xs) == 1 || len(xs) == 2 && xs[0].root == xs[1].root {
		return xs[0]
	}
	children := make([]Expr, 0, len(xs))
	var seen map[*node]struct{}
	if len(xs) > 2 {
		seen = make(map[*node]struct{}, len(xs))
	}
	for _, x := range xs {
		if x.root == nil {
			if op == or {
				return True()
			}
			continue
		}
		if seen != nil {
			if _, ok := seen[x.root]; ok {
				continue
			}
			seen[x.root] = struct{}{}
		}
		children = append(children, x)
	}
	switch len(children) {
	case 0:
		return True()
	case 1:
		return children[0]
	default:
		return Expr{root: &node{op: op, children: children}}
	}
}

// Map rewrites literal leaves before compilation. A false result or an empty
// string replaces that leaf with TRUE, including inside ORs. Map does not
// expand, collapse or prune the tree and never modifies its receiver. Shared
// nodes are mapped once and their mapped output is reused.
//
// For soundness, f must return a requirement implied by the original literal
// under the caller's lookup semantics (for example a shorter or case-folded
// literal).
func (e Expr) Map(f func(string) (string, bool)) Expr {
	return e.mapLiterals(func(lits []string) Expr {
		mapped := make([]string, 0, len(lits))
		for _, lit := range lits {
			if s, ok := f(lit); ok {
				mapped = append(mapped, s)
			}
		}
		return AllOf(mapped...)
	})
}

// MapAll replaces each literal with the conjunction of f's non-empty results.
// No non-empty results replaces that leaf with TRUE; an OR branch that becomes
// TRUE makes its whole OR TRUE. Mapping precedes compilation: it does not
// expand, collapse, prune or cap the tree, and never modifies its receiver.
//
// Callback output is copied before the next callback, so f may reuse its slice.
// Shared nodes are mapped once and their mapped output is reused.
// For soundness, the original literal must imply every returned requirement
// under the caller's lookup semantics.
func (e Expr) MapAll(f func(string) []string) Expr {
	return e.mapLiterals(func(lits []string) Expr {
		mapped := make([]string, 0, len(lits))
		for _, lit := range lits {
			mapped = append(mapped, f(lit)...)
		}
		return AllOf(mapped...)
	})
}

// mapLiterals shares traversal and DAG memoization between leaf mappings.
// f receives each compact literal conjunction as a read-only slice.
func (e Expr) mapLiterals(f func([]string) Expr) Expr {
	if e.root == nil {
		return e
	}
	// Postorder traversal avoids recursion even for incremental tree chains.
	type frame struct {
		expr Expr
		next int
		out  []Expr
	}
	stack := []frame{{expr: e}}
	mappedNodes := make(map[*node]Expr)
	var result Expr
	for len(stack) > 0 {
		i := len(stack) - 1
		fr := &stack[i]
		n := fr.expr.root
		cached, ok := mappedNodes[n]
		switch {
		case ok:
			result = cached
		case n == nil:
			result = True()
		case n.op == literals:
			result = f(n.lits)
		case fr.next < len(n.children):
			child := n.children[fr.next]
			fr.next++
			stack = append(stack, frame{expr: child})
			continue
		default:
			result = combine(n.op, fr.out)
		}
		mappedNodes[n] = result
		stack = stack[:i]
		if i > 0 {
			stack[i-1].out = append(stack[i-1].out, result)
		}
	}
	return result
}
