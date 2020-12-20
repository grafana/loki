package astmapper

import (
	"github.com/prometheus/prometheus/promql/parser"
)

/*
subtreeFolder is a NodeMapper which embeds an entire parser.Node in an embedded query
if it does not contain any previously embedded queries. This allows the frontend to "zip up" entire
subtrees of an AST that have not already been parallelized.

*/
type subtreeFolder struct{}

// NewSubtreeFolder creates a subtreeFolder which can reduce an AST
// to one embedded query if it contains no embedded queries yet
func NewSubtreeFolder() ASTMapper {
	return NewASTNodeMapper(&subtreeFolder{})
}

// MapNode implements NodeMapper
func (f *subtreeFolder) MapNode(node parser.Node) (parser.Node, bool, error) {
	switch n := node.(type) {
	// do not attempt to fold number or string leaf nodes
	case *parser.NumberLiteral, *parser.StringLiteral:
		return n, true, nil
	}

	containsEmbedded, err := Predicate(node, predicate(isEmbedded))
	if err != nil {
		return nil, true, err
	}

	if containsEmbedded {
		return node, false, nil
	}

	expr, err := VectorSquasher(node)
	return expr, true, err
}

func isEmbedded(node parser.Node) (bool, error) {
	switch n := node.(type) {
	case *parser.VectorSelector:
		if n.Name == EmbeddedQueriesMetricName {
			return true, nil
		}

	case *parser.MatrixSelector:
		return isEmbedded(n.VectorSelector)
	}
	return false, nil
}

type predicate = func(parser.Node) (bool, error)

// Predicate is a helper which uses parser.Walk under the hood determine if any node in a subtree
// returns true for a specified function
func Predicate(node parser.Node, fn predicate) (bool, error) {
	v := &visitor{
		fn: fn,
	}

	if err := parser.Walk(v, node, nil); err != nil {
		return false, err
	}
	return v.result, nil
}

type visitor struct {
	fn     predicate
	result bool
}

// Visit implements parser.Visitor
func (v *visitor) Visit(node parser.Node, path []parser.Node) (parser.Visitor, error) {
	// if the visitor has already seen a predicate success, don't overwrite
	if v.result {
		return nil, nil
	}

	var err error

	v.result, err = v.fn(node)
	if err != nil {
		return nil, err
	}
	if v.result {
		return nil, nil
	}
	return v, nil
}
