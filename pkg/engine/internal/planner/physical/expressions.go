package physical

import (
	"fmt"
	"strings"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// ExpressionType represents the type of expression in the physical plan.
type ExpressionType uint32

const (
	_ ExpressionType = iota // zero-value is an invalid type

	ExprTypeUnary
	ExprTypeBinary
	ExprTypeVariadic
	ExprTypeLiteral
	ExprTypeColumn
)

// String returns the string representation of the [ExpressionType].
func (t ExpressionType) String() string {
	switch t {
	case ExprTypeUnary:
		return "UnaryExpression"
	case ExprTypeBinary:
		return "BinaryExpression"
	case ExprTypeVariadic:
		return "VariadicExpression"
	case ExprTypeLiteral:
		return "LiteralExpression"
	case ExprTypeColumn:
		return "ColumnExpression"
	default:
		panic(fmt.Sprintf("unknown expression type %d", t))
	}
}

// Expression is the common interface for all expressions in a physical plan.
type Expression interface {
	fmt.Stringer
	Clone() Expression
	Type() ExpressionType
	isExpr()
}

func cloneExpressions[E Expression](exprs []E) []E {
	clonedExprs := make([]E, len(exprs))
	for i, expr := range exprs {
		clonedExprs[i] = expr.Clone().(E)
	}
	return clonedExprs
}

// UnaryExpression is the common interface for all unary expressions in a
// physical plan.
type UnaryExpression interface {
	Expression
	isUnaryExpr()
}

// BinaryExpression is the common interface for all binary expressions in a
// physical plan.
type BinaryExpression interface {
	Expression
	isBinaryExpr()
}

// FunctionExpression is the common interface for all function expressions in a
// physical plan.
type FunctionExpression interface {
	Expression
	isFunctionExpr()
}

// LiteralExpression is the common interface for all literal expressions in a
// physical plan.
type LiteralExpression interface {
	Expression
	ValueType() types.DataType
	isLiteralExpr()
}

// ColumnExpression is the common interface for all column expressions in a
// physical plan.
type ColumnExpression interface {
	Expression
	isColumnExpr()
}

// UnaryExpr is an expression that implements the [UnaryExpression] interface.
type UnaryExpr struct {
	// Left is the expression being operated on
	Left Expression
	// Op is the unary operator to apply to the expression
	Op types.UnaryOp
}

func (*UnaryExpr) isExpr()      {}
func (*UnaryExpr) isUnaryExpr() {}

// Clone returns a copy of the [UnaryExpr].
func (e *UnaryExpr) Clone() Expression {
	return &UnaryExpr{
		Left: e.Left.Clone(),
		Op:   e.Op,
	}
}

func (e *UnaryExpr) String() string {
	return fmt.Sprintf("%s(%s)", e.Op, e.Left)
}

// Type returns the type of the [UnaryExpr].
func (*UnaryExpr) Type() ExpressionType {
	return ExprTypeUnary
}

// BinaryExpr is an expression that implements the [BinaryExpression] interface.
type BinaryExpr struct {
	Left, Right Expression
	Op          types.BinaryOp
}

func (*BinaryExpr) isExpr()       {}
func (*BinaryExpr) isBinaryExpr() {}

// Clone returns a copy of the [BinaryExpr].
func (e *BinaryExpr) Clone() Expression {
	return &BinaryExpr{
		Left:  e.Left.Clone(),
		Right: e.Right.Clone(),
		Op:    e.Op,
	}
}

func (e *BinaryExpr) String() string {
	return fmt.Sprintf("%s(%s, %s)", e.Op, e.Left, e.Right)
}

// Type returns the type of the [BinaryExpr].
func (*BinaryExpr) Type() ExpressionType {
	return ExprTypeBinary
}

// LiteralExpr is an expression that implements the [LiteralExpression] interface.
type LiteralExpr struct {
	inner types.Literal
}

func (*LiteralExpr) isExpr()        {}
func (*LiteralExpr) isLiteralExpr() {}

// Clone returns a copy of the [LiteralExpr].
func (e *LiteralExpr) Clone() Expression {
	// No need to clone literals.
	return &LiteralExpr{inner: e.inner}
}

// String returns the string representation of the literal value.
func (e *LiteralExpr) String() string {
	return e.inner.String()
}

// Type returns the type of the [LiteralExpr].
func (*LiteralExpr) Type() ExpressionType {
	return ExprTypeLiteral
}

// ValueType returns the kind of value represented by the literal.
func (e *LiteralExpr) ValueType() types.DataType {
	return e.inner.Type()
}

// Value returns the value represented by the literal.
func (e *LiteralExpr) Value() any {
	return e.inner.Any()
}

// Literal returns the underlying literal.
func (e *LiteralExpr) Literal() types.Literal {
	return e.inner
}

func NewLiteral(value any) *LiteralExpr {
	return &LiteralExpr{inner: types.NewLiteral(value)}
}

// ColumnExpr is an expression that implements the [ColumnExpr] interface.
type ColumnExpr struct {
	Ref types.ColumnRef
}

func newColumnExpr(column string, ty types.ColumnType) *ColumnExpr {
	return &ColumnExpr{
		Ref: types.ColumnRef{
			Column: column,
			Type:   ty,
		},
	}
}

func (e *ColumnExpr) isExpr()       {}
func (e *ColumnExpr) isColumnExpr() {}

// Clone returns a copy of the [ColumnExpr].
func (e *ColumnExpr) Clone() Expression {
	return &ColumnExpr{Ref: e.Ref}
}

// String returns the string representation of the column expression.
// It contains of the name of the column and its type, joined by a dot (`.`).
func (e *ColumnExpr) String() string {
	return e.Ref.String()
}

// Type returns the type of the [ColumnExpr].
func (e *ColumnExpr) Type() ExpressionType {
	return ExprTypeColumn
}

// VariadicExpr is an expression that implements the [FunctionExpression] interface.
type VariadicExpr struct {
	// Op is the function operation to apply to the parameters
	Op types.VariadicOp

	// Expressions are the parameters paaws to the function
	Expressions []Expression
}

func (*VariadicExpr) isExpr()         {}
func (*VariadicExpr) isFunctionExpr() {}

// Clone returns a copy of the [VariadicExpr].
func (e *VariadicExpr) Clone() Expression {
	return &VariadicExpr{
		Expressions: cloneExpressions(e.Expressions),
		Op:          e.Op,
	}
}

func (e *VariadicExpr) String() string {
	exprs := make([]string, len(e.Expressions))
	for i, expr := range e.Expressions {
		exprs[i] = expr.String()
	}
	return fmt.Sprintf("%s(%s)", e.Op, strings.Join(exprs, ", "))
}

// Type returns the type of the [VariadicExpr].
func (*VariadicExpr) Type() ExpressionType {
	return ExprTypeVariadic
}

// ExpressionToMatchers converts a selector expression to a list of matchers.
// The selector expression is required to be a (tree of) [BinaryExpression]
// with a [ColumnExpression] on the left and a [LiteralExpression] on the right.
// It optionally supports ambiguous column references. Non-ambiguous column references are label matchers.
func ExpressionToMatchers(selector Expression, allowAmbiguousColumnRefs bool) ([]*labels.Matcher, error) {
	if selector == nil {
		return nil, nil
	}

	switch expr := selector.(type) {
	case *BinaryExpr:
		switch expr.Op {
		case types.BinaryOpAnd:
			lhs, err := ExpressionToMatchers(expr.Left, allowAmbiguousColumnRefs)
			if err != nil {
				return nil, err
			}
			rhs, err := ExpressionToMatchers(expr.Right, allowAmbiguousColumnRefs)
			if err != nil {
				return nil, err
			}
			return append(lhs, rhs...), nil
		case types.BinaryOpEq, types.BinaryOpNeq, types.BinaryOpMatchRe, types.BinaryOpNotMatchRe:
			op, err := convertBinaryOp(expr.Op)
			if err != nil {
				return nil, err
			}
			name, err := convertColumnRef(expr.Left, allowAmbiguousColumnRefs)
			if err != nil {
				return nil, err
			}
			value, err := convertLiteralToString(expr.Right)
			if err != nil {
				return nil, err
			}
			lhs, err := labels.NewMatcher(op, name, value)
			if err != nil {
				return nil, err
			}
			return []*labels.Matcher{lhs}, nil
		default:
			return nil, fmt.Errorf("invalid binary expression in stream selector expression: %v", expr.Op.String())
		}
	default:
		return nil, fmt.Errorf("invalid expression type in stream selector expression: %T", expr)
	}
}

func convertLiteralToString(expr Expression) (string, error) {
	l, ok := expr.(*LiteralExpr)
	if !ok {
		return "", fmt.Errorf("expected literal expression, got %T", expr)
	}
	if l.ValueType() != types.Loki.String {
		return "", fmt.Errorf("literal type is not a string, got %v", l.ValueType())
	}
	return l.Value().(string), nil
}

func convertColumnRef(expr Expression, allowAmbiguousColumnRefs bool) (string, error) {
	ref, ok := expr.(*ColumnExpr)
	if !ok {
		return "", fmt.Errorf("expected column expression, got %T", expr)
	}
	if !allowAmbiguousColumnRefs && ref.Ref.Type != types.ColumnTypeLabel {
		return "", fmt.Errorf("column type is not a label, got %v", ref.Ref.Type)
	}
	return ref.Ref.Column, nil
}

var (
	binOpToMatchTypeMapping = map[types.BinaryOp]labels.MatchType{
		types.BinaryOpEq:         labels.MatchEqual,
		types.BinaryOpNeq:        labels.MatchNotEqual,
		types.BinaryOpMatchRe:    labels.MatchRegexp,
		types.BinaryOpNotMatchRe: labels.MatchNotRegexp,
	}
)

func convertBinaryOp(t types.BinaryOp) (labels.MatchType, error) {
	ty, ok := binOpToMatchTypeMapping[t]
	if !ok {
		return -1, fmt.Errorf("invalid binary operator for matcher: %v", t)
	}
	return ty, nil
}
