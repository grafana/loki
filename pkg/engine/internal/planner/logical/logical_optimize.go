package logical

import (
	"fmt"
	"iter"
	"slices"

	"github.com/grafana/regexp/syntax"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/util"
)

type optimizationPass interface {
	Apply(*Plan) error
}

// Optimize performs optimizations on p.
func Optimize(p *Plan) error {
	passes := []optimizationPass{
		simplifyRegexPass{},
	}

	for _, pass := range passes {
		if err := pass.Apply(p); err != nil {
			return err
		}
	}

	// After running all passes, we need to rerun the SSA builder to correct
	// IDs, as nodes may have moved around.
	var b ssaBuilder
	b.process(p.Instructions[len(p.Instructions)-1])

	return nil
}

type simplifyRegexPass struct{}

func (pass simplifyRegexPass) Apply(p *Plan) error {
	var operandBuffer []*Value

	// NOTE(rfratto): We can't do a range loop here because we're modifying the
	// slice as we iterate over it.
	for i := 0; i < len(p.Instructions); i++ {
		instr := p.Instructions[i]
		b, ok := instr.(*BinOp)
		if !ok || !pass.shouldApply(b) {
			continue
		}

		simplified, changed, err := pass.simplifyBinop(b)
		if err != nil {
			return err
		} else if !changed {
			// No simplification performed.
			continue
		}

		// The final element in simplified becomes the "return" of the regex
		// simplification, and is what other nodes will now reference.
		newValue, ok := simplified[len(simplified)-1].(Value)
		if !ok {
			return fmt.Errorf("expected regex simplify to end in a Value")
		}

		instrs := extractInstructions(simplified)
		buildReferrers(instrs...) // Hook up referrers for the new instructions.

		// Replace the binop instruction with the simplified set.
		p.Instructions = slices.Replace(p.Instructions, i, i+1, instrs...)

		// Fix references: any referrers to binop should now instead refer to
		// the final instruction in simplified.
		for _, ref := range *b.Referrers() {
			operandBuffer = ref.Operands(operandBuffer[:0])
			for _, operand := range operandBuffer {
				if *operand != b {
					continue
				}
				*operand = newValue
			}
			newValue.base().addReferrer(ref)
		}
	}

	return nil
}

func (pass simplifyRegexPass) shouldApply(b *BinOp) bool {
	// We can only support regex simplification on actual regex operations.
	if b.Op != types.BinaryOpMatchRe && b.Op != types.BinaryOpNotMatchRe {
		return false
	}

	// At the moment, we don't support simplifying regex on stream selectors:
	//
	//  1. The logic copied from the classic engine produces incorrect results,
	//     un-anchoring regexes.
	//  2. The metastore catalogue expects to be able to convert expressions back
	//     into Prometheus label matchers, which doesn't support the simplified
	//     expressions (such as MATCH_SUBSTR)
	//
	// To avoid this, we'll skip any binop which is found in a MakeTable
	// selector.
	for ref := range pass.walkReferences(b) {
		mt, ok := ref.(*MakeTable)
		if !ok {
			continue
		}

		// We need to see if our bin-op is reachable from specifically
		// MakeTable's selector, and not just its predicates.
		//
		// TODO(rfratto): MakeTable contains predicates *only* to propagate down
		// bloom filters. Can we find a way to do this without storing the
		// predicates in the MakeTable? That would allow removing this entire
		// slow check.
		var inSelector bool
		walkNode(mt.Selector, func(n Node) bool {
			if n == b {
				inSelector = true
				return false
			}
			return true
		})
		if inSelector {
			return false
		}
	}

	return true
}

// walkReferences returns an iterator over all instructions that directly or
// indirectly reference v.
func (pass simplifyRegexPass) walkReferences(v Value) iter.Seq[Instruction] {
	return func(yield func(Instruction) bool) {
		var (
			stack = []Value{v}
			seen  = map[Node]struct{}{v: {}}
		)

		for len(stack) > 0 {
			// Pop a node from the stack.
			next := stack[0]
			stack = stack[1:]

			for _, ref := range *next.Referrers() {
				if _, seen := seen[ref]; seen {
					continue
				}
				seen[ref] = struct{}{}

				if !yield(ref) {
					return
				}

				refValue, ok := ref.(Value)
				if !ok {
					continue
				}
				stack = append(stack, refValue)
			}
		}
	}
}

func (pass simplifyRegexPass) simplifyBinop(b *BinOp) (simplified []Node, changed bool, err error) {
	if b.Op != types.BinaryOpMatchRe && b.Op != types.BinaryOpNotMatchRe {
		panic("simplifyRegex called with non-regex binary operation")
	}

	lit, ok := b.Right.(*Literal)
	if !ok {
		return nil, false, nil
	}

	expr, ok := lit.Value().(string)
	if !ok {
		return nil, false, nil
	}

	reg, err := syntax.Parse(expr, syntax.Perl)
	if err != nil {
		return nil, false, err
	}

	// True if from is matching against the message column.
	var isMessage bool
	if ref, ok := b.Left.(*ColumnRef); ok {
		isMessage = ref.Ref.Type == types.ColumnTypeBuiltin && ref.Ref.Column == types.ColumnNameBuiltinMessage
	}

	nodes, changed := pass.simplifyRegex(b.Left, isMessage, reg.Simplify())

	if changed && b.Op == types.BinaryOpNotMatchRe {
		// Add a final instruction to invert the match.
		nodes = append(nodes, &UnaryOp{
			Op:    types.UnaryOpNot,
			Value: nodes[len(nodes)-1].(Value),
		})
	}

	return nodes, changed, nil
}

func (pass simplifyRegexPass) simplifyRegex(from Value, isMessage bool, reg *syntax.Regexp) ([]Node, bool) {
	if util.IsCaseInsensitive(reg) {
		// TODO(rfratto): To support case-insensitive regex simplification, we
		// need to have new binary operations for case-insensitive equality and
		// substring searches.
		return nil, false
	}

	// Based off of RegexSimplifier in pkg/logql/log/filter.go.

	switch reg.Op {
	case syntax.OpAlternate:
		// Expressions like "foo|bar" simplify to "foo" || "bar".
		util.ClearCapture(reg.Sub...)

		var result []Node

		f, ok := pass.simplifyRegex(from, isMessage, reg.Sub[0])
		if !ok {
			return nil, false
		}
		result = chainOr(nil, f...)

		// Merge in the rest of the alternates.
		for _, reg := range reg.Sub[1:] {
			f2, ok := pass.simplifyRegex(from, isMessage, reg)
			if !ok {
				return nil, false
			}
			result = chainOr(result, f2...)
		}

		return result, true

	case syntax.OpConcat:
		return pass.simplifyRegexConcat(from, reg, nil)

	case syntax.OpCapture:
		// Remove capture groups.
		util.ClearCapture(reg)
		return pass.simplifyRegex(from, isMessage, reg)

	case syntax.OpLiteral:
		// Literal strings like "foo" simplifies to a basic substr/equality check.
		//
		// NOTE(rfratto): Matching the logic from the old engine, only the
		// top-level literals use full equality for non-message columns.
		op := types.BinaryOpMatchSubstr
		if !isMessage {
			// Regex searches on non-message columns check for equality rather
			// than a contains.
			op = types.BinaryOpEq
		}
		return []Node{
			&BinOp{
				Left:  from,
				Right: &Literal{inner: types.StringLiteral(reg.Rune)},
				Op:    op,
			},
		}, true

	case syntax.OpStar:
		if reg.Sub[0].Op == syntax.OpAnyCharNotNL {
			// ".*" simplifies to an always-pass expression, so we propagate the
			// source back up.
			return []Node{from}, true
		}

	case syntax.OpPlus:
		if len(reg.Sub) == 1 && reg.Sub[0].Op == syntax.OpAnyCharNotNL {
			// ".+" simplifies to <from> != "" (not an empty string).
			return []Node{
				&BinOp{
					Left:  from,
					Right: &Literal{inner: types.StringLiteral("")},
					Op:    types.BinaryOpNeq,
				},
			}, true
		}

	case syntax.OpEmptyMatch:
		// "()" simplifies to an always-pass expression, so we propagate the
		// source back up.
		return []Node{from}, true
	}

	return nil, false
}

// simplifyRegexConcat attempts to simplify regex concatenations. Supported
// concatenations are either
//
//   - a combination of literals and star ("foo.*", ".*foo.*", ".*foo"), or
//   - a literal and alternates ("hello(world|universe)"), which represent a multiplication of alternates.
//
// The baseLiteral argument holds the in-progress concatenation to test. It is
// used in recursive calls to simplifyRegexConcat, and initial callers may pass
// nil.
func (pass simplifyRegexPass) simplifyRegexConcat(from Value, reg *syntax.Regexp, baseLiteral []byte) ([]Node, bool) {
	util.ClearCapture(reg.Sub...)

	// Remove empty matches.
	reg.Sub = slices.DeleteFunc(reg.Sub, func(r *syntax.Regexp) bool {
		return r.Op == syntax.OpEmptyMatch
	})

	// The original engine has this check, rejecting any concatenation with more
	// than three subexpressions.
	//
	// It's not clear why this check exists, but there are a few reasons it
	// might've been added:
	//
	// * Many subexpressions can lead to an explosion in alternates (multiplying
	//   for each subexpression)
	//
	// * Avoid needing to figure out logic for multiple infix checks
	//   (".*foo.*bar").
	//
	// TODO(rfratto): Better understand the classic algorithm and whether this
	// check can be removed/modified.
	if len(reg.Sub) > 3 {
		return nil, false
	}

	var result []Node
	var totalLiterals int

	for _, sub := range reg.Sub {
		if util.IsCaseInsensitive(sub) {
			// TODO(rfratto): To support case-insensitive regex simplification, we
			// need to have new binary operations for case-insensitive equality and
			// substring searches.
			return nil, false
		}

		switch {
		case sub.Op == syntax.OpLiteral:
			if totalLiterals != 0 {
				// Only one literal is supported.
				//
				// NOTE(rfratto): I'm not sure why this is, but it's the same
				// logic from the old engine.
				return nil, false
			}

			totalLiterals++
			baseLiteral = append(baseLiteral, []byte(string(sub.Rune))...)
			continue

		case sub.Op == syntax.OpAlternate && len(baseLiteral) != 0:
			var changed bool
			result, changed = pass.simplifyRegexConcatAlternates(from, sub, baseLiteral, result)
			if !changed {
				return nil, false
			}

		case sub.Op == syntax.OpStar && sub.Sub[0].Op == syntax.OpAnyCharNotNL: // ".*"
			continue // Nothing to do; always matches everything.

		default:
			return nil, false
		}
	}

	// We found a filter of concat with alternates.
	if result != nil {
		return result, true
	}

	// We found a concat across literals.
	if len(baseLiteral) > 0 {
		return []Node{
			&BinOp{
				Left:  from,
				Right: &Literal{inner: types.StringLiteral(baseLiteral)},
				Op:    types.BinaryOpMatchSubstr,
			},
		}, true
	}

	return nil, false
}

func (pass simplifyRegexPass) simplifyRegexConcatAlternates(from Value, reg *syntax.Regexp, literal []byte, curr []Node) ([]Node, bool) {
	for _, alt := range reg.Sub {
		if util.IsCaseInsensitive(alt) {
			// TODO(rfratto): To support case-insensitive regex simplification, we
			// need to have new binary operations for case-insensitive equality and
			// substring searches.
			return nil, false
		}

		switch alt.Op {
		case syntax.OpEmptyMatch:
			curr = chainOr(curr, &BinOp{
				Left:  from,
				Right: NewLiteral(string(literal)),
				Op:    types.BinaryOpMatchSubstr,
			})

		case syntax.OpLiteral:
			// Concatenate the root literal with the alternate.
			checkLiteral := string(literal) + string(alt.Rune)

			curr = chainOr(curr, &BinOp{
				Left:  from,
				Right: NewLiteral(checkLiteral),
				Op:    types.BinaryOpMatchSubstr,
			})

		case syntax.OpConcat:
			f, ok := pass.simplifyRegexConcat(from, alt, literal)
			if !ok {
				return nil, false
			}
			curr = chainOr(curr, f...)

		case syntax.OpStar:
			// We treat ".*" as if it was OpEmptyMatch.
			if alt.Sub[0].Op != syntax.OpAnyCharNotNL {
				return nil, false
			}
			curr = chainOr(curr, &BinOp{
				Left:  from,
				Right: NewLiteral(string(literal)),
				Op:    types.BinaryOpMatchSubstr,
			})

		default:
			return nil, false
		}
	}

	if len(curr) > 0 {
		return curr, true
	}
	return nil, false
}

func chainOr(left []Node, right ...Node) []Node {
	if len(left) == 0 {
		return append([]Node{}, right...)
	}

	newTail := &BinOp{
		Left:  left[len(left)-1].(Value),
		Right: right[len(right)-1].(Value),
		Op:    types.BinaryOpOr,
	}

	result := left
	result = append(result, right...)
	result = append(result, newTail)
	return result
}

func extractInstructions(nodes []Node) []Instruction {
	var res []Instruction
	for _, n := range nodes {
		if i, ok := n.(Instruction); ok {
			res = append(res, i)
		}
	}
	return res
}
