package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

/*
This file defines the structures and methods for binary operations within the logical query plan.

Key components:
1. BinOpType: An enum representing the type of binary operation (e.g., math, comparison, set).
2. BinOpExpr: A struct representing a binary operation expression, which includes:
   - name: Identifier for the binary operation.
   - ty: Type of binary operation (BinOpType).
   - op: The actual operation (e.g., +, -, ==, !=, &&, ||).
   - l: Left expression operand.
   - r: Right expression operand.
3. Methods for BinOpExpr:
   - ToField: Converts the binary operation expression to a column schema.
   - Type: Returns the type of binary operation.
   - Name: Returns the name of the binary operation.
*/

// UNKNOWN is a constant used for string representation of unknown operation types
const UNKNOWN = "unknown"

// BinOpType is an enum representing the category of binary operation.
// It allows for grouping similar operations together for type-safe handling.
type BinOpType int

const (
	BinOpTypeInvalid BinOpType = iota // Invalid or uninitialized binary operation
	BinOpTypeMath                     // Mathematical operations (+, -, *, /, %)
	BinOpTypeCmp                      // Comparison operations (==, !=, <, <=, >, >=)
	BinOpTypeSet                      // Set operations (AND, OR, NOT, XOR)
)

// String returns a human-readable representation of the binary operation type.
func (t BinOpType) String() string {
	switch t {
	case BinOpTypeMath:
		return "math"
	case BinOpTypeCmp:
		return "cmp"
	case BinOpTypeSet:
		return "set"
	default:
		return UNKNOWN
	}
}

// BinOpExpr represents a binary operation expression in the query plan.
// It combines two expressions with an operation to produce a result.
type BinOpExpr struct {
	// name is the identifier for this binary operation
	name string
	// ty is the type of binary operation (e.g. math, cmp, set)
	ty BinOpType
	// op is the actual operation (e.g. +, -, ==, !=, &&, ||)
	op int
	// l is the left expression
	l Expr
	// r is the right expression
	r Expr
}

// ToField converts the binary operation to a column schema.
// The name of the column is the name of the binary operation,
// and the type is derived from the left operand.
func (b BinOpExpr) ToField(p Plan) schema.ColumnSchema {
	return schema.ColumnSchema{
		Name: b.name,
		Type: b.l.ToField(p).Type,
	}
}

// Type returns the type of the binary operation.
func (b BinOpExpr) Type() BinOpType {
	return b.ty
}

// Name returns the name of the binary operation.
func (b BinOpExpr) Name() string {
	return b.name
}

// Left returns the left operand of the binary operation.
func (b BinOpExpr) Left() Expr {
	return b.l
}

// Right returns the right operand of the binary operation.
func (b BinOpExpr) Right() Expr {
	return b.r
}

// Op returns a string representation of the binary operation.
// It delegates to the appropriate type-specific operation based on the operation type.
func (b BinOpExpr) Op() fmt.Stringer {
	switch b.ty {
	case BinOpTypeMath:
		return BinaryOpMath(b.op)
	case BinOpTypeCmp:
		return BinaryOpCmp(b.op)
	case BinOpTypeSet:
		return BinaryOpSet(b.op)
	default:
		panic(fmt.Sprintf("unknown binary operation type: %d", b.ty))
	}
}

// BinaryOpMath represents mathematical binary operations
type BinaryOpMath int

const (
	BinaryOpMathInvalid BinaryOpMath = iota
	// BinaryOpAdd represents addition operation (+)
	BinaryOpAdd
	// BinaryOpSubtract represents subtraction operation (-)
	BinaryOpSubtract
	// BinaryOpMultiply represents multiplication operation (*)
	BinaryOpMultiply
	// BinaryOpDivide represents division operation (/)
	BinaryOpDivide
	// BinaryOpModulo represents modulo operation (%)
	BinaryOpModulo
)

// String returns a human-readable representation of the mathematical operation.
func (b BinaryOpMath) String() string {
	switch b {
	case BinaryOpAdd:
		return "+"
	case BinaryOpSubtract:
		return "-"
	case BinaryOpMultiply:
		return "*"
	case BinaryOpDivide:
		return "/"
	case BinaryOpModulo:
		return "%"
	default:
		return "unknown"
	}
}

// BinaryOpCmp represents comparison binary operations
type BinaryOpCmp int

const (
	BinaryOpCmpInvalid BinaryOpCmp = iota
	// BinaryOpEq represents equality comparison (==)
	BinaryOpEq
	// BinaryOpNeq represents inequality comparison (!=)
	BinaryOpNeq
	// BinaryOpLt represents less than comparison (<)
	BinaryOpLt
	// BinaryOpLte represents less than or equal comparison (<=)
	BinaryOpLte
	// BinaryOpGt represents greater than comparison (>)
	BinaryOpGt
	// BinaryOpGte represents greater than or equal comparison (>=)
	BinaryOpGte
)

// String returns a human-readable representation of the comparison operation.
func (b BinaryOpCmp) String() string {
	switch b {
	case BinaryOpEq:
		return "=="
	case BinaryOpNeq:
		return "!="
	case BinaryOpLt:
		return "<"
	case BinaryOpLte:
		return "<="
	case BinaryOpGt:
		return ">"
	case BinaryOpGte:
		return ">="
	default:
		return UNKNOWN
	}
}

// BinaryOpSet represents set operations between boolean expressions
type BinaryOpSet int

const (
	BinaryOpSetInvalid BinaryOpSet = iota
	// BinaryOpAnd represents logical AND operation
	BinaryOpAnd
	// BinaryOpOr represents logical OR operation
	BinaryOpOr
	// BinaryOpNot represents logical NOT operation (also known as "unless")
	BinaryOpNot
	// BinaryOpXor represents logical XOR operation
	BinaryOpXor
)

// String returns a human-readable representation of the set operation.
func (b BinaryOpSet) String() string {
	switch b {
	case BinaryOpAnd:
		return "and"
	case BinaryOpOr:
		return "or"
	case BinaryOpNot:
		return "not"
	case BinaryOpXor:
		return "xor"
	default:
		return UNKNOWN
	}
}

// newBinOpConstructor creates a constructor function for binary operations of a specific type.
// This is a higher-order function that returns a function for creating binary operations.
func newBinOpConstructor(t BinOpType, op int) func(name string, l Expr, r Expr) Expr {
	return func(name string, l Expr, r Expr) Expr {
		binop := BinOpExpr{
			name: name,
			ty:   t,
			op:   op,
			l:    l,
			r:    r,
		}
		return NewBinOpExpr(binop)
	}
}

// newBinOpSetConstructor creates a constructor function for set operations.
func newBinOpSetConstructor(op BinaryOpSet) func(name string, l Expr, r Expr) Expr {
	return newBinOpConstructor(BinOpTypeSet, int(op))
}

// newBinOpCmpConstructor creates a constructor function for comparison operations.
func newBinOpCmpConstructor(op BinaryOpCmp) func(name string, l Expr, r Expr) Expr {
	return newBinOpConstructor(BinOpTypeCmp, int(op))
}

// newBinOpMathConstructor creates a constructor function for mathematical operations.
func newBinOpMathConstructor(op BinaryOpMath) func(name string, l Expr, r Expr) Expr {
	return newBinOpConstructor(BinOpTypeMath, int(op))
}

var (
	// And creates a logical AND expression
	And = newBinOpSetConstructor(BinaryOpAnd)
	// Or creates a logical OR expression
	Or = newBinOpSetConstructor(BinaryOpOr)
	// Not creates a logical NOT expression
	Not = newBinOpSetConstructor(BinaryOpNot)
	// Xor creates a logical XOR expression
	Xor = newBinOpSetConstructor(BinaryOpXor)
)

var (
	// Eq creates an equality comparison expression
	Eq = newBinOpCmpConstructor(BinaryOpEq)
	// Neq creates an inequality comparison expression
	Neq = newBinOpCmpConstructor(BinaryOpNeq)
	// Lt creates a less than comparison expression
	Lt = newBinOpCmpConstructor(BinaryOpLt)
	// Lte creates a less than or equal comparison expression
	Lte = newBinOpCmpConstructor(BinaryOpLte)
	// Gt creates a greater than comparison expression
	Gt = newBinOpCmpConstructor(BinaryOpGt)
	// Gte creates a greater than or equal comparison expression
	Gte = newBinOpCmpConstructor(BinaryOpGte)
)

var (
	// Add creates a binary addition expression
	Add = newBinOpMathConstructor(BinaryOpAdd)
	// Subtract creates a binary subtraction expression
	Subtract = newBinOpMathConstructor(BinaryOpSubtract)
	// Multiply creates a binary multiplication expression
	Multiply = newBinOpMathConstructor(BinaryOpMultiply)
	// Divide creates a binary division expression
	Divide = newBinOpMathConstructor(BinaryOpDivide)
	// Modulo creates a binary modulo expression
	Modulo = newBinOpMathConstructor(BinaryOpModulo)
)
