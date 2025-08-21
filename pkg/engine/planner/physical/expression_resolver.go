package physical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/logical"
)

// ExpressionResolver resolves logical expressions to physical expressions,
// using the column registry to resolve ambiguous column references.
type ExpressionResolver struct {
	registry *ColumnRegistry
}

// NewExpressionResolver creates a new expression resolver with the given column registry.
func NewExpressionResolver(registry *ColumnRegistry) *ExpressionResolver {
	return &ExpressionResolver{
		registry: registry,
	}
}

// ResolveExpression converts a logical expression to a physical expression,
// resolving any ambiguous column references using the registry.
func (r *ExpressionResolver) ResolveExpression(expr logical.Value) (Expression, error) {
	switch e := expr.(type) {
	case *logical.BinOp:
		return r.resolveBinaryOp(e)
	case *logical.ColumnRef:
		return r.resolveColumnRef(e)
	case *logical.Literal:
		return r.resolveLiteral(e)
	default:
		return nil, fmt.Errorf("unsupported expression type: %T", expr)
	}
}

func (r *ExpressionResolver) resolveBinaryOp(op *logical.BinOp) (Expression, error) {
	left, err := r.ResolveExpression(op.Left)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve left operand: %w", err)
	}

	right, err := r.ResolveExpression(op.Right)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve right operand: %w", err)
	}

	return &BinaryExpr{
		Left:  left,
		Right: right,
		Op:    op.Op,
	}, nil
}

func (r *ExpressionResolver) resolveColumnRef(ref *logical.ColumnRef) (Expression, error) {
	columnType := ref.Ref.Type
	columnName := ref.Ref.Column

	// If the column type is ambiguous, resolve it using the registry
	if columnType == types.ColumnTypeAmbiguous {
		if r.registry != nil {
			resolvedCol, err := r.registry.ResolveColumn(columnName)
			if err != nil {
				// Column not in registry, keep original type
				// This might be a label that wasn't parsed
				columnType = types.ColumnTypeLabel
			} else {
				columnType = resolvedCol.Type
			}
		}
	}

	return &ColumnExpr{
		Ref: types.ColumnRef{
			Column: columnName,
			Type:   columnType,
		},
	}, nil
}

func (r *ExpressionResolver) resolveLiteral(lit *logical.Literal) (Expression, error) {
	return NewLiteral(lit.Value()), nil
}
