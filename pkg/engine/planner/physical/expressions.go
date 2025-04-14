package physical

import (
	"fmt"

	"github.com/apache/arrow/go/v18/arrow"

	"github.com/grafana/loki/v3/pkg/engine/internal/errors"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// ExpressionType represents the type of expression in the physical plan.
type ExpressionType uint32

const (
	_ ExpressionType = iota // zero-value is an invalid type

	ExprTypeUnary
	ExprTypeBinary
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
	case ExprTypeLiteral:
		return "LiteralExpression"
	case ExprTypeColumn:
		return "ColumnExpression"
	default:
		panic(fmt.Sprintf("unknown expression type %d", t))
	}
}

// Evaluator denotes an object that can evaluate an input in form of an [arrow.Record] and return a [ColumnVector] as result.
type Evaluator interface {
	Evaluate(input arrow.Record) (ColumnVector, error)
}

// EvaluateFunc is a function that can be used as [Evaluator].
type EvaluateFunc func(input arrow.Record) (ColumnVector, error)

// Evaluate implements Evaluator.
func (e EvaluateFunc) Evaluate(input arrow.Record) (ColumnVector, error) {
	return e(input)
}

// Expression is the common interface for all expressions in a physical plan.
type Expression interface {
	fmt.Stringer
	Evaluator
	Type() ExpressionType
	isExpr()
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

// LiteralExpression is the common interface for all literal expressions in a
// physical plan.
type LiteralExpression interface {
	Expression
	ValueType() types.ValueType
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

func (e *UnaryExpr) String() string {
	return fmt.Sprintf("%s(%s)", e.Op, e.Left)
}

// ID returns the type of the [UnaryExpr].
func (*UnaryExpr) Type() ExpressionType {
	return ExprTypeUnary
}

func (e *UnaryExpr) Evaluate(input arrow.Record) (ColumnVector, error) {
	left, err := e.Left.Evaluate(input)
	if err != nil {
		return nil, err
	}
	fmt.Println("evaluated *UnaryExpr", e.Op, left)
	return nil, errors.ErrNotImplemented
}

// BinaryExpr is an expression that implements the [BinaryExpression] interface.
type BinaryExpr struct {
	Left, Right Expression
	Op          types.BinaryOp
}

func (*BinaryExpr) isExpr()       {}
func (*BinaryExpr) isBinaryExpr() {}

func (e *BinaryExpr) String() string {
	return fmt.Sprintf("%s(%s, %s)", e.Op, e.Left, e.Right)
}

// ID returns the type of the [BinaryExpr].
func (*BinaryExpr) Type() ExpressionType {
	return ExprTypeBinary
}

func (e *BinaryExpr) Evaluate(input arrow.Record) (ColumnVector, error) {
	left, err := e.Left.Evaluate(input)
	if err != nil {
		return nil, err
	}
	right, err := e.Right.Evaluate(input)
	if err != nil {
		return nil, err
	}
	fmt.Println("evaluated *BinaryExpr", e.Op, left, right)
	return nil, errors.ErrNotImplemented
}

// LiteralExpr is an expression that implements the [LiteralExpression] interface.
type LiteralExpr struct {
	Value types.Literal
}

func (*LiteralExpr) isExpr()        {}
func (*LiteralExpr) isLiteralExpr() {}

// String returns the string representation of the literal value.
func (e *LiteralExpr) String() string {
	return e.Value.String()
}

// ID returns the type of the [LiteralExpr].
func (*LiteralExpr) Type() ExpressionType {
	return ExprTypeLiteral
}

// ValueType returns the kind of value represented by the literal.
func (e *LiteralExpr) ValueType() types.ValueType {
	return e.Value.ValueType()
}

func (e *LiteralExpr) Evaluate(input arrow.Record) (ColumnVector, error) {
	return &Scalar{
		value: e.Value,
		rows:  input.NumRows(),
	}, nil
}

func NewLiteral(value any) *LiteralExpr {
	return &LiteralExpr{
		Value: types.Literal{Value: value},
	}
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

// String returns the string representation of the column expression.
// It contains of the name of the column and its type, joined by a dot (`.`).
func (e *ColumnExpr) String() string {
	return e.Ref.String()
}

// ID returns the type of the [ColumnExpr].
func (e *ColumnExpr) Type() ExpressionType {
	return ExprTypeColumn
}

func (e *ColumnExpr) Evaluate(input arrow.Record) (ColumnVector, error) {
	for i := range input.NumCols() {
		if input.ColumnName(int(i)) == e.Ref.Column {
			return &Array{
				array: input.Column(int(i)),
				rows:  input.NumRows(),
			}, nil
		}
	}
	return nil, errors.ErrKey
}

// ColumnVector represents columnar values from evaluated expressions.
type ColumnVector interface {
	// ToArray returns the underlying Arrow array representation of the column vector.
	ToArray() arrow.Array
	// Value returns the value at the specified index position in the column vector.
	Value(i int64) any
	// Type returns the Arrow data type of the column vector.
	Type() arrow.Type
}

// Scalar represents a single value repeated any number of times.
type Scalar struct {
	value types.Literal
	rows  int64
}

var _ ColumnVector = (*Scalar)(nil)

// ToArray implements ColumnVector.
func (v *Scalar) ToArray() arrow.Array {
	return nil
}

// Value implements ColumnVector.
func (v *Scalar) Value(_ int64) any {
	return v.value.Value
}

// Type implements ColumnVector.
func (v Scalar) Type() arrow.Type {
	switch v.value.ValueType() {
	case types.ValueTypeBool:
		return arrow.BOOL
	case types.ValueTypeStr:
		return arrow.STRING
	case types.ValueTypeInt:
		return arrow.INT64
	case types.ValueTypeFloat:
		return arrow.FLOAT64
	case types.ValueTypeTimestamp:
		return arrow.UINT64
	default:
		return arrow.NULL
	}
}

// Array represents a column of data, stored as an [arrow.Array].
type Array struct {
	array arrow.Array
	rows  int64
}

// ToArray implements ColumnVector.
func (a *Array) ToArray() arrow.Array {
	return a.array
}

// Value implements ColumnVector.
func (a *Array) Value(i int64) any {
	return a.array.ValueStr(int(i))
}

// Type implements ColumnVector.
func (a *Array) Type() arrow.Type {
	return a.array.DataType().ID()
}

var _ ColumnVector = (*Array)(nil)
