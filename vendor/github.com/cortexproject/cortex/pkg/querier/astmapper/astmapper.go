package astmapper

import (
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/promql"
)

// ASTMapper is the exported interface for mapping between multiple AST representations
type ASTMapper interface {
	Map(node promql.Node) (promql.Node, error)
}

// MapperFunc is a function adapter for ASTMapper
type MapperFunc func(node promql.Node) (promql.Node, error)

// Map applies a mapperfunc as an ASTMapper
func (fn MapperFunc) Map(node promql.Node) (promql.Node, error) {
	return fn(node)
}

// MultiMapper can compose multiple ASTMappers
type MultiMapper struct {
	mappers []ASTMapper
}

// Map implements ASTMapper
func (m *MultiMapper) Map(node promql.Node) (promql.Node, error) {
	var result promql.Node = node
	var err error

	if len(m.mappers) == 0 {
		return nil, errors.New("MultiMapper: No mappers registered")
	}

	for _, x := range m.mappers {
		result, err = x.Map(result)
		if err != nil {
			return nil, err
		}
	}
	return result, nil

}

// Register adds ASTMappers into a multimapper.
// Since registered functions are applied in the order they're registered, it's advised to register them
// in decreasing priority and only operate on nodes that each function cares about, defaulting to CloneNode.
func (m *MultiMapper) Register(xs ...ASTMapper) {
	m.mappers = append(m.mappers, xs...)
}

// NewMultiMapper instaniates an ASTMapper from multiple ASTMappers
func NewMultiMapper(xs ...ASTMapper) *MultiMapper {
	m := &MultiMapper{}
	m.Register(xs...)
	return m
}

// CloneNode is a helper function to clone a node.
func CloneNode(node promql.Node) (promql.Node, error) {
	return promql.ParseExpr(node.String())
}

// NodeMapper either maps a single AST node or returns the unaltered node.
// It also returns a bool to signal that no further recursion is necessary.
// This is helpful because it allows mappers to only implement logic for node types they want to change.
// It makes some mappers trivially easy to implement
type NodeMapper interface {
	MapNode(node promql.Node) (mapped promql.Node, finished bool, err error)
}

// NodeMapperFunc is an adapter for NodeMapper
type NodeMapperFunc func(node promql.Node) (promql.Node, bool, error)

// MapNode applies a NodeMapperFunc as a NodeMapper
func (f NodeMapperFunc) MapNode(node promql.Node) (promql.Node, bool, error) {
	return f(node)
}

// NewASTNodeMapper creates an ASTMapper from a NodeMapper
func NewASTNodeMapper(mapper NodeMapper) ASTNodeMapper {
	return ASTNodeMapper{mapper}
}

// ASTNodeMapper is an ASTMapper adapter which uses a NodeMapper internally.
type ASTNodeMapper struct {
	NodeMapper
}

// Map implements ASTMapper from a NodeMapper
func (nm ASTNodeMapper) Map(node promql.Node) (promql.Node, error) {
	node, fin, err := nm.MapNode(node)

	if err != nil {
		return nil, err
	}

	if fin {
		return node, nil
	}

	switch n := node.(type) {
	case nil:
		// nil handles cases where we check optional fields that are not set
		return nil, nil

	case promql.Expressions:
		for i, e := range n {
			mapped, err := nm.Map(e)
			if err != nil {
				return nil, err
			}
			n[i] = mapped.(promql.Expr)
		}
		return n, nil

	case *promql.AggregateExpr:
		expr, err := nm.Map(n.Expr)
		if err != nil {
			return nil, err
		}
		n.Expr = expr.(promql.Expr)
		return n, nil

	case *promql.BinaryExpr:
		lhs, err := nm.Map(n.LHS)
		if err != nil {
			return nil, err
		}
		n.LHS = lhs.(promql.Expr)

		rhs, err := nm.Map(n.RHS)
		if err != nil {
			return nil, err
		}
		n.RHS = rhs.(promql.Expr)
		return n, nil

	case *promql.Call:
		for i, e := range n.Args {
			mapped, err := nm.Map(e)
			if err != nil {
				return nil, err
			}
			n.Args[i] = mapped.(promql.Expr)
		}
		return n, nil

	case *promql.SubqueryExpr:
		mapped, err := nm.Map(n.Expr)
		if err != nil {
			return nil, err
		}
		n.Expr = mapped.(promql.Expr)
		return n, nil

	case *promql.ParenExpr:
		mapped, err := nm.Map(n.Expr)
		if err != nil {
			return nil, err
		}
		n.Expr = mapped.(promql.Expr)
		return n, nil

	case *promql.UnaryExpr:
		mapped, err := nm.Map(n.Expr)
		if err != nil {
			return nil, err
		}
		n.Expr = mapped.(promql.Expr)
		return n, nil

	case *promql.EvalStmt:
		mapped, err := nm.Map(n.Expr)
		if err != nil {
			return nil, err
		}
		n.Expr = mapped.(promql.Expr)
		return n, nil

	case *promql.NumberLiteral, *promql.StringLiteral, *promql.VectorSelector, *promql.MatrixSelector:
		return n, nil

	default:
		panic(errors.Errorf("nodeMapper: unhandled node type %T", node))
	}
}
