// Package logical implements logical query plan operations and expressions
package logical

import (
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
)

// Compile-time checks to ensure types implement Expr interface
var (
	_ Expr = BinaryMathExpr{}
	_ Expr = BooleanCmpExpr{}
	_ Expr = BooleanSetExpr{}
)

// BinaryOpMath represents mathematical binary operations
type BinaryOpMath string

const (
	// BinaryOpAdd represents addition operation (+)
	BinaryOpAdd BinaryOpMath = "+"
	// BinaryOpSubtract represents subtraction operation (-)
	BinaryOpSubtract BinaryOpMath = "-"
	// BinaryOpMultiply represents multiplication operation (*)
	BinaryOpMultiply BinaryOpMath = "*"
	// BinaryOpDivide represents division operation (/)
	BinaryOpDivide BinaryOpMath = "/"
	// BinaryOpModulo represents modulo operation (%)
	BinaryOpModulo BinaryOpMath = "%"
)

var (
	// Add creates a binary addition expression
	Add = newBinaryMathExprConstructor(BinaryOpAdd)
	// Subtract creates a binary subtraction expression
	Subtract = newBinaryMathExprConstructor(BinaryOpSubtract)
	// Multiply creates a binary multiplication expression
	Multiply = newBinaryMathExprConstructor(BinaryOpMultiply)
	// Divide creates a binary division expression
	Divide = newBinaryMathExprConstructor(BinaryOpDivide)
	// Modulo creates a binary modulo expression
	Modulo = newBinaryMathExprConstructor(BinaryOpModulo)
)

// BinaryOpCmp represents comparison binary operations
type BinaryOpCmp string

const (
	// BinaryOpEq represents equality comparison (==)
	BinaryOpEq BinaryOpCmp = "=="
	// BinaryOpNeq represents inequality comparison (!=)
	BinaryOpNeq BinaryOpCmp = "!="
	// BinaryOpLt represents less than comparison (<)
	BinaryOpLt BinaryOpCmp = "<"
	// BinaryOpLte represents less than or equal comparison (<=)
	BinaryOpLte BinaryOpCmp = "<="
	// BinaryOpGt represents greater than comparison (>)
	BinaryOpGt BinaryOpCmp = ">"
	// BinaryOpGte represents greater than or equal comparison (>=)
	BinaryOpGte BinaryOpCmp = ">="
)

var (
	// Eq creates an equality comparison expression
	Eq = newBinaryCompareExprConstructor(BinaryOpEq)
	// Neq creates an inequality comparison expression
	Neq = newBinaryCompareExprConstructor(BinaryOpNeq)
	// Lt creates a less than comparison expression
	Lt = newBinaryCompareExprConstructor(BinaryOpLt)
	// Lte creates a less than or equal comparison expression
	Lte = newBinaryCompareExprConstructor(BinaryOpLte)
	// Gt creates a greater than comparison expression
	Gt = newBinaryCompareExprConstructor(BinaryOpGt)
	// Gte creates a greater than or equal comparison expression
	Gte = newBinaryCompareExprConstructor(BinaryOpGte)
)

// BinaryOpSet represents set operations between boolean expressions
type BinaryOpSet string

const (
	// BinaryOpAnd represents logical AND operation
	BinaryOpAnd BinaryOpSet = "and"
	// BinaryOpOr represents logical OR operation
	BinaryOpOr BinaryOpSet = "or"
	// BinaryOpNot represents logical NOT operation (also known as "unless")
	BinaryOpNot BinaryOpSet = "not"
	// BinaryOpXor represents logical XOR operation
	BinaryOpXor BinaryOpSet = "xor"
)

var (
	// And creates a logical AND expression
	And = newBinarySetExprConstructor(BinaryOpAnd)
	// Or creates a logical OR expression
	Or = newBinarySetExprConstructor(BinaryOpOr)
	// Not creates a logical NOT expression
	Not = newBinarySetExprConstructor(BinaryOpNot)
	// Xor creates a logical XOR expression
	Xor = newBinarySetExprConstructor(BinaryOpXor)
)

// BinaryMathExpr represents a mathematical operation between two expressions
type BinaryMathExpr struct {
	name string
	op   BinaryOpMath
	l    Expr
	r    Expr
}

// newBinaryMathExprConstructor creates a constructor function for binary mathematical expressions
func newBinaryMathExprConstructor(op BinaryOpMath) func(name string, l Expr, r Expr) BinaryMathExpr {
	return func(name string, l Expr, r Expr) BinaryMathExpr {
		return BinaryMathExpr{
			name: name,
			op:   op,
			l:    l,
			r:    r,
		}
	}
}

// ToField converts the binary math expression to a column schema
func (b BinaryMathExpr) ToField(p Plan) schema.ColumnSchema {
	return schema.ColumnSchema{
		Name: b.name,
		Type: b.l.ToField(p).Type,
	}
}

// BooleanCmpExpr represents a comparison operation between two expressions
type BooleanCmpExpr struct {
	name string
	op   BinaryOpCmp
	l    Expr
	r    Expr
}

// newBinaryCompareExprConstructor creates a constructor function for binary comparison expressions
func newBinaryCompareExprConstructor(op BinaryOpCmp) func(name string, l Expr, r Expr) BooleanCmpExpr {
	return func(name string, l Expr, r Expr) BooleanCmpExpr {
		return BooleanCmpExpr{
			name: name,
			op:   op,
			l:    l,
			r:    r,
		}
	}
}

// ToField converts the boolean comparison expression to a column schema
func (b BooleanCmpExpr) ToField(_ Plan) schema.ColumnSchema {
	return schema.ColumnSchema{
		Name: b.name,
		// TODO: bool type
		Type: datasetmd.VALUE_TYPE_UINT64,
	}
}

// BooleanSetExpr represents a set operation between two boolean expressions
type BooleanSetExpr struct {
	name string
	op   BinaryOpSet
	l    Expr
	r    Expr
}

// newBinarySetExprConstructor creates a constructor function for binary set expressions
func newBinarySetExprConstructor(op BinaryOpSet) func(name string, l Expr, r Expr) BooleanSetExpr {
	return func(name string, l Expr, r Expr) BooleanSetExpr {
		return BooleanSetExpr{
			name: name,
			op:   op,
			l:    l,
			r:    r,
		}
	}
}

// ToField converts the boolean set expression to a column schema
func (b BooleanSetExpr) ToField(p Plan) schema.ColumnSchema {
	return schema.ColumnSchema{
		Name: b.name,
		Type: b.l.ToField(p).Type,
	}
}
